//go:build !remote

package libpod

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/driver"
	"github.com/containers/podman/v5/pkg/signal"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/docker/go-units"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

// inspectLocked inspects a container for low-level information.
// The caller must held c.lock.
func (c *Container) inspectLocked(size bool) (*define.InspectContainerData, error) {
	storeCtr, err := c.runtime.store.Container(c.ID())
	if err != nil {
		return nil, fmt.Errorf("getting container from store %q: %w", c.ID(), err)
	}
	layer, err := c.runtime.store.Layer(storeCtr.LayerID)
	if err != nil {
		return nil, fmt.Errorf("reading information about layer %q: %w", storeCtr.LayerID, err)
	}
	driverData, err := driver.GetDriverData(c.runtime.store, layer.ID)
	if err != nil {
		return nil, fmt.Errorf("getting graph driver info %q: %w", c.ID(), err)
	}
	return c.getContainerInspectData(size, driverData)
}

// Inspect a container for low-level information
func (c *Container) Inspect(size bool) (*define.InspectContainerData, error) {
	if !c.batched {
		c.lock.Lock()
		defer c.lock.Unlock()

		if err := c.syncContainer(); err != nil {
			return nil, err
		}
	}

	return c.inspectLocked(size)
}

func (c *Container) volumesFrom() ([]string, error) {
	ctrSpec, err := c.specFromState()
	if err != nil {
		return nil, err
	}
	if ctrs, ok := ctrSpec.Annotations[define.VolumesFromAnnotation]; ok {
		return strings.Split(ctrs, ";"), nil
	}
	return nil, nil
}

