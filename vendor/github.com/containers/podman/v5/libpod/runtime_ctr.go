//go:build !remote

package libpod

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/containers/buildah"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/containers/podman/v5/libpod/shutdown"
	"github.com/containers/podman/v5/pkg/domain/entities/reports"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/docker/go-units"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/cgroups"
	"go.podman.io/common/pkg/config"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/stringid"
)

// Contains the public Runtime API for containers

// A CtrCreateOption is a functional option which alters the Container created
// by NewContainer
type CtrCreateOption func(*Container) error

// ContainerFilter is a function to determine whether a container is included
// in command output. Containers to be outputted are tested using the function.
// A true return will include the container, a false return will exclude it.
type ContainerFilter func(*Container) bool

// NewContainer creates a new container from a given OCI config.
func (r *Runtime) NewContainer(ctx context.Context, rSpec *spec.Spec, spec *specgen.SpecGenerator, infra bool, options ...CtrCreateOption) (*Container, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}
	if infra {
		options = append(options, withIsInfra())
		if len(spec.RawImageName) == 0 {
			options = append(options, withIsDefaultInfra())
		}
	}
	return r.newContainer(ctx, rSpec, options...)
}

func (r *Runtime) PrepareVolumeOnCreateContainer(_ context.Context, ctr *Container) error {
	// Copy the content from the underlying image into the newly created
	// volume if configured to do so.
	if !r.config.Containers.PrepareVolumeOnCreate {
		return nil
	}

	defer func() {
		if err := ctr.cleanupStorage(); err != nil {
			logrus.Errorf("Cleaning up container storage %s: %v", ctr.ID(), err)
		}
	}()

	mountPoint, err := ctr.mountStorage()
	if err == nil {
		// Finish up mountStorage
		ctr.state.Mounted = true
		ctr.state.Mountpoint = mountPoint
		if err = ctr.save(); err != nil {
			logrus.Errorf("Saving container %s state: %v", ctr.ID(), err)
		}
	}

	return err
}

// RestoreContainer re-creates a container from an imported checkpoint
func (r *Runtime) RestoreContainer(ctx context.Context, rSpec *spec.Spec, config *ContainerConfig) (*Container, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	ctr, err := r.initContainerVariables(rSpec, config)
	if err != nil {
		return nil, fmt.Errorf("initializing container variables: %w", err)
	}
	// For an imported checkpoint no one has ever set the StartedTime. Set it now.
	ctr.state.StartedTime = time.Now()

	// If the path to ConmonPidFile starts with the default value (RunRoot), then
	// the user has not specified '--conmon-pidfile' during run or create (probably).
	// In that case reset ConmonPidFile to be set to the default value later.
	if strings.HasPrefix(ctr.config.ConmonPidFile, r.storageConfig.RunRoot) {
		ctr.config.ConmonPidFile = ""
	}

	// If the path to PidFile starts with the default value (RunRoot), then
	// the user has not specified '--pidfile' during run or create (probably).
	// In that case reset PidFile to be set to the default value later.
	if strings.HasPrefix(ctr.config.PidFile, r.storageConfig.RunRoot) {
		ctr.config.PidFile = ""
	}

	return r.setupContainer(ctx, ctr)
}

// RenameContainer renames the given container.
// Returns a copy of the container that has been renamed if successful.
func (r *Runtime) RenameContainer(_ context.Context, ctr *Container, newName string) (*Container, error) {
	ctr.lock.Lock()
	defer ctr.lock.Unlock()

	if err := ctr.syncContainer(); err != nil {
		return nil, err
	}

	newName = strings.TrimPrefix(newName, "/")
	if newName == "" || !define.NameRegex.MatchString(newName) {
		return nil, define.RegexError
	}

	// We need to pull an updated config, in case another rename fired and
	// the config was re-written.
	newConf, err := r.state.GetContainerConfig(ctr.ID())
	if err != nil {
		return nil, fmt.Errorf("retrieving container %s configuration from DB to remove: %w", ctr.ID(), err)
	}
	ctr.config = newConf

	logrus.Infof("Going to rename container %s from %q to %q", ctr.ID(), ctr.Name(), newName)

	// Step 1: Alter the config. Save the old name, we need it to rewrite
	// the config.
	oldName := ctr.config.Name
	ctr.config.Name = newName

	// Step 2: rewrite the old container's config in the DB.
	if err := r.state.SafeRewriteContainerConfig(ctr, oldName, ctr.config.Name, ctr.config); err != nil {
		// Assume the rename failed.
		// Set config back to the old name so reflect what is actually
		// present in the DB.
		ctr.config.Name = oldName
		return nil, fmt.Errorf("renaming container %s: %w", ctr.ID(), err)
	}

	// Step 3: rename the container in c/storage.
	// This can fail if the name is already in use by a non-Podman
	// container. This puts us in a bad spot - we've already renamed the
	// container in Podman. We can swap the order, but then we have the
	// opposite problem. Atomicity is a real problem here, with no easy
	// solution.
	if err := r.store.SetNames(ctr.ID(), []string{ctr.Name()}); err != nil {
		return nil, err
	}

	ctr.newContainerEvent(events.Rename)
	return ctr, nil
}

