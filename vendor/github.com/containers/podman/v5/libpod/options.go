//go:build !remote

package libpod

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/containers/buildah/pkg/parse"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/containers/podman/v5/pkg/namespaces"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/sirupsen/logrus"
	nettypes "go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/secrets"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/regexp"
)

var umaskRegex = regexp.Delayed(`^[0-7]{1,4}$`)

// WithStorageConfig uses the given configuration to set up container storage.
// If this is not specified, the system default configuration will be used
// instead.
func WithStorageConfig(config storage.StoreOptions) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		setField := false

		if config.RunRoot != "" {
			rt.storageConfig.RunRoot = config.RunRoot
			rt.storageSet.RunRootSet = true
			setField = true
		}

		if config.GraphRoot != "" {
			rt.storageConfig.GraphRoot = config.GraphRoot
			rt.storageSet.GraphRootSet = true
			setField = true
		}

		graphDriverChanged := false
		if config.GraphDriverName != "" {
			rt.storageConfig.GraphDriverName = config.GraphDriverName
			rt.storageSet.GraphDriverNameSet = true
			setField = true
			graphDriverChanged = true
		}

		if config.GraphDriverOptions != nil {
			if graphDriverChanged {
				rt.storageConfig.GraphDriverOptions = make([]string, len(config.GraphDriverOptions))
				copy(rt.storageConfig.GraphDriverOptions, config.GraphDriverOptions)
			} else {
				rt.storageConfig.GraphDriverOptions = config.GraphDriverOptions
			}
			setField = true
		}

		if config.UIDMap != nil {
			rt.storageConfig.UIDMap = make([]idtools.IDMap, len(config.UIDMap))
			copy(rt.storageConfig.UIDMap, config.UIDMap)
		}

		if config.GIDMap != nil {
			rt.storageConfig.GIDMap = make([]idtools.IDMap, len(config.GIDMap))
			copy(rt.storageConfig.GIDMap, config.GIDMap)
		}

		if config.PullOptions != nil {
			rt.storageConfig.PullOptions = maps.Clone(config.PullOptions)
		}

		// If any one of runroot, graphroot, graphdrivername,
		// or graphdriveroptions are set, then GraphRoot and RunRoot
		// must be set
		if setField {
			storeOpts, err := storage.DefaultStoreOptions()
			if err != nil {
				return err
			}
			if rt.storageConfig.GraphRoot == "" {
				rt.storageConfig.GraphRoot = storeOpts.GraphRoot
			}
			if rt.storageConfig.RunRoot == "" {
				rt.storageConfig.RunRoot = storeOpts.RunRoot
			}
		}

		return nil
	}
}

func WithTransientStore(transientStore bool) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		rt.storageConfig.TransientStore = transientStore

		return nil
	}
}

func WithImageStore(imageStore string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		rt.storageConfig.ImageStore = imageStore

		return nil
	}
}

// WithOCIRuntime specifies an OCI runtime to use for running containers.
func WithOCIRuntime(runtime string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		if runtime == "" {
			return fmt.Errorf("must provide a valid path: %w", define.ErrInvalidArg)
		}

		rt.config.Engine.OCIRuntime = runtime

		return nil
	}
}

// WithCtrOCIRuntime specifies an OCI runtime in container's config.
func WithCtrOCIRuntime(runtime string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.OCIRuntime = runtime
		return nil
	}
}

// WithConmonPath specifies the path to the conmon binary which manages the
// runtime.
func WithConmonPath(path string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		if path == "" {
			return fmt.Errorf("must provide a valid path: %w", define.ErrInvalidArg)
		}

		rt.config.Engine.ConmonPath.Set([]string{path})

		return nil
	}
}

// WithNetworkCmdPath specifies the path to the slirp4netns binary which manages the
// runtime.
func WithNetworkCmdPath(path string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		rt.config.Engine.NetworkCmdPath = path

		return nil
	}
}

// WithNetworkBackend specifies the name of the network backend.
func WithNetworkBackend(name string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		rt.config.Network.NetworkBackend = name

		return nil
	}
}

// WithCgroupManager specifies the manager implementation name which is used to
// handle cgroups for containers.
// Current valid values are "cgroupfs" and "systemd".
func WithCgroupManager(manager string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		if manager != config.CgroupfsCgroupsManager && manager != config.SystemdCgroupsManager {
			return fmt.Errorf("cgroup manager must be one of %s and %s: %w",
				config.CgroupfsCgroupsManager, config.SystemdCgroupsManager, define.ErrInvalidArg)
		}

		rt.config.Engine.CgroupManager = manager

		return nil
	}
}

// WithStaticDir sets the directory that static runtime files which persist
// across reboots will be stored.
func WithStaticDir(dir string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		rt.config.Engine.StaticDir = dir

		return nil
	}
}

// WithRegistriesConf configures the runtime to always use specified
// registries.conf for image processing.
func WithRegistriesConf(path string) RuntimeOption {
	logrus.Debugf("Setting custom registries.conf: %q", path)
	return func(rt *Runtime) error {
		if err := fileutils.Exists(path); err != nil {
			return fmt.Errorf("locating specified registries.conf: %w", err)
		}
		if rt.imageContext == nil {
			rt.imageContext = &types.SystemContext{
				BigFilesTemporaryDir: parse.GetTempDir(),
			}
		}

		rt.imageContext.SystemRegistriesConfPath = path
		return nil
	}
}

