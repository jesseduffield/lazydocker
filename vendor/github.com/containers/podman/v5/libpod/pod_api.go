//go:build !remote

package libpod

import (
	"context"
	"errors"
	"fmt"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/containers/podman/v5/pkg/parallel"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/cgroups"
)

// startInitContainers starts a pod's init containers.
func (p *Pod) startInitContainers(ctx context.Context) error {
	initCtrs, err := p.initContainers()
	if err != nil {
		return err
	}
	// Now iterate init containers
	for _, initCon := range initCtrs {
		if err := initCon.startNoPodLock(ctx, true); err != nil {
			return err
		}
		// Check that the init container waited correctly and the exit
		// code is good
		rc, err := initCon.Wait(ctx)
		if err != nil {
			return err
		}
		if rc != 0 {
			return fmt.Errorf("init container %s exited with code %d", initCon.ID(), rc)
		}
		// If the container is a once init container, we need to remove it
		// after it runs
		if initCon.config.InitContainerType == define.OneShotInitContainer {
			icLock := initCon.lock
			icLock.Lock()
			var time *uint
			opts := ctrRmOpts{
				RemovePod: true,
				Timeout:   time,
			}

			if _, _, err := p.runtime.removeContainer(ctx, initCon, opts); err != nil {
				icLock.Unlock()
				return fmt.Errorf("failed to remove once init container %s: %w", initCon.ID(), err)
			}
			icLock.Unlock()
		}
	}
	return nil
}

// Start starts all containers within a pod.
// It combines the effects of Init() and Start() on a container.
// If a container has already been initialized it will be started,
// otherwise it will be initialized then started.
// Containers that are already running or have been paused are ignored
// All containers are started independently, in order dictated by their
// dependencies.
// An error and a map[string]error are returned.
// If the error is not nil and the map is nil, an error was encountered before
// any containers were started.
// If map is not nil, an error was encountered when starting one or more
// containers. The container ID is mapped to the error encountered. The error is
// set to ErrPodPartialFail.
// If both error and the map are nil, all containers were started successfully.
func (p *Pod) Start(ctx context.Context) (map[string]error, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if !p.valid {
		return nil, define.ErrPodRemoved
	}

	if err := p.maybeStartServiceContainer(ctx); err != nil {
		return nil, err
	}

	// Before "regular" containers start in the pod, all init containers
	// must have run and exited successfully.
	if err := p.startInitContainers(ctx); err != nil {
		return nil, err
	}
	allCtrs, err := p.runtime.state.PodContainers(p)
	if err != nil {
		return nil, err
	}
	// Build a dependency graph of containers in the pod
	graph, err := BuildContainerGraph(allCtrs)
	if err != nil {
		return nil, fmt.Errorf("generating dependency graph for pod %s: %w", p.ID(), err)
	}
	// If there are no containers without dependencies, we can't start
	// Error out
	if len(graph.noDepNodes) == 0 {
		return nil, fmt.Errorf("no containers in pod %s have no dependencies, cannot start pod: %w", p.ID(), define.ErrNoSuchCtr)
	}

	ctrErrors := make(map[string]error)
	ctrsVisited := make(map[string]bool)

	// Traverse the graph beginning at nodes with no dependencies
	for _, node := range graph.noDepNodes {
		startNode(ctx, node, false, ctrErrors, ctrsVisited, false)
	}

	if len(ctrErrors) > 0 {
		return ctrErrors, fmt.Errorf("starting some containers: %w", define.ErrPodPartialFail)
	}
	defer p.newPodEvent(events.Start)
	return nil, nil
}

// Stop stops all containers within a pod without a timeout.  It assumes -1 for
// a timeout.
func (p *Pod) Stop(ctx context.Context, cleanup bool) (map[string]error, error) {
	return p.StopWithTimeout(ctx, cleanup, -1)
}