func (r *Runtime) initContainerVariables(rSpec *spec.Spec, config *ContainerConfig) (*Container, error) {
	if rSpec == nil {
		return nil, fmt.Errorf("must provide a valid runtime spec to create container: %w", define.ErrInvalidArg)
	}
	ctr := new(Container)
	ctr.config = new(ContainerConfig)
	ctr.state = new(ContainerState)

	if config == nil {
		ctr.config.ID = stringid.GenerateRandomID()
		size, err := units.FromHumanSize(r.config.Containers.ShmSize)
		if useDevShm {
			if err != nil {
				return nil, fmt.Errorf("converting containers.conf ShmSize %s to an int: %w", r.config.Containers.ShmSize, err)
			}
			ctr.config.ShmSize = size
			ctr.config.NoShm = false
			ctr.config.NoShmShare = false
		} else {
			ctr.config.NoShm = true
			ctr.config.NoShmShare = true
		}
		ctr.config.StopSignal = 15

		ctr.config.StopTimeout = r.config.Engine.StopTimeout
	} else {
		// This is a restore from an imported checkpoint
		ctr.restoreFromCheckpoint = true
		if err := JSONDeepCopy(config, ctr.config); err != nil {
			return nil, fmt.Errorf("copying container config for restore: %w", err)
		}
		// If the ID is empty a new name for the restored container was requested
		if ctr.config.ID == "" {
			ctr.config.ID = stringid.GenerateRandomID()
		}
		// Reset the log path to point to the default
		ctr.config.LogPath = ""
		// Later in validate() the check is for nil. JSONDeepCopy sets it to an empty
		// object. Resetting it to nil if it was nil before.
		if config.StaticMAC == nil {
			ctr.config.StaticMAC = nil
		}
	}

	ctr.config.Spec = rSpec
	ctr.config.CreatedTime = time.Now()

	ctr.state.BindMounts = make(map[string]string)

	ctr.config.OCIRuntime = r.defaultOCIRuntime.Name()

	// Set namespace based on current runtime namespace
	// Do so before options run so they can override it
	if r.config.Engine.Namespace != "" {
		ctr.config.Namespace = r.config.Engine.Namespace
	}

	ctr.runtime = r

	return ctr, nil
}

func (r *Runtime) newContainer(ctx context.Context, rSpec *spec.Spec, options ...CtrCreateOption) (*Container, error) {
	var ctr *Container
	var err error

	ctr, err = r.initContainerVariables(rSpec, nil)

	if err != nil {
		return nil, fmt.Errorf("initializing container variables: %w", err)
	}

	for _, option := range options {
		if err := option(ctr); err != nil {
			return nil, fmt.Errorf("running container create option: %w", err)
		}
	}

	return r.setupContainer(ctx, ctr)
}