// WithDatabaseBackend configures the runtime's database backend.
func WithDatabaseBackend(value string) RuntimeOption {
	logrus.Debugf("Setting custom database backend: %q", value)
	return func(rt *Runtime) error {
		// The value will be parsed later on.
		rt.config.Engine.DBBackend = value
		return nil
	}
}

// WithHooksDir sets the directories to look for OCI runtime hook configuration.
func WithHooksDir(hooksDirs ...string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		if slices.Contains(hooksDirs, "") {
			return fmt.Errorf("empty-string hook directories are not supported: %w", define.ErrInvalidArg)
		}

		rt.config.Engine.HooksDir.Set(hooksDirs)
		return nil
	}
}

// WithCDI sets the devices to check for CDI configuration.
func WithCDI(devices []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.CDIDevices = devices
		return nil
	}
}

func WithCDISpecDirs(cdiSpecDirs []string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		rt.config.Engine.CdiSpecDirs.Set(cdiSpecDirs)
		return nil
	}
}

// WithStorageOpts sets the devices to check for CDI configuration.
func WithStorageOpts(storageOpts map[string]string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.StorageOpts = storageOpts
		return nil
	}
}

// WithDefaultMountsFile sets the file to look at for default mounts (mainly
// secrets).
// Note we are not saving this in the database as it is for testing purposes
// only.
func WithDefaultMountsFile(mountsFile string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		if mountsFile == "" {
			return define.ErrInvalidArg
		}
		rt.config.Containers.DefaultMountsFile = mountsFile
		return nil
	}
}

// WithTmpDir sets the directory that temporary runtime files which are not
// expected to survive across reboots will be stored.
// This should be located on a tmpfs mount (/tmp or /run for example).
func WithTmpDir(dir string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}
		rt.config.Engine.TmpDir = dir

		return nil
	}
}

// WithNetworkConfigDir sets the network configuration directory.
func WithNetworkConfigDir(dir string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		rt.config.Network.NetworkConfigDir = dir

		return nil
	}
}

// WithNamespace sets the namespace for libpod.
// Namespaces are used to create scopes to separate containers and pods
// in the state.
// When namespace is set, libpod will only view containers and pods in
// the same namespace. All containers and pods created will default to
// the namespace set here.
// A namespace of "", the empty string, is equivalent to no namespace,
// and all containers and pods will be visible.
func WithNamespace(ns string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		rt.config.Engine.Namespace = ns

		return nil
	}
}

// WithVolumePath sets the path under which all named volumes
// should be created.
// The path changes based on whether the user is running as root or not.
func WithVolumePath(volPath string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		rt.config.Engine.VolumePath = volPath
		rt.storageSet.VolumePathSet = true

		return nil
	}
}

// WithReset tells Libpod that the runtime will be used to perform a system
// reset. A number of checks at initialization are relaxed as the runtime is
// going to be used to remove all containers, pods, volumes, images, and
// networks.
func WithReset() RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		rt.doReset = true

		return nil
	}
}

// WithRenumber tells Libpod that the runtime will be used to perform a system
// renumber. A number of checks on initialization related to locks are relaxed.
func WithRenumber() RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		rt.doRenumber = true

		return nil
	}
}

// WithEventsLogger sets the events backend to use.
// Currently supported values are "file" for file backend and "journald" for
// journald backend.
func WithEventsLogger(logger string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}

		if !events.IsValidEventer(logger) {
			return fmt.Errorf("%q is not a valid events backend: %w", logger, define.ErrInvalidArg)
		}

		rt.config.Engine.EventsLogger = logger
		return nil
	}
}

// WithEnableSDNotify sets a runtime option so we know whether to disable socket/FD
// listening
func WithEnableSDNotify() RuntimeOption {
	return func(rt *Runtime) error {
		rt.config.Engine.SDNotify = true
		return nil
	}
}

// WithSyslog sets a runtime option so we know that we have to log to the syslog as well
func WithSyslog() RuntimeOption {
	return func(rt *Runtime) error {
		rt.syslog = true
		return nil
	}
}

// WithRuntimeFlags adds the global runtime flags to the container config
func WithRuntimeFlags(runtimeFlags []string) RuntimeOption {
	return func(rt *Runtime) error {
		if rt.valid {
			return define.ErrRuntimeFinalized
		}
		rt.runtimeFlags = runtimeFlags
		return nil
	}
}

// Container Creation Options

// WithMaxLogSize sets the maximum size of container logs.
// Positive sizes are limits in bytes, -1 is unlimited.
func WithMaxLogSize(limit int64) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrRuntimeFinalized
		}
		ctr.config.LogSize = limit

		return nil
	}
}

// WithShmDir sets the directory that should be mounted on /dev/shm.
func WithShmDir(dir string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.ShmDir = dir
		return nil
	}
}

// WithNoShm tells libpod whether to mount /dev/shm
func WithNoShm(mount bool) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.NoShm = mount
		return nil
	}
}

// WithNoShmShare tells libpod whether to share containers /dev/shm with other containers
func WithNoShmShare(share bool) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.NoShmShare = share
		return nil
	}
}

// WithSystemd turns on systemd mode in the container
func WithSystemd() CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		t := true
		ctr.config.Systemd = &t
		return nil
	}
}

// WithSdNotifySocket sets the sd-notify of the container
func WithSdNotifySocket(socketPath string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.SdNotifySocket = socketPath
		return nil
	}
}