// StopWithTimeout stops all containers within a pod that are not already stopped
// Each container will use its own stop timeout.
// Only running containers will be stopped. Paused, stopped, or created
// containers will be ignored.
// If cleanup is true, mounts and network namespaces will be cleaned up after
// the container is stopped.
// All containers are stopped independently. An error stopping one container
// will not prevent other containers being stopped.
// An error and a map[string]error are returned.
// If the error is not nil and the map is nil, an error was encountered before
// any containers were stopped.
// If map is not nil, an error was encountered when stopping one or more
// containers. The container ID is mapped to the error encountered. The error is
// set to ErrPodPartialFail.
// If both error and the map are nil, all containers were stopped without error.
func (p *Pod) StopWithTimeout(ctx context.Context, cleanup bool, timeout int) (map[string]error, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	return p.stopWithTimeout(ctx, cleanup, timeout)
}

func (p *Pod) stopWithTimeout(ctx context.Context, cleanup bool, timeout int) (map[string]error, error) {
	if !p.valid {
		return nil, define.ErrPodRemoved
	}

	allCtrs, err := p.runtime.state.PodContainers(p)
	if err != nil {
		return nil, err
	}

	p.newPodEvent(events.Stop)

	var ctrErrors map[string]error

	// Try and generate a graph of the pod for ordered stop.
	graph, err := BuildContainerGraph(allCtrs)
	if err != nil {
		// Can't do an ordered stop, do it the old fashioned way.
		logrus.Warnf("Unable to build graph for pod %s, switching to unordered stop: %v", p.ID(), err)

		ctrErrors = make(map[string]error)
		for _, ctr := range allCtrs {
			var err error
			if timeout > -1 {
				err = ctr.StopWithTimeout(uint(timeout))
			} else {
				err = ctr.Stop()
			}
			if err != nil && !errors.Is(err, define.ErrCtrStateInvalid) && !errors.Is(err, define.ErrCtrStopped) {
				ctrErrors[ctr.ID()] = err
			} else if cleanup {
				err := ctr.Cleanup(ctx, false)
				if err != nil && !errors.Is(err, define.ErrCtrStateInvalid) && !errors.Is(err, define.ErrCtrStopped) {
					ctrErrors[ctr.ID()] = err
				}
			}
		}
	} else {
		var realTimeout *uint
		if timeout > -1 {
			innerTimeout := uint(timeout)
			realTimeout = &innerTimeout
		}

		ctrErrors, err = stopContainerGraph(ctx, graph, p, realTimeout, cleanup)
		if err != nil {
			return nil, err
		}
	}

	if len(ctrErrors) > 0 {
		return ctrErrors, fmt.Errorf("stopping some containers: %w", define.ErrPodPartialFail)
	}

	if err := p.maybeStopServiceContainer(); err != nil {
		return nil, err
	}

	if err := p.updatePod(); err != nil {
		return nil, err
	}
	if err := p.removePodCgroup(); err != nil {
		return nil, err
	}

	return nil, nil
}

// Stops the pod if only the infra containers remains running.
func (p *Pod) stopIfOnlyInfraRemains(ctx context.Context, ignoreID string) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	infraID := ""

	if p.HasInfraContainer() {
		infra, err := p.infraContainer()
		if err != nil {
			return err
		}
		infraID = infra.ID()
	}

	allCtrs, err := p.runtime.state.PodContainers(p)
	if err != nil {
		return err
	}

	for _, ctr := range allCtrs {
		if ctr.ID() == infraID || ctr.ID() == ignoreID {
			continue
		}

		state, err := ctr.State()
		if err != nil {
			return fmt.Errorf("getting state of container %s: %w", ctr.ID(), err)
		}

		switch state {
		case define.ContainerStateExited,
			define.ContainerStateRemoving,
			define.ContainerStateStopping,
			define.ContainerStateUnknown:
			continue
		default:
			return nil
		}
	}

	errs, err := p.stopWithTimeout(ctx, true, -1)
	for ctr, e := range errs {
		logrus.Errorf("Failed to stop container %s: %v", ctr, e)
	}
	return err
}