func (r *Runtime) setupContainer(ctx context.Context, ctr *Container) (_ *Container, retErr error) {
	if ctr.IsDefaultInfra() || ctr.IsService() {
		err := ctr.createInitRootfs()
		if err != nil {
			return nil, err
		}
		_, err = ctr.prepareCatatonitMount()
		if err != nil {
			return nil, err
		}
	}

	// normalize the networks to names
	// the db backend only knows about network names so we have to make
	// sure we do not use ids internally
	if len(ctr.config.Networks) > 0 {
		normalizeNetworks := make(map[string]types.PerNetworkOptions, len(ctr.config.Networks))
		// first get the already used interface names so we do not conflict
		usedIfNames := make([]string, 0, len(ctr.config.Networks))
		for _, opts := range ctr.config.Networks {
			if opts.InterfaceName != "" {
				// check that no name is assigned to more than network
				if slices.Contains(usedIfNames, opts.InterfaceName) {
					return nil, fmt.Errorf("network interface name %q is already assigned to another network", opts.InterfaceName)
				}
				usedIfNames = append(usedIfNames, opts.InterfaceName)
			}
		}
		i := 0
		for nameOrID, opts := range ctr.config.Networks {
			netName, nicName, err := r.normalizeNetworkName(nameOrID)
			if err != nil {
				return nil, err
			}

			// check whether interface is to be named as the network_interface
			// when name left unspecified
			if opts.InterfaceName == "" {
				opts.InterfaceName = nicName
			}

			// assign default interface name if empty
			if opts.InterfaceName == "" {
				for i < 100000 {
					ifName := fmt.Sprintf("eth%d", i)
					if !slices.Contains(usedIfNames, ifName) {
						opts.InterfaceName = ifName
						usedIfNames = append(usedIfNames, ifName)
						break
					}
					i++
				}
				// if still empty we did not find a free name
				if opts.InterfaceName == "" {
					return nil, errors.New("failed to find free network interface name")
				}
			}
			opts.Aliases = append(opts.Aliases, getExtraNetworkAliases(ctr)...)

			normalizeNetworks[netName] = opts
		}
		ctr.config.Networks = normalizeNetworks
	}

	// Validate the container
	if err := ctr.validate(); err != nil {
		return nil, err
	}
	if ctr.config.IsInfra {
		ctr.config.StopTimeout = 10
	}

	// Inhibit shutdown until creation succeeds
	shutdown.Inhibit()
	defer shutdown.Uninhibit()

	// Allocate a lock for the container
	lock, err := r.lockManager.AllocateLock()
	if err != nil {
		return nil, fmt.Errorf("allocating lock for new container: %w", err)
	}
	ctr.lock = lock
	ctr.config.LockID = ctr.lock.ID()
	logrus.Debugf("Allocated lock %d for container %s", ctr.lock.ID(), ctr.ID())

	defer func() {
		if retErr != nil {
			if err := ctr.lock.Free(); err != nil {
				logrus.Errorf("Freeing lock for container after creation failed: %v", err)
			}
		}
	}()

	ctr.valid = true
	ctr.state.State = define.ContainerStateConfigured
	ctr.runtime = r

	if ctr.config.OCIRuntime == "" {
		ctr.ociRuntime = r.defaultOCIRuntime
	} else {
		ociRuntime, ok := r.ociRuntimes[ctr.config.OCIRuntime]
		if !ok {
			return nil, fmt.Errorf("requested OCI runtime %s is not available: %w", ctr.config.OCIRuntime, define.ErrInvalidArg)
		}
		ctr.ociRuntime = ociRuntime
	}

	// Check NoCgroups support
	if ctr.config.NoCgroups {
		if !ctr.ociRuntime.SupportsNoCgroups() {
			return nil, fmt.Errorf("requested OCI runtime %s is not compatible with NoCgroups: %w", ctr.ociRuntime.Name(), define.ErrInvalidArg)
		}
	}

	var pod *Pod
	if ctr.config.Pod != "" {
		// Get the pod from state
		pod, err = r.state.Pod(ctr.config.Pod)
		if err != nil {
			return nil, fmt.Errorf("cannot add container %s to pod %s: %w", ctr.ID(), ctr.config.Pod, err)
		}
	}

	// Check Cgroup parent sanity, and set it if it was not set.
	// Only if we're actually configuring Cgroups.
	if !ctr.config.NoCgroups {
		ctr.config.CgroupManager = r.config.Engine.CgroupManager
		switch r.config.Engine.CgroupManager {
		case config.CgroupfsCgroupsManager:
			if ctr.config.CgroupParent == "" {
				if pod != nil && pod.config.UsePodCgroup && !ctr.IsInfra() {
					podCgroup, err := pod.CgroupPath()
					if err != nil {
						return nil, fmt.Errorf("retrieving pod %s cgroup: %w", pod.ID(), err)
					}
					expectPodCgroup, err := ctr.expectPodCgroup()
					if err != nil {
						return nil, err
					}
					if expectPodCgroup && podCgroup == "" {
						return nil, fmt.Errorf("pod %s cgroup is not set: %w", pod.ID(), define.ErrInternal)
					}
					canUseCgroup := !rootless.IsRootless() || isRootlessCgroupSet(podCgroup)
					if canUseCgroup {
						ctr.config.CgroupParent = podCgroup
					}
				} else if !rootless.IsRootless() {
					ctr.config.CgroupParent = CgroupfsDefaultCgroupParent
				}
			} else if strings.HasSuffix(path.Base(ctr.config.CgroupParent), ".slice") {
				return nil, fmt.Errorf("systemd slice received as cgroup parent when using cgroupfs: %w", define.ErrInvalidArg)
			}
		case config.SystemdCgroupsManager:
			if ctr.config.CgroupParent == "" {
				switch {
				case pod != nil && pod.config.UsePodCgroup && !ctr.IsInfra():
					podCgroup, err := pod.CgroupPath()
					if err != nil {
						return nil, fmt.Errorf("retrieving pod %s cgroup: %w", pod.ID(), err)
					}
					expectPodCgroup, err := ctr.expectPodCgroup()
					if err != nil {
						return nil, err
					}
					if expectPodCgroup && podCgroup == "" {
						return nil, fmt.Errorf("pod %s cgroup is not set: %w", pod.ID(), define.ErrInternal)
					}
					ctr.config.CgroupParent = podCgroup
				case rootless.IsRootless() && ctr.config.CgroupsMode != cgroupSplit:
					ctr.config.CgroupParent = SystemdDefaultRootlessCgroupParent
				case ctr.config.CgroupsMode != cgroupSplit:
					ctr.config.CgroupParent = SystemdDefaultCgroupParent
				}
			} else if len(ctr.config.CgroupParent) < 6 || !strings.HasSuffix(path.Base(ctr.config.CgroupParent), ".slice") {
				return nil, fmt.Errorf("did not receive systemd slice as cgroup parent when using systemd to manage cgroups: %w", define.ErrInvalidArg)
			}
		default:
			return nil, fmt.Errorf("unsupported Cgroup manager: %s - cannot validate cgroup parent: %w", r.config.Engine.CgroupManager, define.ErrInvalidArg)
		}
	}

	if ctr.config.Timezone == "" {
		ctr.config.Timezone = r.config.Containers.TZ
	}

	if ctr.restoreFromCheckpoint {
		// Remove information about bind mount
		// for new container from imported checkpoint
		// NewFromSpec() is deprecated according to its comment
		// however the recommended replace just causes a nil map panic
		g := generate.NewFromSpec(ctr.config.Spec)
		g.RemoveMount("/dev/shm")
		ctr.config.ShmDir = ""
		g.RemoveMount("/etc/resolv.conf")
		g.RemoveMount("/etc/hostname")
		g.RemoveMount("/etc/hosts")
		g.RemoveMount("/run/.containerenv")
		g.RemoveMount("/run/secrets")
		g.RemoveMount("/var/run/.containerenv")
		g.RemoveMount("/var/run/secrets")

		// Regenerate Cgroup paths so they don't point to the old
		// container ID.
		cgroupPath, err := ctr.getOCICgroupPath()
		if err != nil {
			return nil, err
		}
		g.SetLinuxCgroupsPath(cgroupPath)
	}

	// Set up storage for the container
	if err := ctr.setupStorage(ctx); err != nil {
		return nil, err
	}
	defer func() {
		if retErr != nil {
			if err := ctr.teardownStorage(); err != nil {
				logrus.Errorf("Removing partially-created container root filesystem: %v", err)
			}
		}
	}()

	ctr.config.SecretsPath = filepath.Join(ctr.config.StaticDir, "secrets")
	err = os.MkdirAll(ctr.config.SecretsPath, 0o755)
	if err != nil {
		return nil, err
	}
	for _, secr := range ctr.config.Secrets {
		err = ctr.extractSecretToCtrStorage(secr)
		if err != nil {
			return nil, err
		}
	}

	if ctr.config.ConmonPidFile == "" {
		ctr.config.ConmonPidFile = filepath.Join(ctr.state.RunDir, "conmon.pid")
	}

	if ctr.config.PidFile == "" {
		ctr.config.PidFile = filepath.Join(ctr.state.RunDir, "pidfile")
	}

	// Go through named volumes and add them.
	// If they don't exist they will be created using basic options.
	for _, vol := range ctr.config.NamedVolumes {
		isAnonymous := false
		if vol.Name == "" {
			// Anonymous volume. We'll need to create it.
			// It needs a name first.
			vol.Name = stringid.GenerateRandomID()
			isAnonymous = true
		} else {
			// Check if it already exists
			_, err := r.state.Volume(vol.Name)
			if err == nil {
				// The volume exists, we're good
				// Make sure to drop all volume-opt options as they only apply to
				// the volume create which we don't do again.
				var volOpts []string
				for _, opts := range vol.Options {
					if !strings.HasPrefix(opts, "volume-opt") {
						volOpts = append(volOpts, opts)
					}
				}
				vol.Options = volOpts
				continue
			} else if !errors.Is(err, define.ErrNoSuchVolume) {
				return nil, fmt.Errorf("retrieving named volume %s for new container: %w", vol.Name, err)
			}
		}
		if vol.IsAnonymous {
			// If SetAnonymous is true, make this an anonymous volume
			// this is needed for emptyDir volumes from kube yamls
			isAnonymous = true
		}

		logrus.Debugf("Creating new volume %s for container", vol.Name)

		// The volume does not exist, so we need to create it.
		volOptions := []VolumeCreateOption{
			WithVolumeName(vol.Name),
			WithVolumeMountLabel(ctr.MountLabel()),
		}
		if isAnonymous {
			volOptions = append(volOptions, withSetAnon())
		}

		// If volume-opts are set, parse and add driver opts.
		if len(vol.Options) > 0 {
			isDriverOpts := false
			driverOpts := make(map[string]string)
			var volOpts []string
			for _, opts := range vol.Options {
				if strings.HasPrefix(opts, "volume-opt") {
					isDriverOpts = true
					driverOptKey, driverOptValue, err := util.ParseDriverOpts(opts)
					if err != nil {
						return nil, err
					}
					driverOpts[driverOptKey] = driverOptValue
				} else {
					volOpts = append(volOpts, opts)
				}
			}
			vol.Options = volOpts
			if isDriverOpts {
				parsedOptions := []VolumeCreateOption{WithVolumeOptions(driverOpts)}
				volOptions = append(volOptions, parsedOptions...)
			}
		}

		volOptions = append(volOptions, WithVolumeUID(ctr.RootUID()), WithVolumeGID(ctr.RootGID()))

		_, err = r.newVolume(ctx, false, volOptions...)
		if err != nil {
			return nil, fmt.Errorf("creating named volume %q: %w", vol.Name, err)
		}
	}

	switch ctr.config.LogDriver {
	case define.NoLogging, define.PassthroughLogging, define.JournaldLogging:
		break
	default:
		if ctr.config.LogPath == "" {
			ctr.config.LogPath = filepath.Join(ctr.config.StaticDir, "ctr.log")
		}
	}

	if useDevShm && !MountExists(ctr.config.Spec.Mounts, "/dev/shm") && ctr.config.ShmDir == "" && !ctr.config.NoShm {
		ctr.config.ShmDir = filepath.Join(ctr.bundlePath(), "shm")
		if err := os.MkdirAll(ctr.config.ShmDir, 0o700); err != nil {
			if !os.IsExist(err) {
				return nil, fmt.Errorf("unable to create shm dir: %w", err)
			}
		}
		ctr.config.Mounts = append(ctr.config.Mounts, ctr.config.ShmDir)
	}

	// Add the container to the state
	// TODO: May be worth looking into recovering from name/ID collisions here
	if ctr.config.Pod != "" {
		// Lock the pod to ensure we can't add containers to pods
		// being removed
		pod.lock.Lock()
		defer pod.lock.Unlock()

		if err := r.state.AddContainerToPod(pod, ctr); err != nil {
			return nil, err
		}
	} else if err := r.state.AddContainer(ctr); err != nil {
		return nil, err
	}

	if ctr.runtime.config.Engine.EventsContainerCreateInspectData {
		if err := ctr.newContainerEventWithInspectData(events.Create, define.HealthCheckResults{}, true); err != nil {
			return nil, err
		}
	} else {
		ctr.newContainerEvent(events.Create)
	}
	return ctr, nil
}