// WithSdNotifyMode sets the sd-notify method
func WithSdNotifyMode(mode string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		if err := define.ValidateSdNotifyMode(mode); err != nil {
			return err
		}

		ctr.config.SdNotifyMode = mode
		return nil
	}
}

// WithShmSize sets the size of /dev/shm tmpfs mount.
func WithShmSize(size int64) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.ShmSize = size
		return nil
	}
}

// WithShmSizeSystemd sets the size of systemd-specific mounts:
//
//	/run
//	/run/lock
//	/var/log/journal
//	/tmp
func WithShmSizeSystemd(size int64) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.ShmSizeSystemd = size
		return nil
	}
}

// WithPrivileged sets the privileged flag in the container runtime.
func WithPrivileged(privileged bool) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.Privileged = privileged
		return nil
	}
}

// WithReadWriteTmpfs sets up read-write tmpfs flag in the container runtime.
// Only Used if containers are run in ReadOnly mode.
func WithReadWriteTmpfs(readWriteTmpfs bool) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.ReadWriteTmpfs = readWriteTmpfs
		return nil
	}
}

// WithSecLabels sets the labels for SELinux.
func WithSecLabels(labelOpts []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.LabelOpts = labelOpts
		return nil
	}
}

// WithUser sets the user identity field in configuration.
// Valid uses [user | user:group | uid | uid:gid | user:gid | uid:group ].
func WithUser(user string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.User = user
		return nil
	}
}

// WithRootFSFromImage sets up a fresh root filesystem using the given image.
// If useImageConfig is specified, image volumes, environment variables, and
// other configuration from the image will be added to the config.
// TODO: Replace image name and ID with a libpod.Image struct when that is
// finished.
func WithRootFSFromImage(imageID, imageName, rawImageName string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.RootfsImageID = imageID
		ctr.config.RootfsImageName = imageName
		ctr.config.RawImageName = rawImageName
		return nil
	}
}

// WithStdin keeps stdin on the container open to allow interaction.
func WithStdin() CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.Stdin = true

		return nil
	}
}

// WithPod adds the container to a pod.
// Containers which join a pod can only join the Linux namespaces of other
// containers in the same pod.
// Containers can only join pods in the same libpod namespace.
func (r *Runtime) WithPod(pod *Pod) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		if pod == nil {
			return define.ErrInvalidArg
		}
		ctr.config.Pod = pod.ID()

		return nil
	}
}

// WithLabels adds labels to the container.
func WithLabels(labels map[string]string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.Labels = make(map[string]string)
		maps.Copy(ctr.config.Labels, labels)

		return nil
	}
}

// WithName sets the container's name.
func WithName(name string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		name = strings.TrimPrefix(name, "/")
		// Check the name against a regex
		if !define.NameRegex.MatchString(name) {
			return define.RegexError
		}

		ctr.config.Name = name

		return nil
	}
}

// WithStopSignal sets the signal that will be sent to stop the container.
func WithStopSignal(signal syscall.Signal) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		if signal == 0 {
			return fmt.Errorf("stop signal cannot be 0: %w", define.ErrInvalidArg)
		} else if signal > 64 {
			return fmt.Errorf("stop signal cannot be greater than 64 (SIGRTMAX): %w", define.ErrInvalidArg)
		}

		ctr.config.StopSignal = uint(signal)

		return nil
	}
}

// WithStopTimeout sets the time to after initial stop signal is sent to the
// container, before sending the kill signal.
func WithStopTimeout(timeout uint) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.StopTimeout = timeout

		return nil
	}
}

// WithTimeout sets the maximum time a container is allowed to run"
func WithTimeout(timeout uint) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.Timeout = timeout

		return nil
	}
}

// WithIDMappings sets the idmappings for the container
func WithIDMappings(idmappings storage.IDMappingOptions) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.IDMappings = idmappings
		return nil
	}
}

// WithIPCNSFrom indicates that the container should join the IPC namespace of
// the given container.
// If the container has joined a pod, it can only join the namespaces of
// containers in the same pod.
func WithIPCNSFrom(nsCtr *Container) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		if err := checkDependencyContainer(nsCtr, ctr); err != nil {
			return err
		}

		ctr.config.IPCNsCtr = nsCtr.ID()

		return nil
	}
}

// WithNetNSFrom indicates that the container should join the network namespace
// of the given container.
// If the container has joined a pod, it can only join the namespaces of
// containers in the same pod.
func WithNetNSFrom(nsCtr *Container) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		if err := checkDependencyContainer(nsCtr, ctr); err != nil {
			return err
		}

		ctr.config.NetNsCtr = nsCtr.ID()

		return nil
	}
}

// WithPIDNSFrom indicates that the container should join the PID namespace of
// the given container.
// If the container has joined a pod, it can only join the namespaces of
// containers in the same pod.
func WithPIDNSFrom(nsCtr *Container) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		if err := checkDependencyContainer(nsCtr, ctr); err != nil {
			return err
		}

		ctr.config.PIDNsCtr = nsCtr.ID()

		return nil
	}
}

// WithAddCurrentUserPasswdEntry indicates that container should add current
// user entry to /etc/passwd, since the UID will be mapped into the container,
// via user namespace
func WithAddCurrentUserPasswdEntry() CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.AddCurrentUserPasswdEntry = true
		return nil
	}
}