// Cleanup cleans up all containers within a pod that have stopped.
// All containers are cleaned up independently. An error with one container will
// not prevent other containers being cleaned up.
// An error and a map[string]error are returned.
// If the error is not nil and the map is nil, an error was encountered before
// any containers were cleaned up.
// If map is not nil, an error was encountered when working on one or more
// containers. The container ID is mapped to the error encountered. The error is
// set to ErrPodPartialFail.
// If both error and the map are nil, all containers were paused without error
func (p *Pod) Cleanup(ctx context.Context) (map[string]error, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if !p.valid {
		return nil, define.ErrPodRemoved
	}

	allCtrs, err := p.runtime.state.PodContainers(p)
	if err != nil {
		return nil, err
	}

	ctrErrChan := make(map[string]<-chan error)

	// Enqueue a function for each container with the parallel executor.
	for _, ctr := range allCtrs {
		c := ctr
		logrus.Debugf("Adding parallel job to clean up container %s", c.ID())
		retChan := parallel.Enqueue(ctx, func() error {
			return c.Cleanup(ctx, false)
		})

		ctrErrChan[c.ID()] = retChan
	}

	ctrErrors := make(map[string]error)

	// Get returned error for every container we worked on
	for id, channel := range ctrErrChan {
		if err := <-channel; err != nil {
			if errors.Is(err, define.ErrCtrStateInvalid) || errors.Is(err, define.ErrCtrStopped) {
				continue
			}
			ctrErrors[id] = err
		}
	}

	if len(ctrErrors) > 0 {
		return ctrErrors, fmt.Errorf("cleaning up some containers: %w", define.ErrPodPartialFail)
	}

	if err := p.maybeStopServiceContainer(); err != nil {
		return nil, err
	}

	return nil, nil
}

// Pause pauses all containers within a pod that are running.
// Only running containers will be paused. Paused, stopped, or created
// containers will be ignored.
// All containers are paused independently. An error pausing one container
// will not prevent other containers being paused.
// An error and a map[string]error are returned.
// If the error is not nil and the map is nil, an error was encountered before
// any containers were paused.
// If map is not nil, an error was encountered when pausing one or more
// containers. The container ID is mapped to the error encountered. The error is
// set to ErrPodPartialFail.
// If both error and the map are nil, all containers were paused without error
func (p *Pod) Pause(ctx context.Context) (map[string]error, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if !p.valid {
		return nil, define.ErrPodRemoved
	}

	if rootless.IsRootless() {
		cgroupv2, err := cgroups.IsCgroup2UnifiedMode()
		if err != nil {
			return nil, fmt.Errorf("failed to determine cgroupversion: %w", err)
		}
		if !cgroupv2 {
			return nil, fmt.Errorf("can not pause pods containing rootless containers with cgroup V1: %w", define.ErrNoCgroups)
		}
	}

	allCtrs, err := p.runtime.state.PodContainers(p)
	if err != nil {
		return nil, err
	}

	ctrErrChan := make(map[string]<-chan error)

	// Enqueue a function for each container with the parallel executor.
	for _, ctr := range allCtrs {
		c := ctr
		logrus.Debugf("Adding parallel job to pause container %s", c.ID())
		retChan := parallel.Enqueue(ctx, c.Pause)

		ctrErrChan[c.ID()] = retChan
	}

	p.newPodEvent(events.Pause)

	ctrErrors := make(map[string]error)

	// Get returned error for every container we worked on
	for id, channel := range ctrErrChan {
		if err := <-channel; err != nil {
			if errors.Is(err, define.ErrCtrStateInvalid) || errors.Is(err, define.ErrCtrStopped) {
				continue
			}
			ctrErrors[id] = err
		}
	}

	if len(ctrErrors) > 0 {
		return ctrErrors, fmt.Errorf("pausing some containers: %w", define.ErrPodPartialFail)
	}
	return nil, nil
}

// Unpause unpauses all containers within a pod that are running.
// Only paused containers will be unpaused. Running, stopped, or created
// containers will be ignored.
// All containers are unpaused independently. An error unpausing one container
// will not prevent other containers being unpaused.
// An error and a map[string]error are returned.
// If the error is not nil and the map is nil, an error was encountered before
// any containers were unpaused.
// If map is not nil, an error was encountered when unpausing one or more
// containers. The container ID is mapped to the error encountered. The error is
// set to ErrPodPartialFail.
// If both error and the map are nil, all containers were unpaused without error.
func (p *Pod) Unpause(ctx context.Context) (map[string]error, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if !p.valid {
		return nil, define.ErrPodRemoved
	}

	allCtrs, err := p.runtime.state.PodContainers(p)
	if err != nil {
		return nil, err
	}

	ctrErrChan := make(map[string]<-chan error)

	// Enqueue a function for each container with the parallel executor.
	for _, ctr := range allCtrs {
		c := ctr
		logrus.Debugf("Adding parallel job to unpause container %s", c.ID())
		retChan := parallel.Enqueue(ctx, c.Unpause)

		ctrErrChan[c.ID()] = retChan
	}

	p.newPodEvent(events.Unpause)

	ctrErrors := make(map[string]error)

	// Get returned error for every container we worked on
	for id, channel := range ctrErrChan {
		if err := <-channel; err != nil {
			if errors.Is(err, define.ErrCtrStateInvalid) || errors.Is(err, define.ErrCtrStopped) {
				continue
			}
			ctrErrors[id] = err
		}
	}

	if len(ctrErrors) > 0 {
		return ctrErrors, fmt.Errorf("unpausing some containers: %w", define.ErrPodPartialFail)
	}
	return nil, nil
}