func (c *Container) getContainerInspectData(size bool, driverData *define.DriverData) (*define.InspectContainerData, error) {
	config := c.config
	runtimeInfo := c.state
	ctrSpec, err := c.specFromState()
	if err != nil {
		return nil, err
	}

	// Process is allowed to be nil in the stateSpec
	args := []string{}
	if config.Spec.Process != nil {
		args = config.Spec.Process.Args
	}
	var path string
	if len(args) > 0 {
		path = args[0]
	}
	if len(args) > 1 {
		args = args[1:]
	}

	execIDs := []string{}
	for id := range c.state.ExecSessions {
		execIDs = append(execIDs, id)
	}

	resolvPath := ""
	hostsPath := ""
	hostnamePath := ""
	if c.state.BindMounts != nil {
		if getPath, ok := c.state.BindMounts["/etc/resolv.conf"]; ok {
			resolvPath = getPath
		}
		if getPath, ok := c.state.BindMounts["/etc/hosts"]; ok {
			hostsPath = getPath
		}
		if getPath, ok := c.state.BindMounts["/etc/hostname"]; ok {
			hostnamePath = getPath
		}
	}

	namedVolumes, mounts := c.SortUserVolumes(ctrSpec)
	inspectMounts, err := c.GetMounts(namedVolumes, c.config.ImageVolumes, mounts)
	if err != nil {
		return nil, err
	}

	cgroupPath, err := c.cGroupPath()
	if err != nil {
		// Handle the case where the container is not running or has no cgroup.
		if errors.Is(err, define.ErrNoCgroups) || errors.Is(err, define.ErrCtrStopped) {
			cgroupPath = ""
		} else {
			return nil, err
		}
	}

	data := &define.InspectContainerData{
		ID:      config.ID,
		Created: config.CreatedTime,
		Path:    path,
		Args:    args,
		State: &define.InspectContainerState{
			OciVersion:     ctrSpec.Version,
			Status:         runtimeInfo.State.String(),
			Running:        runtimeInfo.State == define.ContainerStateRunning,
			Paused:         runtimeInfo.State == define.ContainerStatePaused,
			OOMKilled:      runtimeInfo.OOMKilled,
			Dead:           runtimeInfo.State.String() == "bad state",
			Pid:            runtimeInfo.PID,
			ConmonPid:      runtimeInfo.ConmonPID,
			ExitCode:       runtimeInfo.ExitCode,
			Error:          runtimeInfo.Error,
			StartedAt:      runtimeInfo.StartedTime,
			FinishedAt:     runtimeInfo.FinishedTime,
			Checkpointed:   runtimeInfo.Checkpointed,
			CgroupPath:     cgroupPath,
			RestoredAt:     runtimeInfo.RestoredTime,
			CheckpointedAt: runtimeInfo.CheckpointedTime,
			Restored:       runtimeInfo.Restored,
			CheckpointPath: runtimeInfo.CheckpointPath,
			CheckpointLog:  runtimeInfo.CheckpointLog,
			RestoreLog:     runtimeInfo.RestoreLog,
			StoppedByUser:  c.state.StoppedByUser,
		},
		Image:                   config.RootfsImageID,
		ImageName:               config.RootfsImageName,
		Namespace:               config.Namespace,
		Rootfs:                  config.Rootfs,
		Pod:                     config.Pod,
		ResolvConfPath:          resolvPath,
		HostnamePath:            hostnamePath,
		HostsPath:               hostsPath,
		StaticDir:               config.StaticDir,
		OCIRuntime:              config.OCIRuntime,
		ConmonPidFile:           config.ConmonPidFile,
		PidFile:                 config.PidFile,
		Name:                    config.Name,
		RestartCount:            int32(runtimeInfo.RestartCount),
		Driver:                  driverData.Name,
		MountLabel:              config.MountLabel,
		ProcessLabel:            config.ProcessLabel,
		AppArmorProfile:         ctrSpec.Process.ApparmorProfile,
		ExecIDs:                 execIDs,
		GraphDriver:             driverData,
		Mounts:                  inspectMounts,
		Dependencies:            c.Dependencies(),
		IsInfra:                 c.IsInfra(),
		IsService:               c.IsService(),
		KubeExitCodePropagation: config.KubeExitCodePropagation.String(),
		LockNumber:              c.lock.ID(),
		UseImageHosts:           c.config.UseImageHosts,
		UseImageHostname:        c.config.UseImageHostname,
	}

	if config.RootfsImageID != "" { // May not be set if the container was created with --rootfs
		image, _, err := c.runtime.libimageRuntime.LookupImage(config.RootfsImageID, nil)
		if err != nil {
			return nil, err
		}
		data.ImageDigest = image.Digest().String()
	}

	if ctrSpec.Process.Capabilities != nil {
		data.EffectiveCaps = ctrSpec.Process.Capabilities.Effective
		data.BoundingCaps = ctrSpec.Process.Capabilities.Bounding
	}

	if c.state.ConfigPath != "" {
		data.OCIConfigPath = c.state.ConfigPath
	}

	// Check if healthcheck is not nil and --no-healthcheck option is not set.
	// If --no-healthcheck is set Test will be always set to `[NONE]`, so the
	// inspect status should be set to nil.
	if c.config.HealthCheckConfig != nil && (len(c.config.HealthCheckConfig.Test) != 1 || c.config.HealthCheckConfig.Test[0] != "NONE") {
		// This container has a healthcheck defined in it; we need to add its state
		healthCheckState, err := c.readHealthCheckLog()
		if err != nil {
			// An error here is not considered fatal; no health state will be displayed
			logrus.Error(err)
		} else {
			data.State.Health = &healthCheckState
		}
	} else {
		data.State.Health = nil
	}

	networkConfig, err := c.getContainerNetworkInfo()
	if err != nil {
		return nil, err
	}
	data.NetworkSettings = networkConfig
	// Ports in NetworkSettings includes exposed ports for network modes that are not host,
	// and not container.
	if c.config.NetNsCtr == "" && c.NetworkMode() != "host" {
		addInspectPortsExpose(c.config.ExposedPorts, data.NetworkSettings.Ports)
	}

	inspectConfig := c.generateInspectContainerConfig(ctrSpec)
	data.Config = inspectConfig

	hostConfig, err := c.generateInspectContainerHostConfig(ctrSpec, namedVolumes, mounts)
	if err != nil {
		return nil, err
	}
	data.HostConfig = hostConfig

	if size {
		rootFsSize, err := c.rootFsSize()
		if err != nil {
			logrus.Errorf("Getting rootfs size %q: %v", config.ID, err)
		}
		data.SizeRootFs = rootFsSize

		rwSize, err := c.rwSize()
		if err != nil {
			logrus.Errorf("Getting rw size %q: %v", config.ID, err)
		}
		data.SizeRw = &rwSize
	}
	return data, nil
}