// RemoveContainer removes the given container. If force is true, the container
// will be stopped first (otherwise, an error will be returned if the container
// is running). If removeVolume is specified, anonymous named volumes used by the
// container will be removed also (iff the container is the sole user of the
// volumes). Timeout sets the stop timeout for the container if it is running.
func (r *Runtime) RemoveContainer(ctx context.Context, c *Container, force bool, removeVolume bool, timeout *uint) error {
	opts := ctrRmOpts{
		Force:        force,
		RemoveVolume: removeVolume,
		Timeout:      timeout,
	}

	// NOTE: container will be locked down the road. There is no unlocked
	// version of removeContainer.
	_, _, err := r.removeContainer(ctx, c, opts)
	return err
}

// RemoveContainerAndDependencies removes the given container and all its
// dependencies. This may include pods (if the container or any of its
// dependencies is an infra or service container, the associated pod(s) will also
// be removed). Otherwise, it functions identically to RemoveContainer.
// Returns two arrays: containers removed, and pods removed. These arrays are
// always returned, even if error is set, and indicate any containers that were
// successfully removed prior to the error.
func (r *Runtime) RemoveContainerAndDependencies(ctx context.Context, c *Container, force bool, removeVolume bool, timeout *uint) (map[string]error, map[string]error, error) {
	opts := ctrRmOpts{
		Force:        force,
		RemoveVolume: removeVolume,
		RemoveDeps:   true,
		Timeout:      timeout,
	}

	// NOTE: container will be locked down the road. There is no unlocked
	// version of removeContainer.
	return r.removeContainer(ctx, c, opts)
}

// Options for removeContainer
type ctrRmOpts struct {
	// Whether to stop running container(s)
	Force bool
	// Whether to remove anonymous volumes used by removing the container
	RemoveVolume bool
	// Only set by `removePod` as `removeContainer` is being called as part
	// of removing a whole pod.
	RemovePod bool
	// Whether to ignore dependencies of the container when removing
	// (This is *DANGEROUS* and should not be used outside of non-graph
	// traversal pod removal code).
	IgnoreDeps bool
	// Remove all the dependencies associated with the container. Can cause
	// multiple containers, and possibly one or more pods, to be removed.
	RemoveDeps bool
	// Do not lock the pod that the container is part of (used only by
	// recursive calls of removeContainer, used when removing dependencies)
	NoLockPod bool
	// Timeout to use when stopping the container. Only used if `Force` is
	// true.
	Timeout *uint
}