// WithUserNSFrom indicates that the container should join the user namespace of
// the given container.
// If the container has joined a pod, it can only join the namespaces of
// containers in the same pod.
func WithUserNSFrom(nsCtr *Container) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		if err := checkDependencyContainer(nsCtr, ctr); err != nil {
			return err
		}

		ctr.config.UserNsCtr = nsCtr.ID()
		if err := JSONDeepCopy(nsCtr.IDMappings(), &ctr.config.IDMappings); err != nil {
			return err
		}
		// NewFromSpec() is deprecated according to its comment
		// however the recommended replace just causes a nil map panic
		g := generate.NewFromSpec(ctr.config.Spec)

		g.ClearLinuxUIDMappings()
		for _, uidmap := range nsCtr.config.IDMappings.UIDMap {
			g.AddLinuxUIDMapping(uint32(uidmap.HostID), uint32(uidmap.ContainerID), uint32(uidmap.Size))
		}
		g.ClearLinuxGIDMappings()
		for _, gidmap := range nsCtr.config.IDMappings.GIDMap {
			g.AddLinuxGIDMapping(uint32(gidmap.HostID), uint32(gidmap.ContainerID), uint32(gidmap.Size))
		}
		return nil
	}
}

// WithUTSNSFrom indicates that the container should join the UTS namespace of
// the given container.
// If the container has joined a pod, it can only join the namespaces of
// containers in the same pod.
func WithUTSNSFrom(nsCtr *Container) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		if err := checkDependencyContainer(nsCtr, ctr); err != nil {
			return err
		}

		ctr.config.UTSNsCtr = nsCtr.ID()

		return nil
	}
}

// WithCgroupNSFrom indicates that the container should join the Cgroup namespace
// of the given container.
// If the container has joined a pod, it can only join the namespaces of
// containers in the same pod.
func WithCgroupNSFrom(nsCtr *Container) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		if err := checkDependencyContainer(nsCtr, ctr); err != nil {
			return err
		}

		ctr.config.CgroupNsCtr = nsCtr.ID()

		return nil
	}
}

// WithDependencyCtrs sets dependency containers of the given container.
// Dependency containers must be running before this container is started.
func WithDependencyCtrs(ctrs []*Container) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		deps := make([]string, 0, len(ctrs))

		for _, dep := range ctrs {
			if err := checkDependencyContainer(dep, ctr); err != nil {
				return err
			}

			deps = append(deps, dep.ID())
		}

		ctr.config.Dependencies = deps

		return nil
	}
}

// WithNetNS indicates that the container should be given a new network
// namespace with a minimal configuration.
// An optional array of port mappings can be provided.
// Conflicts with WithNetNSFrom().
func WithNetNS(portMappings []nettypes.PortMapping, postConfigureNetNS bool, netmode string, networks map[string]nettypes.PerNetworkOptions) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.PostConfigureNetNS = postConfigureNetNS
		ctr.config.NetMode = namespaces.NetworkMode(netmode)
		ctr.config.CreateNetNS = true
		ctr.config.PortMappings = portMappings

		if !ctr.config.NetMode.IsBridge() && len(networks) > 0 {
			return errors.New("cannot use networks when network mode is not bridge")
		}
		ctr.config.Networks = networks

		return nil
	}
}

// WithExposedPorts includes a set of ports that were exposed by the image in
// the container config, e.g. for display when the container is inspected.
func WithExposedPorts(exposedPorts map[uint16][]string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.ExposedPorts = exposedPorts

		return nil
	}
}

// WithNetworkOptions sets additional options for the networks.
func WithNetworkOptions(options map[string][]string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.NetworkOptions = options

		return nil
	}
}

// WithLogDriver sets the log driver for the container
func WithLogDriver(driver string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		switch driver {
		case "":
			return fmt.Errorf("log driver must be set: %w", define.ErrInvalidArg)
		case define.JournaldLogging, define.KubernetesLogging, define.JSONLogging, define.NoLogging, define.PassthroughLogging, define.PassthroughTTYLogging:
			break
		default:
			return fmt.Errorf("invalid log driver: %w", define.ErrInvalidArg)
		}

		ctr.config.LogDriver = driver

		return nil
	}
}

// WithLogPath sets the path to the log file.
func WithLogPath(path string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		if path == "" {
			return fmt.Errorf("log path must be set: %w", define.ErrInvalidArg)
		}
		if isDirectory(path) {
			containerDir := filepath.Join(path, ctr.ID())
			if err := os.Mkdir(containerDir, 0o755); err != nil {
				return fmt.Errorf("failed to create container log directory %s: %w", containerDir, err)
			}

			ctr.config.LogPath = filepath.Join(containerDir, "ctr.log")
		} else {
			ctr.config.LogPath = path
		}

		return nil
	}
}

// WithLogTag sets the tag to the log file.
func WithLogTag(tag string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		if tag == "" {
			return fmt.Errorf("log tag must be set: %w", define.ErrInvalidArg)
		}

		ctr.config.LogTag = tag

		return nil
	}
}

// WithCgroupsMode disables the creation of Cgroups for the conmon process.
func WithCgroupsMode(mode string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		switch mode {
		case "disabled":
			ctr.config.NoCgroups = true
			ctr.config.CgroupsMode = mode
		case "enabled", "no-conmon", cgroupSplit:
			ctr.config.CgroupsMode = mode
		default:
			return fmt.Errorf("invalid cgroup mode %q: %w", mode, define.ErrInvalidArg)
		}

		return nil
	}
}