// Get inspect-formatted mounts list.
// Only includes user-specified mounts. Only includes bind mounts and named
// volumes, not tmpfs volumes.
func (c *Container) GetMounts(namedVolumes []*ContainerNamedVolume, imageVolumes []*ContainerImageVolume, mounts []spec.Mount) ([]define.InspectMount, error) {
	inspectMounts := []define.InspectMount{}

	// No mounts, return early
	if len(c.config.UserVolumes) == 0 {
		return inspectMounts, nil
	}

	for _, volume := range namedVolumes {
		mountStruct := define.InspectMount{}
		mountStruct.Type = "volume"
		mountStruct.Destination = volume.Dest
		mountStruct.Name = volume.Name
		mountStruct.SubPath = volume.SubPath

		// For src and driver, we need to look up the named
		// volume.
		volFromDB, err := c.runtime.state.Volume(volume.Name)
		if err != nil {
			return nil, fmt.Errorf("looking up volume %s in container %s config: %w", volume.Name, c.ID(), err)
		}
		mountStruct.Driver = volFromDB.Driver()

		mountPoint, err := volFromDB.MountPoint()
		if err != nil {
			return nil, err
		}
		mountStruct.Source = mountPoint

		parseMountOptionsForInspect(volume.Options, &mountStruct)

		inspectMounts = append(inspectMounts, mountStruct)
	}

	for _, volume := range imageVolumes {
		mountStruct := define.InspectMount{}
		mountStruct.Type = "image"
		mountStruct.Destination = volume.Dest
		mountStruct.Source = volume.Source
		mountStruct.RW = volume.ReadWrite
		mountStruct.SubPath = volume.SubPath

		inspectMounts = append(inspectMounts, mountStruct)
	}

	for _, mount := range mounts {
		// It's a mount.
		// Is it a tmpfs? If so, discard.
		if mount.Type == define.TypeTmpfs {
			continue
		}

		mountStruct := define.InspectMount{}
		mountStruct.Type = define.TypeBind
		mountStruct.Source = mount.Source
		mountStruct.Destination = mount.Destination

		parseMountOptionsForInspect(mount.Options, &mountStruct)

		inspectMounts = append(inspectMounts, mountStruct)
	}

	return inspectMounts, nil
}

// GetSecurityOptions retrieves and returns the security related annotations and process information upon inspection
func (c *Container) GetSecurityOptions() []string {
	ctrSpec := c.config.Spec
	SecurityOpt := []string{}
	if ctrSpec.Process != nil {
		if ctrSpec.Process.NoNewPrivileges {
			SecurityOpt = append(SecurityOpt, "no-new-privileges")
		}
	}
	if label, ok := ctrSpec.Annotations[define.InspectAnnotationLabel]; ok {
		SecurityOpt = append(SecurityOpt, fmt.Sprintf("label=%s", label))
	}
	if seccomp, ok := ctrSpec.Annotations[define.InspectAnnotationSeccomp]; ok {
		SecurityOpt = append(SecurityOpt, fmt.Sprintf("seccomp=%s", seccomp))
	}
	if apparmor, ok := ctrSpec.Annotations[define.InspectAnnotationApparmor]; ok {
		SecurityOpt = append(SecurityOpt, fmt.Sprintf("apparmor=%s", apparmor))
	}
	if c.config.Spec != nil && c.config.Spec.Linux != nil && c.config.Spec.Linux.MaskedPaths == nil {
		SecurityOpt = append(SecurityOpt, "unmask=all")
	}

	return SecurityOpt
}

// Parse mount options so we can populate them in the mount structure.
// The mount passed in will be modified.
func parseMountOptionsForInspect(options []string, mount *define.InspectMount) {
	isRW := true
	mountProp := ""
	zZ := ""
	otherOpts := []string{}

	// Some of these may be overwritten if the user passes us garbage opts
	// (for example, [ro,rw])
	// We catch these on the Podman side, so not a problem there, but other
	// users of libpod who do not properly validate mount options may see
	// this.
	// Not really worth dealing with on our end - garbage in, garbage out.
	for _, opt := range options {
		switch opt {
		case "ro":
			isRW = false
		case "rw":
			// Do nothing, silently discard
		case "shared", "slave", "private", "rshared", "rslave", "rprivate", "unbindable", "runbindable":
			mountProp = opt
		case "z", "Z":
			zZ = opt
		default:
			otherOpts = append(otherOpts, opt)
		}
	}

	mount.RW = isRW
	mount.Propagation = mountProp
	mount.Mode = zZ
	mount.Options = otherOpts
}