// Internal function to remove a container.
// Locks the container, but does not lock the runtime.
// removePod is used only when removing pods. It instructs Podman to ignore
// infra container protections, and *not* remove from the database (as pod
// remove will handle that).
// ignoreDeps is *DANGEROUS* and should not be used outside of a very specific
// context (alternate pod removal code, where graph traversal is not possible).
// removeDeps instructs Podman to remove dependency containers (and possible
// a dependency pod if an infra container is involved). removeDeps conflicts
// with removePod - pods have their own dependency management.
// noLockPod is used for recursive removeContainer calls when the pod is already
// locked.
// TODO: At this point we should just start accepting an options struct
func (r *Runtime) removeContainer(ctx context.Context, c *Container, opts ctrRmOpts) (removedCtrs map[string]error, removedPods map[string]error, retErr error) {
	removedCtrs = make(map[string]error)
	removedPods = make(map[string]error)

	if !c.valid {
		if ok, _ := r.state.HasContainer(c.ID()); !ok {
			// Container probably already removed
			// Or was never in the runtime to begin with
			removedCtrs[c.ID()] = nil
			return
		}
	}

	if opts.RemovePod && opts.RemoveDeps {
		retErr = fmt.Errorf("cannot remove dependencies while also removing a pod: %w", define.ErrInvalidArg)
		return
	}

	// We need to refresh container config from the DB, to ensure that any
	// changes (e.g. a rename) are picked up before we start removing.
	// Since HasContainer above succeeded, we can safely assume the
	// container exists.
	// This is *very iffy* but it should be OK because the container won't
	// exist once we're done.
	newConf, err := r.state.GetContainerConfig(c.ID())
	if err != nil {
		retErr = fmt.Errorf("retrieving container %s configuration from DB to remove: %w", c.ID(), err)
		return
	}
	c.config = newConf

	logrus.Debugf("Removing container %s", c.ID())

	// We need to lock the pod before we lock the container.
	// To avoid races around removing a container and the pod it is in.
	// Don't need to do this in pod removal case - we're evicting the entire
	// pod.
	var pod *Pod
	runtime := c.runtime
	if c.config.Pod != "" {
		pod, err = r.state.Pod(c.config.Pod)
		if err != nil {
			// There's a potential race here where the pod we are in
			// was already removed.
			// If so, this container is also removed, as pods take
			// all their containers with them.
			// So if it's already gone, check if we are too.
			if errors.Is(err, define.ErrNoSuchPod) {
				// We could check the DB to see if we still
				// exist, but that would be a serious violation
				// of DB integrity.
				// Mark this container as removed so there's no
				// confusion, though.
				removedCtrs[c.ID()] = nil
				return
			}

			retErr = err
			return
		}

		if !opts.RemovePod {
			// Lock the pod while we're removing container
			if pod.config.LockID == c.config.LockID {
				retErr = fmt.Errorf("container %s and pod %s share lock ID %d: %w", c.ID(), pod.ID(), c.config.LockID, define.ErrWillDeadlock)
				return
			}
			if !opts.NoLockPod {
				pod.lock.Lock()
				defer pod.lock.Unlock()
			}
			if err := pod.updatePod(); err != nil {
				// As above, there's a chance the pod was
				// already removed.
				if errors.Is(err, define.ErrNoSuchPod) {
					removedCtrs[c.ID()] = nil
					return
				}

				retErr = err
				return
			}

			infraID := pod.state.InfraContainerID
			if c.ID() == infraID && !opts.RemoveDeps {
				retErr = fmt.Errorf("container %s is the infra container of pod %s and cannot be removed without removing the pod", c.ID(), pod.ID())
				return
			}
		}
	}

	// For pod removal, the container is already locked by the caller
	locked := false
	if !opts.RemovePod {
		c.lock.Lock()
		defer func() {
			if locked {
				c.lock.Unlock()
			}
		}()
		locked = true
	}

	if !r.valid {
		retErr = define.ErrRuntimeStopped
		return
	}

	// Update the container to get current state
	if err := c.syncContainer(); err != nil {
		retErr = err
		return
	}

	serviceForPod := false
	if c.IsService() {
		for _, id := range c.state.Service.Pods {
			depPod, err := c.runtime.LookupPod(id)
			if err != nil {
				if errors.Is(err, define.ErrNoSuchPod) {
					continue
				}
				retErr = err
				return
			}
			if !opts.RemoveDeps {
				retErr = fmt.Errorf("container %s is the service container of pod(s) %s and cannot be removed without removing the pod(s)", c.ID(), strings.Join(c.state.Service.Pods, ","))
				return
			}
			// If we are the service container for the pod we are a
			// member of: we need to remove that pod last, since
			// this container is part of it.
			if pod != nil && pod.ID() == depPod.ID() {
				serviceForPod = true
				continue
			}
			logrus.Infof("Removing pod %s as container %s is its service container", depPod.ID(), c.ID())
			podRemovedCtrs, err := r.RemovePod(ctx, depPod, true, opts.Force, opts.Timeout)
			maps.Copy(removedCtrs, podRemovedCtrs)
			if err != nil && !errors.Is(err, define.ErrNoSuchPod) && !errors.Is(err, define.ErrPodRemoved) {
				removedPods[depPod.ID()] = err
				retErr = fmt.Errorf("error removing container %s dependency pods: %w", c.ID(), err)
				return
			}
			removedPods[depPod.ID()] = nil
		}
	}
	if (serviceForPod || c.config.IsInfra) && !opts.RemovePod {
		// We're going to remove the pod we are a part of.
		// This will get rid of us as well, so we can just return
		// immediately after.
		if locked {
			locked = false
			c.lock.Unlock()
		}

		logrus.Infof("Removing pod %s (dependency of container %s)", pod.ID(), c.ID())
		podRemovedCtrs, err := r.removePod(ctx, pod, true, opts.Force, opts.Timeout)
		maps.Copy(removedCtrs, podRemovedCtrs)
		if err != nil && !errors.Is(err, define.ErrNoSuchPod) && !errors.Is(err, define.ErrPodRemoved) {
			removedPods[pod.ID()] = err
			retErr = fmt.Errorf("error removing container %s pod: %w", c.ID(), err)
			return
		}
		removedPods[pod.ID()] = nil
		return
	}

	// If we're not force-removing, we need to check if we're in a good
	// state to remove.
	if !opts.Force {
		if err := c.checkReadyForRemoval(); err != nil {
			retErr = err
			return
		}
	}

	if c.state.State == define.ContainerStatePaused {
		isV2, err := cgroups.IsCgroup2UnifiedMode()
		if err != nil {
			retErr = err
			return
		}
		// cgroups v1 and v2 handle signals on paused processes differently
		if !isV2 {
			if err := c.unpause(); err != nil {
				retErr = err
				return
			}
		}
		if err := c.ociRuntime.KillContainer(c, 9, false); err != nil {
			retErr = err
			return
		}
		// Need to update container state to make sure we know it's stopped
		if err := c.waitForExitFileAndSync(); err != nil {
			retErr = err
			return
		}
	}

	// Check that no other containers depend on the container.
	// Only used if not removing a pod - pods guarantee that all
	// deps will be evicted at the same time.
	if !opts.IgnoreDeps {
		deps, err := r.state.ContainerInUse(c)
		if err != nil {
			retErr = err
			return
		}
		if !opts.RemoveDeps {
			if len(deps) != 0 {
				depsStr := strings.Join(deps, ", ")
				retErr = fmt.Errorf("container %s has dependent containers which must be removed before it: %s: %w", c.ID(), depsStr, define.ErrCtrExists)
				return
			}
		}
		for _, depCtr := range deps {
			dep, err := r.GetContainer(depCtr)
			if err != nil {
				retErr = err
				return
			}
			logrus.Infof("Removing container %s (dependency of container %s)", dep.ID(), c.ID())
			recursiveOpts := ctrRmOpts{
				Force:        opts.Force,
				RemoveVolume: opts.RemoveVolume,
				RemoveDeps:   true,
				NoLockPod:    true,
				Timeout:      opts.Timeout,
			}
			ctrs, pods, err := r.removeContainer(ctx, dep, recursiveOpts)
			for rmCtr, err := range ctrs {
				if errors.Is(err, define.ErrNoSuchCtr) || errors.Is(err, define.ErrCtrRemoved) {
					removedCtrs[rmCtr] = nil
				} else {
					removedCtrs[rmCtr] = err
				}
			}
			maps.Copy(removedPods, pods)
			if err != nil && !errors.Is(err, define.ErrNoSuchCtr) && !errors.Is(err, define.ErrCtrRemoved) {
				retErr = err
				return
			}
		}
	}

	// Check that the container's in a good state to be removed.
	if c.ensureState(define.ContainerStateRunning, define.ContainerStateStopping) {
		time := c.StopTimeout()
		if opts.Timeout != nil {
			time = *opts.Timeout
		}
		// Ignore ErrConmonDead - we couldn't retrieve the container's
		// exit code properly, but it's still stopped.
		if err := c.stop(time); err != nil && !errors.Is(err, define.ErrConmonDead) {
			retErr = fmt.Errorf("cannot remove container %s as it could not be stopped: %w", c.ID(), err)
			return
		}

		// We unlocked as part of stop() above - there's a chance someone
		// else got in and removed the container before we reacquired the
		// lock.
		// Do a quick ping of the database to check if the container
		// still exists.
		if ok, _ := r.state.HasContainer(c.ID()); !ok {
			// When the container has already been removed, the OCI runtime directory remains.
			if err := c.cleanupRuntime(ctx); err != nil {
				retErr = fmt.Errorf("cleaning up container %s from OCI runtime: %w", c.ID(), err)
				return
			}
			// Do not add to removed containers, someone else
			// removed it.
			return
		}
	}

	reportErrorf := func(msg string, args ...any) {
		err := fmt.Errorf(msg, args...) // Always use fmt.Errorf instead of just logrus.Errorf(…) because the format string probably contains %w
		if retErr == nil {
			retErr = err
		} else {
			logrus.Errorf("%s", err.Error())
		}
	}

	// Clean up network namespace, cgroups, mounts.
	// Do this before we set ContainerStateRemoving, to ensure that we can
	// actually remove from the OCI runtime.
	if err := c.cleanup(ctx); err != nil {
		reportErrorf("cleaning up container %s: %w", c.ID(), err)
	}

	// Remove all active exec sessions
	// removing the exec sessions might temporarily unlock the container's lock.  Using it
	// after setting the state to ContainerStateRemoving will prevent that the container is
	// restarted
	if err := c.removeAllExecSessions(); err != nil {
		reportErrorf("removing exec sessions: %w", err)
	}

	// Set ContainerStateRemoving as an intermediate state (we may get
	// killed at any time) and save the container.
	c.state.State = define.ContainerStateRemoving

	if err := c.save(); err != nil {
		if !errors.Is(err, define.ErrCtrRemoved) {
			reportErrorf("saving container: %w", err)
		}
	}

	// Stop the container's storage
	if err := c.teardownStorage(); err != nil {
		reportErrorf("cleaning up storage: %w", err)
	}

	// Remove the container from the state
	if c.config.Pod != "" {
		// If we're removing the pod, the container will be evicted
		// from the state elsewhere
		if err := r.state.RemoveContainerFromPod(pod, c); err != nil {
			reportErrorf("removing container %s from database: %w", c.ID(), err)
		}
	} else {
		if err := r.state.RemoveContainer(c); err != nil {
			reportErrorf("removing container %s from database: %w", c.ID(), err)
		}
	}
	removedCtrs[c.ID()] = nil

	// Remove the container's CID file on container removal.
	if cidFile, ok := c.config.Spec.Annotations[define.InspectAnnotationCIDFile]; ok {
		if err := os.Remove(cidFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			reportErrorf("cleaning up CID file: %w", err)
		}
	}

	// Deallocate the container's lock
	if err := c.lock.Free(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		reportErrorf("freeing lock for container %s: %w", c.ID(), err)
	}

	// Set container as invalid so it can no longer be used
	c.valid = false

	c.newContainerEvent(events.Remove)

	if !opts.RemoveVolume {
		return
	}

	for _, v := range c.config.NamedVolumes {
		if volume, err := runtime.state.Volume(v.Name); err == nil {
			if !volume.Anonymous() {
				continue
			}
			if err := runtime.removeVolume(ctx, volume, false, opts.Timeout, false); err != nil && !errors.Is(err, define.ErrNoSuchVolume) {
				if errors.Is(err, define.ErrVolumeBeingUsed) {
					// Ignore error, since podman will report original error
					volumesFrom, _ := c.volumesFrom()
					if len(volumesFrom) > 0 {
						logrus.Debugf("Cleaning up volume not possible since volume is in use (%s)", v.Name)
						continue
					}
				}
				logrus.Errorf("Cleaning up volume (%s): %v", v.Name, err)
			}
		}
	}

	//nolint:nakedret
	return
}