// WithCgroupParent sets the Cgroup Parent of the new container.
func WithCgroupParent(parent string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		if parent == "" {
			return fmt.Errorf("cgroup parent cannot be empty: %w", define.ErrInvalidArg)
		}

		ctr.config.CgroupParent = parent

		return nil
	}
}

// WithDNSSearch sets the additional search domains of a container.
func WithDNSSearch(searchDomains []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.DNSSearch = searchDomains
		return nil
	}
}

// WithDNS sets additional name servers for the container.
func WithDNS(dnsServers []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		var dns []net.IP
		for _, i := range dnsServers {
			result := net.ParseIP(i)
			if result == nil {
				return fmt.Errorf("invalid IP address %s: %w", i, define.ErrInvalidArg)
			}
			dns = append(dns, result)
		}
		ctr.config.DNSServer = append(ctr.config.DNSServer, dns...)

		return nil
	}
}

// WithDNSOption sets additional dns options for the container.
func WithDNSOption(dnsOptions []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		if ctr.config.UseImageResolvConf {
			return fmt.Errorf("cannot add DNS options if container will not create /etc/resolv.conf: %w", define.ErrInvalidArg)
		}
		ctr.config.DNSOption = append(ctr.config.DNSOption, dnsOptions...)
		return nil
	}
}

// WithHosts sets additional host:IP for the hosts file.
func WithHosts(hosts []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.HostAdd = hosts
		return nil
	}
}

// WithConmonPidFile specifies the path to the file that receives the pid of
// conmon.
func WithConmonPidFile(path string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.ConmonPidFile = path
		return nil
	}
}

// WithGroups sets additional groups for the container, which are defined by
// the user.
func WithGroups(groups []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.Groups = groups
		return nil
	}
}

// WithUserVolumes sets the user-added volumes of the container.
// These are not added to the container's spec, but will instead be used during
// commit to populate the volumes of the new image, and to trigger some OCI
// hooks that are only added if volume mounts are present.
// Furthermore, they are used in the output of inspect, to filter volumes -
// only volumes included in this list will be included in the output.
// Unless explicitly set, committed images will have no volumes.
// The given volumes slice must not be nil.
func WithUserVolumes(volumes []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		if volumes == nil {
			return define.ErrInvalidArg
		}

		ctr.config.UserVolumes = make([]string, 0, len(volumes))
		ctr.config.UserVolumes = append(ctr.config.UserVolumes, volumes...)
		return nil
	}
}

// WithEntrypoint sets the entrypoint of the container.
// This is not used to change the container's spec, but will instead be used
// during commit to populate the entrypoint of the new image.
// If not explicitly set it will default to the image's entrypoint.
// A nil entrypoint is allowed, and will clear entrypoint on the created image.
func WithEntrypoint(entrypoint []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.Entrypoint = make([]string, 0, len(entrypoint))
		ctr.config.Entrypoint = append(ctr.config.Entrypoint, entrypoint...)
		return nil
	}
}

// WithCommand sets the command of the container.
// This is not used to change the container's spec, but will instead be used
// during commit to populate the command of the new image.
// If not explicitly set it will default to the image's command.
// A nil command is allowed, and will clear command on the created image.
func WithCommand(command []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.Command = make([]string, 0, len(command))
		ctr.config.Command = append(ctr.config.Command, command...)
		return nil
	}
}

// WithRootFS sets the rootfs for the container.
// This creates a container from a directory on disk and not an image.
func WithRootFS(rootfs string, overlay bool, mapping *string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		if err := fileutils.Exists(rootfs); err != nil {
			return err
		}
		ctr.config.Rootfs = rootfs
		ctr.config.RootfsOverlay = overlay
		ctr.config.RootfsMapping = mapping
		return nil
	}
}

// WithUseImageResolvConf tells the container not to bind-mount resolv.conf in.
// This conflicts with other DNS-related options.
func WithUseImageResolvConf() CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.UseImageResolvConf = true

		return nil
	}
}

// WithUseImageHostname tells the container not to bind-mount /etc/hostname in.
func WithUseImageHostname() CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.UseImageHostname = true

		return nil
	}
}

// WithUseImageHosts tells the container not to bind-mount /etc/hosts in.
// This conflicts with WithHosts().
func WithUseImageHosts() CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.UseImageHosts = true

		return nil
	}
}

// WithRestartPolicy sets the container's restart policy. Valid values are
// "no", "on-failure", and "always". The empty string is allowed, and will be
// equivalent to "no".
func WithRestartPolicy(policy string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		if err := define.ValidateRestartPolicy(policy); err != nil {
			return err
		}

		ctr.config.RestartPolicy = policy

		return nil
	}
}

// WithRestartRetries sets the number of retries to use when restarting a
// container with the "on-failure" restart policy.
// 0 is an allowed value, and indicates infinite retries.
func WithRestartRetries(tries uint) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.RestartRetries = tries

		return nil
	}
}

// WithNamedVolumes adds the given named volumes to the container.
func WithNamedVolumes(volumes []*ContainerNamedVolume) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		for _, vol := range volumes {
			mountOpts, err := util.ProcessOptions(vol.Options, false, "")
			if err != nil {
				return fmt.Errorf("processing options for named volume %q mounted at %q: %w", vol.Name, vol.Dest, err)
			}

			ctr.config.NamedVolumes = append(ctr.config.NamedVolumes, &ContainerNamedVolume{
				Name:        vol.Name,
				Dest:        vol.Dest,
				Options:     mountOpts,
				IsAnonymous: vol.IsAnonymous,
				SubPath:     vol.SubPath,
			})
		}

		return nil
	}
}