// Generate the InspectContainerConfig struct for the Config field of Inspect.
func (c *Container) generateInspectContainerConfig(spec *spec.Spec) *define.InspectContainerConfig {
	ctrConfig := new(define.InspectContainerConfig)

	ctrConfig.Hostname = c.Hostname()
	ctrConfig.User = c.config.User
	if spec.Process != nil {
		ctrConfig.Tty = spec.Process.Terminal
		ctrConfig.Env = append([]string{}, spec.Process.Env...)

		// finds all secrets mounted as env variables and hides the value
		// the inspect command should not display it
		envSecrets := c.config.EnvSecrets
		for envIndex, envValue := range ctrConfig.Env {
			// env variables come in the style `name=value`
			envName := strings.Split(envValue, "=")[0]

			envSecret, ok := envSecrets[envName]
			if ok {
				ctrConfig.Env[envIndex] = envSecret.Name + "=*******"
			}
		}

		ctrConfig.WorkingDir = spec.Process.Cwd
	}

	ctrConfig.StopTimeout = c.config.StopTimeout
	ctrConfig.Timeout = c.config.Timeout
	ctrConfig.OpenStdin = c.config.Stdin
	ctrConfig.Image = c.config.RootfsImageName
	ctrConfig.SystemdMode = c.Systemd()

	// Leave empty is not explicitly overwritten by user
	if len(c.config.Command) != 0 {
		ctrConfig.Cmd = []string{}
		ctrConfig.Cmd = append(ctrConfig.Cmd, c.config.Command...)
	}

	// Leave empty if not explicitly overwritten by user
	if len(c.config.Entrypoint) != 0 {
		ctrConfig.Entrypoint = c.config.Entrypoint
	}

	if len(c.config.Labels) != 0 {
		ctrConfig.Labels = maps.Clone(c.config.Labels)
	}

	if len(spec.Annotations) != 0 {
		ctrConfig.Annotations = maps.Clone(spec.Annotations)
	}
	ctrConfig.StopSignal = signal.ToDockerFormat(c.config.StopSignal)
	// TODO: should JSON deep copy this to ensure internal pointers don't
	// leak.
	ctrConfig.StartupHealthCheck = c.config.StartupHealthCheckConfig

	ctrConfig.Healthcheck = c.config.HealthCheckConfig

	ctrConfig.HealthcheckOnFailureAction = c.config.HealthCheckOnFailureAction.String()

	ctrConfig.HealthLogDestination = c.HealthCheckLogDestination()

	ctrConfig.HealthMaxLogCount = c.HealthCheckMaxLogCount()

	ctrConfig.HealthMaxLogSize = c.HealthCheckMaxLogSize()

	ctrConfig.CreateCommand = c.config.CreateCommand

	ctrConfig.Timezone = c.config.Timezone
	for _, secret := range c.config.Secrets {
		newSec := define.InspectSecret{}
		newSec.Name = secret.Name
		newSec.ID = secret.ID
		newSec.UID = secret.UID
		newSec.GID = secret.GID
		newSec.Mode = secret.Mode
		ctrConfig.Secrets = append(ctrConfig.Secrets, &newSec)
	}

	// Pad Umask to 4 characters
	if len(c.config.Umask) < 4 {
		pad := strings.Repeat("0", 4-len(c.config.Umask))
		ctrConfig.Umask = pad + c.config.Umask
	} else {
		ctrConfig.Umask = c.config.Umask
	}

	ctrConfig.Passwd = c.config.Passwd
	ctrConfig.ChrootDirs = append(ctrConfig.ChrootDirs, c.config.ChrootDirs...)

	ctrConfig.SdNotifyMode = c.config.SdNotifyMode
	ctrConfig.SdNotifySocket = c.config.SdNotifySocket

	// Exosed ports consists of all exposed ports and all port mappings for
	// this container. It does *NOT* follow to another container if we share
	// the network namespace.
	exposedPorts := make(map[string]struct{})
	for port, protocols := range c.config.ExposedPorts {
		for _, proto := range protocols {
			exposedPorts[fmt.Sprintf("%d/%s", port, proto)] = struct{}{}
		}
	}
	for _, mapping := range c.config.PortMappings {
		for i := range mapping.Range {
			exposedPorts[fmt.Sprintf("%d/%s", mapping.ContainerPort+i, mapping.Protocol)] = struct{}{}
		}
	}
	if len(exposedPorts) > 0 {
		ctrConfig.ExposedPorts = exposedPorts
	}

	return ctrConfig
}