// Restart restarts all containers within a pod that are not paused or in an error state.
// It combines the effects of Stop() and Start() on a container
// Each container will use its own stop timeout.
// All containers are started independently, in order dictated by their
// dependencies. An error restarting one container
// will not prevent other containers being restarted.
// An error and a map[string]error are returned.
// If the error is not nil and the map is nil, an error was encountered before
// any containers were restarted.
// If map is not nil, an error was encountered when restarting one or more
// containers. The container ID is mapped to the error encountered. The error is
// set to ErrPodPartialFail.
// If both error and the map are nil, all containers were restarted without error.
func (p *Pod) Restart(ctx context.Context) (map[string]error, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if !p.valid {
		return nil, define.ErrPodRemoved
	}

	if err := p.maybeStartServiceContainer(ctx); err != nil {
		return nil, err
	}

	allCtrs, err := p.runtime.state.PodContainers(p)
	if err != nil {
		return nil, err
	}

	// Build a dependency graph of containers in the pod
	graph, err := BuildContainerGraph(allCtrs)
	if err != nil {
		return nil, fmt.Errorf("generating dependency graph for pod %s: %w", p.ID(), err)
	}

	ctrErrors := make(map[string]error)
	ctrsVisited := make(map[string]bool)

	// If there are no containers without dependencies, we can't start
	// Error out
	if len(graph.noDepNodes) == 0 {
		return nil, fmt.Errorf("no containers in pod %s have no dependencies, cannot start pod: %w", p.ID(), define.ErrNoSuchCtr)
	}

	// Traverse the graph beginning at nodes with no dependencies
	for _, node := range graph.noDepNodes {
		startNode(ctx, node, false, ctrErrors, ctrsVisited, true)
	}

	if len(ctrErrors) > 0 {
		return ctrErrors, fmt.Errorf("stopping some containers: %w", define.ErrPodPartialFail)
	}
	p.newPodEvent(events.Stop)
	p.newPodEvent(events.Start)
	return nil, nil
}

// Kill sends a signal to all running containers within a pod.
// Signals will only be sent to running containers. Containers that are not
// running will be ignored. All signals are sent independently, and sending will
// continue even if some containers encounter errors.
// An error and a map[string]error are returned.
// If the error is not nil and the map is nil, an error was encountered before
// any containers were signalled.
// If map is not nil, an error was encountered when signalling one or more
// containers. The container ID is mapped to the error encountered. The error is
// set to ErrPodPartialFail.
// If both error and the map are nil, all containers were signalled successfully.
func (p *Pod) Kill(ctx context.Context, signal uint) (map[string]error, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if !p.valid {
		return nil, define.ErrPodRemoved
	}

	allCtrs, err := p.runtime.state.PodContainers(p)
	if err != nil {
		return nil, err
	}

	ctrErrChan := make(map[string]<-chan error)

	// Enqueue a function for each container with the parallel executor.
	for _, ctr := range allCtrs {
		c := ctr
		logrus.Debugf("Adding parallel job to kill container %s", c.ID())
		retChan := parallel.Enqueue(ctx, func() error {
			return c.Kill(signal)
		})

		ctrErrChan[c.ID()] = retChan
	}

	p.newPodEvent(events.Kill)

	ctrErrors := make(map[string]error)

	// Get returned error for every container we worked on
	for id, channel := range ctrErrChan {
		if err := <-channel; err != nil {
			if errors.Is(err, define.ErrCtrStateInvalid) || errors.Is(err, define.ErrCtrStopped) {
				continue
			}
			ctrErrors[id] = err
		}
	}

	if len(ctrErrors) > 0 {
		return ctrErrors, fmt.Errorf("killing some containers: %w", define.ErrPodPartialFail)
	}

	if err := p.maybeStopServiceContainer(); err != nil {
		return nil, err
	}

	return nil, nil
}