// WithOverlayVolumes adds the given overlay volumes to the container.
func WithOverlayVolumes(volumes []*ContainerOverlayVolume) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		for _, vol := range volumes {
			ctr.config.OverlayVolumes = append(ctr.config.OverlayVolumes, &ContainerOverlayVolume{
				Dest:    vol.Dest,
				Source:  vol.Source,
				Options: vol.Options,
			})
		}

		return nil
	}
}

// WithImageVolumes adds the given image volumes to the container.
func WithImageVolumes(volumes []*ContainerImageVolume) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		for _, vol := range volumes {
			ctr.config.ImageVolumes = append(ctr.config.ImageVolumes, &ContainerImageVolume{
				Dest:      vol.Dest,
				Source:    vol.Source,
				ReadWrite: vol.ReadWrite,
				SubPath:   vol.SubPath,
			})
		}

		return nil
	}
}

// WithImageVolumes adds the given image volumes to the container.
func WithArtifactVolumes(volumes []*ContainerArtifactVolume) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.ArtifactVolumes = volumes

		return nil
	}
}

// WithHealthCheck adds the healthcheck to the container config
func WithHealthCheck(healthCheck *manifest.Schema2HealthConfig) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.HealthCheckConfig = healthCheck
		return nil
	}
}

// WithHealthCheckLogDestination adds the healthLogDestination to the container config
func WithHealthCheckLogDestination(destination string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		dest, err := define.GetValidHealthCheckDestination(destination)
		if err != nil {
			return err
		}
		ctr.config.HealthLogDestination = &dest
		return nil
	}
}

// WithHealthCheckMaxLogCount adds the healthMaxLogCount to the container config
func WithHealthCheckMaxLogCount(maxLogCount uint) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.HealthMaxLogCount = &maxLogCount
		return nil
	}
}

// WithHealthCheckMaxLogSize adds the healthMaxLogSize to the container config
func WithHealthCheckMaxLogSize(maxLogSize uint) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.HealthMaxLogSize = &maxLogSize
		return nil
	}
}

// WithHealthCheckOnFailureAction adds an on-failure action to health-check config
func WithHealthCheckOnFailureAction(action define.HealthCheckOnFailureAction) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.HealthCheckOnFailureAction = action
		return nil
	}
}

// WithPreserveFDs forwards from the process running Libpod into the container
// the given number of extra FDs (starting after the standard streams) to the created container
func WithPreserveFDs(fd uint) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.PreserveFDs = fd
		return nil
	}
}

// WithPreserveFD forwards from the process running Libpod into the container
// the given list of extra FDs to the created container
func WithPreserveFD(fds []uint) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.PreserveFD = fds
		return nil
	}
}

// WithCreateCommand adds the full command plus arguments of the current
// process to the container config.
func WithCreateCommand(cmd []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.CreateCommand = cmd
		return nil
	}
}

// withIsInfra allows us to differentiate between infra containers and other containers
// within the container config
func withIsInfra() CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.IsInfra = true

		return nil
	}
}

// withIsDefaultInfra allows us to differentiate between the default infra containers generated
// directly by podman and custom infra containers within the container config
func withIsDefaultInfra() CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.IsDefaultInfra = true

		return nil
	}
}

// WithIsService allows us to differentiate between service containers and other container
// within the container config.  It also sets the exit-code propagation of the
// service container.
func WithIsService(ecp define.KubeExitCodePropagation) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.IsService = true
		ctr.config.KubeExitCodePropagation = ecp
		return nil
	}
}

// WithCreateWorkingDir tells Podman to create the container's working directory
// if it does not exist.
func WithCreateWorkingDir() CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.CreateWorkingDir = true
		return nil
	}
}

// Volume Creation Options

func WithVolumeIgnoreIfExist() VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}
		volume.ignoreIfExists = true

		return nil
	}
}

// WithVolumeName sets the name of the volume.
func WithVolumeName(name string) VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		// Check the name against a regex
		if !define.NameRegex.MatchString(name) {
			return define.RegexError
		}
		volume.config.Name = name

		return nil
	}
}

// WithVolumeDriver sets the volume's driver.
// It is presently not implemented, but will be supported in a future Podman
// release.
func WithVolumeDriver(driver string) VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		volume.config.Driver = driver
		return nil
	}
}

// WithVolumeLabels sets the labels of the volume.
func WithVolumeLabels(labels map[string]string) VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		volume.config.Labels = make(map[string]string)
		maps.Copy(volume.config.Labels, labels)

		return nil
	}
}

// WithVolumeMountLabel sets the MountLabel of the volume.
func WithVolumeMountLabel(mountLabel string) VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		volume.config.MountLabel = mountLabel
		return nil
	}
}

// WithVolumeOptions sets the options of the volume.
func WithVolumeOptions(options map[string]string) VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		volume.config.Options = make(map[string]string)
		maps.Copy(volume.config.Options, options)

		return nil
	}
}

// WithVolumeUID sets the UID that the volume will be created as.
func WithVolumeUID(uid int) VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		volume.config.UID = uid

		return nil
	}
}

// WithVolumeSize sets the maximum size of the volume
func WithVolumeSize(size uint64) VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		volume.config.Size = size

		return nil
	}
}

// WithVolumeInodes sets the maximum inodes of the volume
func WithVolumeInodes(inodes uint64) VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		volume.config.Inodes = inodes

		return nil
	}
}