// Generate the InspectContainerHostConfig struct for the HostConfig field of
// Inspect.
func (c *Container) generateInspectContainerHostConfig(ctrSpec *spec.Spec, namedVolumes []*ContainerNamedVolume, mounts []spec.Mount) (*define.InspectContainerHostConfig, error) {
	hostConfig := new(define.InspectContainerHostConfig)

	logConfig := new(define.InspectLogConfig)
	logConfig.Type = c.config.LogDriver
	logConfig.Path = c.config.LogPath
	logConfig.Size = units.HumanSize(float64(c.LogSizeMax()))
	logConfig.Tag = c.config.LogTag

	hostConfig.LogConfig = logConfig

	restartPolicy := new(define.InspectRestartPolicy)
	restartPolicy.Name = c.config.RestartPolicy
	if restartPolicy.Name == "" {
		restartPolicy.Name = define.RestartPolicyNo
	}
	restartPolicy.MaximumRetryCount = c.config.RestartRetries
	hostConfig.RestartPolicy = restartPolicy
	if c.config.NoCgroups {
		hostConfig.Cgroups = "disabled"
	} else {
		hostConfig.Cgroups = "default"
	}

	hostConfig.Dns = make([]string, 0, len(c.config.DNSServer))
	for _, dns := range c.config.DNSServer {
		hostConfig.Dns = append(hostConfig.Dns, dns.String())
	}

	hostConfig.DnsOptions = make([]string, 0, len(c.config.DNSOption))
	hostConfig.DnsOptions = append(hostConfig.DnsOptions, c.config.DNSOption...)

	hostConfig.DnsSearch = make([]string, 0, len(c.config.DNSSearch))
	hostConfig.DnsSearch = append(hostConfig.DnsSearch, c.config.DNSSearch...)

	hostConfig.ExtraHosts = make([]string, 0, len(c.config.HostAdd))
	hostConfig.ExtraHosts = append(hostConfig.ExtraHosts, c.config.HostAdd...)

	hostConfig.GroupAdd = make([]string, 0, len(c.config.Groups))
	hostConfig.GroupAdd = append(hostConfig.GroupAdd, c.config.Groups...)

	hostConfig.HostsFile = c.config.BaseHostsFile

	if ctrSpec.Process != nil {
		if ctrSpec.Process.OOMScoreAdj != nil {
			hostConfig.OomScoreAdj = *ctrSpec.Process.OOMScoreAdj
		}
	}

	hostConfig.SecurityOpt = c.GetSecurityOptions()

	hostConfig.ReadonlyRootfs = ctrSpec.Root.Readonly
	hostConfig.ShmSize = c.config.ShmSize
	hostConfig.Runtime = "oci"

	// Annotations
	if ctrSpec.Annotations != nil {
		if len(ctrSpec.Annotations) != 0 {
			hostConfig.Annotations = ctrSpec.Annotations
		}

		hostConfig.ContainerIDFile = ctrSpec.Annotations[define.InspectAnnotationCIDFile]
		if ctrSpec.Annotations[define.InspectAnnotationAutoremove] == define.InspectResponseTrue {
			hostConfig.AutoRemove = true
		}
		if ctrSpec.Annotations[define.InspectAnnotationAutoremoveImage] == define.InspectResponseTrue {
			hostConfig.AutoRemoveImage = true
		}
		if ctrs, ok := ctrSpec.Annotations[define.VolumesFromAnnotation]; ok {
			hostConfig.VolumesFrom = strings.Split(ctrs, ";")
		}
		if ctrSpec.Annotations[define.InspectAnnotationPrivileged] == define.InspectResponseTrue {
			hostConfig.Privileged = true
		}
		if ctrSpec.Annotations[define.InspectAnnotationInit] == define.InspectResponseTrue {
			hostConfig.Init = true
		}
		if ctrSpec.Annotations[define.InspectAnnotationPublishAll] == define.InspectResponseTrue {
			hostConfig.PublishAllPorts = true
		}
	}

	if err := c.platformInspectContainerHostConfig(ctrSpec, hostConfig); err != nil {
		return nil, err
	}

	// NanoCPUs.
	// This is only calculated if CpuPeriod == 100000.
	// It is given in nanoseconds, versus the microseconds used elsewhere -
	// so multiply by 10000 (not sure why, but 1000 is off by 10).
	if hostConfig.CpuPeriod == 100000 {
		hostConfig.NanoCpus = 10000 * hostConfig.CpuQuota
	}

	// Bind mounts, formatted as src:dst.
	// We'll be appending some options that aren't necessarily in the
	// original command line... but no helping that from inside libpod.
	binds := []string{}
	tmpfs := make(map[string]string)
	for _, namedVol := range namedVolumes {
		if len(namedVol.Options) > 0 {
			binds = append(binds, fmt.Sprintf("%s:%s:%s", namedVol.Name, namedVol.Dest, strings.Join(namedVol.Options, ",")))
		} else {
			binds = append(binds, fmt.Sprintf("%s:%s", namedVol.Name, namedVol.Dest))
		}
	}
	for _, mount := range mounts {
		if mount.Type == define.TypeTmpfs {
			tmpfs[mount.Destination] = strings.Join(mount.Options, ",")
		} else {
			// TODO - maybe we should parse for empty source/destination
			// here. Would be confusing if we print just a bare colon.
			if len(mount.Options) > 0 {
				binds = append(binds, fmt.Sprintf("%s:%s:%s", mount.Source, mount.Destination, strings.Join(mount.Options, ",")))
			} else {
				binds = append(binds, fmt.Sprintf("%s:%s", mount.Source, mount.Destination))
			}
		}
	}
	hostConfig.Binds = binds
	hostConfig.Tmpfs = tmpfs

	// Network mode parsing.
	networkMode := c.NetworkMode()
	hostConfig.NetworkMode = networkMode

	// Port bindings.
	// Only populate if we are creating the network namespace to configure the network.
	if c.config.CreateNetNS {
		hostConfig.PortBindings = makeInspectPortBindings(c.config.PortMappings)
	} else {
		hostConfig.PortBindings = make(map[string][]define.InspectHostPort)
	}

	// Ulimits
	hostConfig.Ulimits = []define.InspectUlimit{}
	if ctrSpec.Process != nil {
		for _, limit := range ctrSpec.Process.Rlimits {
			newLimit := define.InspectUlimit{}
			newLimit.Name = limit.Type
			newLimit.Soft = int64(limit.Soft)
			newLimit.Hard = int64(limit.Hard)
			hostConfig.Ulimits = append(hostConfig.Ulimits, newLimit)
		}
	}

	// Terminal size
	// We can't actually get this for now...
	// So default to something sane.
	// TODO: Populate this.
	hostConfig.ConsoleSize = []uint{0, 0}

	return hostConfig, nil
}