// EvictContainer removes the given container partial or full ID or name, and
// returns the full ID of the evicted container and any error encountered.
// It should be used to remove a container when obtaining a Container struct
// pointer has failed.
// Running container will not be stopped.
// If removeVolume is specified, named volumes used by the container will
// be removed also if and only if the container is the sole user.
func (r *Runtime) EvictContainer(ctx context.Context, idOrName string, removeVolume bool) (string, error) {
	return r.evictContainer(ctx, idOrName, removeVolume)
}

// evictContainer is the internal function to handle container eviction based
// on its partial or full ID or name.
// It returns the full ID of the evicted container and any error encountered.
// This does not lock the runtime nor the container.
// removePod is used only when removing pods. It instructs Podman to ignore
// infra container protections, and *not* remove from the database (as pod
// remove will handle that).
func (r *Runtime) evictContainer(ctx context.Context, idOrName string, removeVolume bool) (string, error) {
	var err error
	var timeout *uint

	if !r.valid {
		return "", define.ErrRuntimeStopped
	}

	id, err := r.state.LookupContainerID(idOrName)
	if err != nil {
		return "", err
	}

	// Begin by trying a normal removal. Valid containers will be removed normally.
	tmpCtr, err := r.state.Container(id)
	if err == nil {
		logrus.Infof("Container %s successfully retrieved from state, attempting normal removal", id)
		// Assume force = true for the evict case
		opts := ctrRmOpts{
			Force:        true,
			RemoveVolume: removeVolume,
			Timeout:      timeout,
		}
		_, _, err = r.removeContainer(ctx, tmpCtr, opts)
		if !tmpCtr.valid {
			// If the container is marked invalid, remove succeeded
			// in kicking it out of the state - no need to continue.
			return id, err
		}

		if err == nil {
			// Something has gone seriously wrong - no error but
			// container was not removed.
			logrus.Errorf("Container %s not removed with no error", id)
		} else {
			logrus.Warnf("Failed to removal container %s normally, proceeding with evict: %v", id, err)
		}
	}

	// Error out if the container does not exist in libpod
	exists, err := r.state.HasContainer(id)
	if err != nil {
		return id, err
	}
	if !exists {
		return id, err
	}

	// Re-create a container struct for removal purposes
	c := new(Container)
	c.config, err = r.state.GetContainerConfig(id)
	if err != nil {
		return id, fmt.Errorf("failed to retrieve config for ctr ID %q: %w", id, err)
	}
	c.state = new(ContainerState)

	// We need to lock the pod before we lock the container.
	// To avoid races around removing a container and the pod it is in.
	// Don't need to do this in pod removal case - we're evicting the entire
	// pod.
	var pod *Pod
	if c.config.Pod != "" {
		pod, err = r.state.Pod(c.config.Pod)
		if err != nil {
			return id, fmt.Errorf("container %s is in pod %s, but pod cannot be retrieved: %w", c.ID(), pod.ID(), err)
		}

		// Lock the pod while we're removing container
		pod.lock.Lock()
		defer pod.lock.Unlock()
		if err := pod.updatePod(); err != nil {
			return id, err
		}

		infraID, err := pod.infraContainerID()
		if err != nil {
			return "", err
		}
		if c.ID() == infraID {
			return id, fmt.Errorf("container %s is the infra container of pod %s and cannot be removed without removing the pod", c.ID(), pod.ID())
		}
	}

	if c.IsService() {
		report, err := c.canStopServiceContainer()
		if err != nil {
			return id, err
		}
		if !report.canBeStopped {
			return id, fmt.Errorf("container %s is the service container of pod(s) %s and cannot be removed without removing the pod(s)", c.ID(), strings.Join(c.state.Service.Pods, ","))
		}
	}

	var cleanupErr error
	// Remove the container from the state
	if c.config.Pod != "" {
		// If we're removing the pod, the container will be evicted
		// from the state elsewhere
		if err := r.state.RemoveContainerFromPod(pod, c); err != nil {
			cleanupErr = err
		}
	} else {
		if err := r.state.RemoveContainer(c); err != nil {
			cleanupErr = err
		}
	}

	// Unmount container mount points
	for _, mount := range c.config.Mounts {
		Unmount(mount)
	}

	// Remove container from c/storage
	if err := r.RemoveStorageContainer(id, true); err != nil {
		if cleanupErr == nil {
			cleanupErr = err
		}
	}

	if !removeVolume {
		return id, cleanupErr
	}

	for _, v := range c.config.NamedVolumes {
		if volume, err := r.state.Volume(v.Name); err == nil {
			if !volume.Anonymous() {
				continue
			}
			if err := r.removeVolume(ctx, volume, false, timeout, false); err != nil && err != define.ErrNoSuchVolume && err != define.ErrVolumeBeingUsed {
				logrus.Errorf("Cleaning up volume (%s): %v", v.Name, err)
			}
		}
	}

	return id, cleanupErr
}