// WithVolumeGID sets the GID that the volume will be created as.
func WithVolumeGID(gid int) VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		volume.config.GID = gid

		return nil
	}
}

// WithVolumeNoChown prevents the volume from being chowned to the process uid at first use.
func WithVolumeNoChown() VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		volume.state.NeedsChown = false

		return nil
	}
}

// WithVolumeDisableQuota prevents the volume from being assigned a quota.
func WithVolumeDisableQuota() VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		volume.config.DisableQuota = true

		return nil
	}
}

// withSetAnon sets a bool notifying libpod that this volume is anonymous and
// should be removed when containers using it are removed and volumes are
// specified for removal.
func withSetAnon() VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		volume.config.IsAnon = true

		return nil
	}
}

// WithVolumeDriverTimeout sets the volume creation timeout period.
// Only usable if a non-local volume driver is in use.
func WithVolumeDriverTimeout(timeout uint) VolumeCreateOption {
	return func(volume *Volume) error {
		if volume.valid {
			return define.ErrVolumeFinalized
		}

		if volume.config.Driver == "" || volume.config.Driver == define.VolumeDriverLocal {
			return fmt.Errorf("Volume driver timeout can only be used with non-local volume drivers: %w", define.ErrInvalidArg)
		}

		tm := timeout

		volume.config.Timeout = &tm

		return nil
	}
}

// WithTimezone sets the timezone in the container
func WithTimezone(path string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		if path != "local" {
			// validate the format of the timezone specified if it's not "local"
			_, err := time.LoadLocation(path)
			if err != nil {
				return fmt.Errorf("finding timezone: %w", err)
			}
		}

		ctr.config.Timezone = path
		return nil
	}
}

// WithUmask sets the umask in the container
func WithUmask(umask string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		if !umaskRegex.MatchString(umask) {
			return fmt.Errorf("invalid umask string %s: %w", umask, define.ErrInvalidArg)
		}
		ctr.config.Umask = umask
		return nil
	}
}

// WithSecrets adds secrets to the container
func WithSecrets(containerSecrets []*ContainerSecret) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.Secrets = containerSecrets
		return nil
	}
}

// WithEnvSecrets adds environment variable secrets to the container
func WithEnvSecrets(envSecrets map[string]string) CtrCreateOption {
	return func(ctr *Container) error {
		ctr.config.EnvSecrets = make(map[string]*secrets.Secret)
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		manager, err := ctr.runtime.SecretsManager()
		if err != nil {
			return err
		}
		for target, src := range envSecrets {
			secr, err := manager.Lookup(src)
			if err != nil {
				return err
			}
			ctr.config.EnvSecrets[target] = secr
		}
		return nil
	}
}

// WithPidFile adds pidFile to the container
func WithPidFile(pidFile string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.PidFile = pidFile
		return nil
	}
}

// WithHostUsers indicates host users to add to /etc/passwd
func WithHostUsers(hostUsers []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.HostUsers = hostUsers
		return nil
	}
}

// WithInitCtrType indicates the container is an initcontainer
func WithInitCtrType(containerType string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		// Make sure the type is valid
		if containerType == define.OneShotInitContainer || containerType == define.AlwaysInitContainer {
			ctr.config.InitContainerType = containerType
			return nil
		}
		return fmt.Errorf("%s is invalid init container type", containerType)
	}
}

// WithHostDevice adds the original host src to the config
func WithHostDevice(dev []specs.LinuxDevice) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.DeviceHostSrc = dev
		return nil
	}
}

// WithSelectedPasswordManagement makes it so that the container either does or does not set up /etc/passwd or /etc/group
func WithSelectedPasswordManagement(passwd *bool) CtrCreateOption {
	return func(c *Container) error {
		if c.valid {
			return define.ErrCtrFinalized
		}
		c.config.Passwd = passwd
		return nil
	}
}

// WithStartupHealthcheck sets a startup healthcheck for the container.
// Requires that a healthcheck must be set.
func WithStartupHealthcheck(startupHC *define.StartupHealthCheck) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}
		ctr.config.StartupHealthCheckConfig = new(define.StartupHealthCheck)
		if err := JSONDeepCopy(startupHC, ctr.config.StartupHealthCheckConfig); err != nil {
			return fmt.Errorf("error copying startup healthcheck into container: %w", err)
		}
		return nil
	}
}

// Pod Creation Options

// WithPodCreateCommand adds the full command plus arguments of the current
// process to the pod config.
func WithPodCreateCommand(createCmd []string) PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}
		pod.config.CreateCommand = createCmd
		return nil
	}
}

// WithPodName sets the name of the pod.
func WithPodName(name string) PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		// Check the name against a regex
		if !define.NameRegex.MatchString(name) {
			return define.RegexError
		}

		pod.config.Name = name

		return nil
	}
}

// WithPodExitPolicy sets the exit policy of the pod.
func WithPodExitPolicy(policy string) PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		parsed, err := config.ParsePodExitPolicy(policy)
		if err != nil {
			return err
		}

		pod.config.ExitPolicy = parsed

		return nil
	}
}

// WithPodRestartPolicy sets the restart policy of the pod.
func WithPodRestartPolicy(policy string) PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		switch policy {
		//TODO: v5.0 if no restart policy is set, follow k8s convention and default to Always
		case define.RestartPolicyNone, define.RestartPolicyNo, define.RestartPolicyOnFailure, define.RestartPolicyAlways, define.RestartPolicyUnlessStopped:
			pod.config.RestartPolicy = policy
		default:
			return fmt.Errorf("%q is not a valid restart policy: %w", policy, define.ErrInvalidArg)
		}

		return nil
	}
}