func (c *Container) GetDevices(priv bool, ctrSpec spec.Spec, deviceNodes map[string]string) ([]define.InspectDevice, error) {
	devices := []define.InspectDevice{}
	if ctrSpec.Linux != nil && !priv {
		for _, dev := range ctrSpec.Linux.Devices {
			key := fmt.Sprintf("%d:%d", dev.Major, dev.Minor)
			if deviceNodes == nil {
				nodes, err := util.FindDeviceNodes(false)
				if err != nil {
					return nil, err
				}
				deviceNodes = nodes
			}
			path, ok := deviceNodes[key]
			if !ok {
				logrus.Warnf("Could not locate device %s on host", key)
				continue
			}
			newDev := define.InspectDevice{}
			newDev.PathOnHost = path
			newDev.PathInContainer = dev.Path
			devices = append(devices, newDev)
		}
	}
	return devices, nil
}

func blkioDeviceThrottle(deviceNodes map[string]string, devs []spec.LinuxThrottleDevice) ([]define.InspectBlkioThrottleDevice, error) {
	out := []define.InspectBlkioThrottleDevice{}
	for _, dev := range devs {
		key := fmt.Sprintf("%d:%d", dev.Major, dev.Minor)
		if deviceNodes == nil {
			nodes, err := util.FindDeviceNodes(true)
			if err != nil {
				return nil, err
			}
			deviceNodes = nodes
		}
		path, ok := deviceNodes[key]
		if !ok {
			logrus.Infof("Could not locate throttle device %s in system devices", key)
			continue
		}
		throttleDev := define.InspectBlkioThrottleDevice{}
		throttleDev.Path = path
		throttleDev.Rate = dev.Rate
		out = append(out, throttleDev)
	}
	return out, nil
}