// Status gets the status of all containers in the pod.
// Returns a map of Container ID to Container Status.
func (p *Pod) Status() (map[string]define.ContainerStatus, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if !p.valid {
		return nil, define.ErrPodRemoved
	}
	allCtrs, err := p.runtime.state.PodContainers(p)
	if err != nil {
		return nil, err
	}
	noInitCtrs := make([]*Container, 0)
	// Do not add init containers into status
	for _, ctr := range allCtrs {
		if ctrType := ctr.config.InitContainerType; len(ctrType) < 1 {
			noInitCtrs = append(noInitCtrs, ctr)
		}
	}
	return containerStatusFromContainers(noInitCtrs)
}

func containerStatusFromContainers(allCtrs []*Container) (map[string]define.ContainerStatus, error) {
	status := make(map[string]define.ContainerStatus, len(allCtrs))
	for _, ctr := range allCtrs {
		state, err := ctr.State()

		if err != nil {
			return nil, err
		}

		status[ctr.ID()] = state
	}

	return status, nil
}

// Inspect returns a PodInspect struct to describe the pod.
func (p *Pod) Inspect() (*define.InspectPodData, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	if err := p.updatePod(); err != nil {
		return nil, err
	}

	containers, err := p.runtime.state.PodContainers(p)
	if err != nil {
		return nil, err
	}
	ctrs := make([]define.InspectPodContainerInfo, 0, len(containers))
	ctrStatuses := make(map[string]define.ContainerStatus, len(containers))
	for _, c := range containers {
		containerStatus := "unknown"
		// Ignoring possible errors here because we don't want this to be
		// catastrophic in nature
		containerState, err := c.State()
		if err == nil {
			containerStatus = containerState.String()
		}
		ctrs = append(ctrs, define.InspectPodContainerInfo{
			ID:    c.ID(),
			Name:  c.Name(),
			State: containerStatus,
		})
		// Do not add init containers fdr status
		if len(c.config.InitContainerType) < 1 {
			ctrStatuses[c.ID()] = c.state.State
		}
	}
	podState, err := createPodStatusResults(ctrStatuses)
	if err != nil {
		return nil, err
	}

	namespaces := map[string]bool{
		"pid":    p.config.UsePodPID,
		"ipc":    p.config.UsePodIPC,
		"net":    p.config.UsePodNet,
		"mount":  p.config.UsePodMount,
		"user":   p.config.UsePodUser,
		"uts":    p.config.UsePodUTS,
		"cgroup": p.config.UsePodCgroupNS,
	}

	sharesNS := []string{}
	for nsStr, include := range namespaces {
		if include {
			sharesNS = append(sharesNS, nsStr)
		}
	}

	// Infra config contains detailed information on the pod's infra
	// container.
	var infraConfig *define.InspectPodInfraConfig
	var inspectMounts []define.InspectMount
	var devices []define.InspectDevice
	var infraSecurity []string
	if p.state.InfraContainerID != "" {
		infra, err := p.runtime.GetContainer(p.state.InfraContainerID)
		if err != nil {
			return nil, err
		}
		infraConfig = new(define.InspectPodInfraConfig)
		infraConfig.HostNetwork = p.NetworkMode() == "host"
		infraConfig.StaticIP = infra.config.ContainerNetworkConfig.StaticIP
		infraConfig.NoManageResolvConf = infra.config.UseImageResolvConf
		infraConfig.NoManageHostname = infra.config.UseImageHostname
		infraConfig.NoManageHosts = infra.config.UseImageHosts
		infraConfig.CPUPeriod = p.CPUPeriod()
		infraConfig.CPUQuota = p.CPUQuota()
		infraConfig.CPUSetCPUs = p.ResourceLim().CPU.Cpus
		infraConfig.PidNS = p.NamespaceMode(specs.PIDNamespace)
		infraConfig.UserNS = p.NamespaceMode(specs.UserNamespace)
		infraConfig.UtsNS = p.NamespaceMode(specs.UTSNamespace)
		namedVolumes, mounts := infra.SortUserVolumes(infra.config.Spec)
		inspectMounts, err = infra.GetMounts(namedVolumes, infra.config.ImageVolumes, mounts)
		infraSecurity = infra.GetSecurityOptions()
		if err != nil {
			return nil, err
		}

		if len(infra.config.ContainerNetworkConfig.DNSServer) > 0 {
			infraConfig.DNSServer = make([]string, 0, len(infra.config.ContainerNetworkConfig.DNSServer))
			for _, entry := range infra.config.ContainerNetworkConfig.DNSServer {
				infraConfig.DNSServer = append(infraConfig.DNSServer, entry.String())
			}
		}
		if len(infra.config.ContainerNetworkConfig.DNSSearch) > 0 {
			infraConfig.DNSSearch = make([]string, 0, len(infra.config.ContainerNetworkConfig.DNSSearch))
			infraConfig.DNSSearch = append(infraConfig.DNSSearch, infra.config.ContainerNetworkConfig.DNSSearch...)
		}
		if len(infra.config.ContainerNetworkConfig.DNSOption) > 0 {
			infraConfig.DNSOption = make([]string, 0, len(infra.config.ContainerNetworkConfig.DNSOption))
			infraConfig.DNSOption = append(infraConfig.DNSOption, infra.config.ContainerNetworkConfig.DNSOption...)
		}
		if len(infra.config.HostAdd) > 0 {
			infraConfig.HostAdd = make([]string, 0, len(infra.config.HostAdd))
			infraConfig.HostAdd = append(infraConfig.HostAdd, infra.config.HostAdd...)
		}
		if len(infra.config.BaseHostsFile) > 0 {
			infraConfig.HostsFile = infra.config.BaseHostsFile
		}

		networks, err := infra.networks()
		if err != nil {
			return nil, err
		}
		netNames := make([]string, 0, len(networks))
		for name := range networks {
			netNames = append(netNames, name)
		}
		if len(netNames) > 0 {
			infraConfig.Networks = netNames
		}
		infraConfig.NetworkOptions = infra.config.ContainerNetworkConfig.NetworkOptions
		infraConfig.PortBindings = makeInspectPortBindings(infra.config.ContainerNetworkConfig.PortMappings)
	}

	inspectData := define.InspectPodData{
		ID:                  p.ID(),
		Name:                p.Name(),
		Namespace:           p.Namespace(),
		Created:             p.CreatedTime(),
		CreateCommand:       p.config.CreateCommand,
		ExitPolicy:          string(p.config.ExitPolicy),
		State:               podState,
		Hostname:            p.config.Hostname,
		Labels:              p.Labels(),
		CreateCgroup:        p.config.UsePodCgroup,
		CgroupParent:        p.CgroupParent(),
		CgroupPath:          p.state.CgroupPath,
		CreateInfra:         infraConfig != nil,
		InfraContainerID:    p.state.InfraContainerID,
		InfraConfig:         infraConfig,
		SharedNamespaces:    sharesNS,
		NumContainers:       uint(len(containers)),
		Containers:          ctrs,
		CPUSetCPUs:          p.ResourceLim().CPU.Cpus,
		CPUPeriod:           p.CPUPeriod(),
		CPUQuota:            p.CPUQuota(),
		MemoryLimit:         p.MemoryLimit(),
		Mounts:              inspectMounts,
		Devices:             devices,
		BlkioDeviceReadBps:  p.BlkiThrottleReadBps(),
		VolumesFrom:         p.VolumesFrom(),
		SecurityOpts:        infraSecurity,
		MemorySwap:          p.MemorySwap(),
		BlkioWeight:         p.BlkioWeight(),
		CPUSetMems:          p.CPUSetMems(),
		BlkioDeviceWriteBps: p.BlkiThrottleWriteBps(),
		CPUShares:           p.CPUShares(),
		RestartPolicy:       p.config.RestartPolicy,
		LockNumber:          p.lock.ID(),
	}

	return &inspectData, nil
}