// GetContainer retrieves a container by its ID
func (r *Runtime) GetContainer(id string) (*Container, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	return r.state.Container(id)
}

// HasContainer checks if a container with the given ID is present
func (r *Runtime) HasContainer(id string) (bool, error) {
	if !r.valid {
		return false, define.ErrRuntimeStopped
	}

	return r.state.HasContainer(id)
}

// LookupContainer looks up a container by its name or a partial ID
// If a partial ID is not unique, an error will be returned
func (r *Runtime) LookupContainer(idOrName string) (*Container, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}
	return r.state.LookupContainer(idOrName)
}

// LookupContainerId looks up a container id by its name or a partial ID
// If a partial ID is not unique, an error will be returned
func (r *Runtime) LookupContainerID(idOrName string) (string, error) {
	if !r.valid {
		return "", define.ErrRuntimeStopped
	}
	return r.state.LookupContainerID(idOrName)
}

// GetContainers retrieves all containers from the state.
// If `loadState` is set, the containers' state will be loaded as well.
// Filters can be provided which will determine what containers are included in
// the output. Multiple filters are handled by ANDing their output, so only
// containers matching all filters are returned
func (r *Runtime) GetContainers(loadState bool, filters ...ContainerFilter) ([]*Container, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	ctrs, err := r.state.AllContainers(loadState)
	if err != nil {
		return nil, err
	}

	ctrsFiltered := applyContainersFilters(ctrs, filters...)

	return ctrsFiltered, nil
}

// Applies container filters on bunch of containers
func applyContainersFilters(containers []*Container, filters ...ContainerFilter) []*Container {
	ctrsFiltered := make([]*Container, 0, len(containers))

	for _, ctr := range containers {
		include := true
		for _, filter := range filters {
			if filter == nil {
				continue
			}
			include = include && filter(ctr)
		}

		if include {
			ctrsFiltered = append(ctrsFiltered, ctr)
		}
	}

	return ctrsFiltered
}