// WithPodRestartRetries sets the number of retries to use when restarting a
// container with the "on-failure" restart policy.
// 0 is an allowed value, and indicates infinite retries.
func WithPodRestartRetries(tries uint) PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		pod.config.RestartRetries = &tries

		return nil
	}
}

// WithPodHostname sets the hostname of the pod.
func WithPodHostname(hostname string) PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		// Check the hostname against a regex
		if !define.NameRegex.MatchString(hostname) {
			return define.RegexError
		}

		pod.config.Hostname = hostname

		return nil
	}
}

// WithPodLabels sets the labels of a pod.
func WithPodLabels(labels map[string]string) PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		pod.config.Labels = make(map[string]string)
		maps.Copy(pod.config.Labels, labels)

		return nil
	}
}

// WithPodCgroupParent sets the Cgroup Parent of the pod.
func WithPodCgroupParent(path string) PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		pod.config.CgroupParent = path

		return nil
	}
}

// WithPodParent tells containers in this pod to use the cgroup created for
// this pod.
// This can still be overridden at the container level by explicitly specifying
// a Cgroup parent.
func WithPodParent() PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		pod.config.UsePodCgroup = true

		return nil
	}
}

// WithPodIPC tells containers in this pod to use the ipc namespace
// created for this pod.
// Containers in a pod will inherit the kernel namespaces from the
// first container added.
func WithPodIPC() PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		pod.config.UsePodIPC = true

		return nil
	}
}

// WithPodNet tells containers in this pod to use the network namespace
// created for this pod.
// Containers in a pod will inherit the kernel namespaces from the
// first container added.
func WithPodNet() PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		pod.config.UsePodNet = true

		return nil
	}
}

// WithPodUser tells containers in this pod to use the user namespace
// created for this pod.
// Containers in a pod will inherit the kernel namespaces from the
// first container added.
// TODO implement WithUserNSFrom, so WithUserNsFromPod functions properly
// Then this option can be added on the pod level
func WithPodUser() PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		pod.config.UsePodUser = true

		return nil
	}
}

// WithPodPID tells containers in this pod to use the pid namespace
// created for this pod.
// Containers in a pod will inherit the kernel namespaces from the
// first container added.
func WithPodPID() PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		pod.config.UsePodPID = true

		return nil
	}
}

// WithPodUTS tells containers in this pod to use the uts namespace
// created for this pod.
// Containers in a pod will inherit the kernel namespaces from the
// first container added.
func WithPodUTS() PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		pod.config.UsePodUTS = true

		return nil
	}
}

// WithPodCgroup tells containers in this pod to use the cgroup namespace
// created for this pod.
// Containers in a pod will inherit the kernel namespaces from the first
// container added.
func WithPodCgroup() PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		pod.config.UsePodCgroupNS = true

		return nil
	}
}

// WithInfraContainer tells the pod to create a pause container
func WithInfraContainer() PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}
		pod.config.HasInfra = true

		return nil
	}
}

// WithServiceContainer associates the specified service container ID with the pod.
func WithServiceContainer(id string) PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}

		ctr, err := pod.runtime.LookupContainer(id)
		if err != nil {
			return fmt.Errorf("looking up service container: %w", err)
		}

		if err := ctr.addServicePodLocked(pod.ID()); err != nil {
			return fmt.Errorf("associating service container %s with pod %s: %w", id, pod.ID(), err)
		}

		pod.config.ServiceContainerID = id
		return nil
	}
}

// WithPodResources sets resource limits to be applied to the pod's cgroup
// these will be inherited by all containers unless overridden.
func WithPodResources(resources specs.LinuxResources) PodCreateOption {
	return func(pod *Pod) error {
		if pod.valid {
			return define.ErrPodFinalized
		}
		pod.config.ResourceLimits = resources
		return nil
	}
}

// WithVolatile sets the volatile flag for the container storage.
// The option can potentially cause data loss when used on a container that must survive a machine reboot.
func WithVolatile() CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.Volatile = true

		return nil
	}
}

// WithChrootDirs is an additional set of directories that need to be
// treated as root directories. Standard bind mounts will be mounted
// into paths relative to these directories.
func WithChrootDirs(dirs []string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.ChrootDirs = dirs

		return nil
	}
}

// WithPasswdEntry sets the entry to write to the /etc/passwd file.
func WithPasswdEntry(passwdEntry string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.PasswdEntry = passwdEntry

		return nil
	}
}

// WithGroupEntry sets the entry to write to the /etc/group file.
func WithGroupEntry(groupEntry string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.GroupEntry = groupEntry

		return nil
	}
}

// WithBaseHostsFile sets the option to copy /etc/hosts file.
func WithBaseHostsFile(baseHostsFile string) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.BaseHostsFile = baseHostsFile

		return nil
	}
}

// WithMountAllDevices sets the option to mount all of a privileged container's
// host devices
func WithMountAllDevices() CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.MountAllDevices = true

		return nil
	}
}

// WithLabelNested sets the LabelNested flag allowing label separation within container
func WithLabelNested(nested bool) CtrCreateOption {
	return func(ctr *Container) error {
		if ctr.valid {
			return define.ErrCtrFinalized
		}

		ctr.config.LabelNested = nested

		return nil
	}
}