// GetAllContainers is a helper function for GetContainers
func (r *Runtime) GetAllContainers() ([]*Container, error) {
	return r.state.AllContainers(false)
}

// GetRunningContainers is a helper function for GetContainers
func (r *Runtime) GetRunningContainers() ([]*Container, error) {
	running := func(c *Container) bool {
		state, _ := c.State()
		return state == define.ContainerStateRunning
	}
	return r.GetContainers(false, running)
}

// GetContainersByList is a helper function for GetContainers
// which takes a []string of container IDs or names
func (r *Runtime) GetContainersByList(containers []string) ([]*Container, error) {
	ctrs := make([]*Container, 0, len(containers))
	for _, inputContainer := range containers {
		ctr, err := r.LookupContainer(inputContainer)
		if err != nil {
			return ctrs, fmt.Errorf("unable to look up container %s: %w", inputContainer, err)
		}
		ctrs = append(ctrs, ctr)
	}
	return ctrs, nil
}

// GetLatestContainer returns a container object of the latest created container.
func (r *Runtime) GetLatestContainer() (*Container, error) {
	lastCreatedIndex := -1
	var lastCreatedTime time.Time
	ctrs, err := r.GetAllContainers()
	if err != nil {
		return nil, fmt.Errorf("unable to find latest container: %w", err)
	}
	if len(ctrs) == 0 {
		return nil, define.ErrNoSuchCtr
	}
	for containerIndex, ctr := range ctrs {
		createdTime := ctr.config.CreatedTime
		if createdTime.After(lastCreatedTime) {
			lastCreatedTime = createdTime
			lastCreatedIndex = containerIndex
		}
	}
	return ctrs[lastCreatedIndex], nil
}

// GetExecSessionContainer gets the container that a given exec session ID is
// attached to.
func (r *Runtime) GetExecSessionContainer(id string) (*Container, error) {
	if !r.valid {
		return nil, define.ErrRuntimeStopped
	}

	ctrID, err := r.state.GetExecSession(id)
	if err != nil {
		return nil, err
	}

	return r.state.Container(ctrID)
}

// PruneContainers removes stopped and exited containers from localstorage.  A set of optional filters
// can be provided to be more granular.
func (r *Runtime) PruneContainers(filterFuncs []ContainerFilter) ([]*reports.PruneReport, error) {
	preports := make([]*reports.PruneReport, 0)
	// We add getting the exited and stopped containers via a filter
	containerStateFilter := func(c *Container) bool {
		if c.PodID() != "" {
			return false
		}
		state, err := c.State()
		if err != nil {
			logrus.Error(err)
			return false
		}
		if state == define.ContainerStateStopped || state == define.ContainerStateExited ||
			state == define.ContainerStateCreated || state == define.ContainerStateConfigured {
			return true
		}
		return false
	}
	filterFuncs = append(filterFuncs, containerStateFilter)
	delContainers, err := r.GetContainers(false, filterFuncs...)
	if err != nil {
		return nil, err
	}
	for _, c := range delContainers {
		report := new(reports.PruneReport)
		report.Id = c.ID()
		report.Err = nil
		report.Size = 0
		size, err := c.RWSize()
		if err != nil {
			report.Err = err
			preports = append(preports, report)
			continue
		}
		var time *uint
		err = r.RemoveContainer(context.Background(), c, false, false, time)
		if err != nil {
			report.Err = err
		} else {
			report.Size = (uint64)(size)
		}
		preports = append(preports, report)
	}
	return preports, nil
}

// MountStorageContainer mounts the storage container's root filesystem
func (r *Runtime) MountStorageContainer(id string) (string, error) {
	if _, err := r.GetContainer(id); err == nil {
		return "", fmt.Errorf("ctr %s is a libpod container: %w", id, define.ErrCtrExists)
	}
	container, err := r.store.Container(id)
	if err != nil {
		return "", err
	}
	mountPoint, err := r.store.Mount(container.ID, "")
	if err != nil {
		return "", fmt.Errorf("mounting storage for container %s: %w", id, err)
	}
	return mountPoint, nil
}

// UnmountStorageContainer unmounts the storage container's root filesystem
func (r *Runtime) UnmountStorageContainer(id string, force bool) (bool, error) {
	if _, err := r.GetContainer(id); err == nil {
		return false, fmt.Errorf("ctr %s is a libpod container: %w", id, define.ErrCtrExists)
	}
	container, err := r.store.Container(id)
	if err != nil {
		return false, err
	}
	return r.store.Unmount(container.ID, force)
}

// MountedStorageContainer returns whether a storage container is mounted
// along with the mount path
func (r *Runtime) IsStorageContainerMounted(id string) (bool, string, error) {
	var path string
	if _, err := r.GetContainer(id); err == nil {
		return false, "", fmt.Errorf("ctr %s is a libpod container: %w", id, define.ErrCtrExists)
	}

	mountCnt, err := r.storageService.MountedContainerImage(id)
	if err != nil {
		return false, "", fmt.Errorf("get mount count of container: %w", err)
	}
	mounted := mountCnt > 0
	if mounted {
		path, err = r.storageService.GetMountpoint(id)
		if err != nil {
			return false, "", fmt.Errorf("get container mount point: %w", err)
		}
	}
	return mounted, path, nil
}

// StorageContainers returns a list of containers from containers/storage that
// are not currently known to Podman.
func (r *Runtime) StorageContainers() ([]storage.Container, error) {
	if r.store == nil {
		return nil, define.ErrStoreNotInitialized
	}

	storeContainers, err := r.store.Containers()
	if err != nil {
		return nil, fmt.Errorf("reading list of all storage containers: %w", err)
	}
	retCtrs := []storage.Container{}
	for _, container := range storeContainers {
		exists, err := r.state.HasContainer(container.ID)
		if err != nil && err != define.ErrNoSuchCtr {
			return nil, fmt.Errorf("failed to check if %s container exists in database: %w", container.ID, err)
		}
		if exists {
			continue
		}
		retCtrs = append(retCtrs, container)
	}

	return retCtrs, nil
}

func (r *Runtime) IsBuildahContainer(id string) (bool, error) {
	return buildah.IsContainer(id, r.store)
}
