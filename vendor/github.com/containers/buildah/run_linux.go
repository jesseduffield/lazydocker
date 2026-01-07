//go:build linux

package buildah

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"

	"github.com/containers/buildah/bind"
	"github.com/containers/buildah/chroot"
	"github.com/containers/buildah/copier"
	"github.com/containers/buildah/define"
	"github.com/containers/buildah/internal"
	"github.com/containers/buildah/internal/tmpdir"
	"github.com/containers/buildah/internal/volumes"
	"github.com/containers/buildah/pkg/binfmt"
	"github.com/containers/buildah/pkg/overlay"
	"github.com/containers/buildah/pkg/parse"
	butil "github.com/containers/buildah/pkg/util"
	"github.com/containers/buildah/util"
	"github.com/docker/go-units"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/etchosts"
	"go.podman.io/common/libnetwork/pasta"
	"go.podman.io/common/libnetwork/resolvconf"
	"go.podman.io/common/libnetwork/slirp4netns"
	nettypes "go.podman.io/common/libnetwork/types"
	netUtil "go.podman.io/common/libnetwork/util"
	"go.podman.io/common/pkg/capabilities"
	"go.podman.io/common/pkg/chown"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/hooks"
	hooksExec "go.podman.io/common/pkg/hooks/exec"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/pkg/lockfile"
	"go.podman.io/storage/pkg/mount"
	"go.podman.io/storage/pkg/stringid"
	"go.podman.io/storage/pkg/unshare"
	"golang.org/x/sys/unix"
	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/pkg/parser"
)

// binfmtRegistered makes sure we only try to register binfmt_misc
// interpreters once, the first time we handle a RUN instruction.
var binfmtRegistered sync.Once

func setChildProcess() error {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, uintptr(1), 0, 0, 0); err != nil {
		fmt.Fprintf(os.Stderr, "prctl(PR_SET_CHILD_SUBREAPER, 1): %v\n", err)
		return err
	}
	return nil
}

func (b *Builder) cdiSetupDevicesInSpec(deviceSpecs []string, configDir string, spec *specs.Spec) ([]string, error) {
	var configDirs []string
	defConfig, err := config.Default()
	if err != nil {
		return nil, fmt.Errorf("failed to get container config: %w", err)
	}
	// The CDI cache prioritizes entries from directories that are later in
	// the list of ones it scans, so start with our general config, then
	// append values passed to us through API layers.
	configDirs = slices.Clone(defConfig.Engine.CdiSpecDirs.Get())
	if b.CDIConfigDir != "" {
		configDirs = append(configDirs, b.CDIConfigDir)
	}
	if configDir != "" {
		configDirs = append(configDirs, configDir)
	}
	if len(configDirs) == 0 {
		// No directories to scan for CDI configuration means that CDI
		// won't have any details for setting up any devices, so we
		// don't need to be doing anything here.
		return deviceSpecs, nil
	}
	var qualifiedDeviceSpecs, unqualifiedDeviceSpecs []string
	for _, deviceSpec := range deviceSpecs {
		if parser.IsQualifiedName(deviceSpec) {
			qualifiedDeviceSpecs = append(qualifiedDeviceSpecs, deviceSpec)
		} else {
			unqualifiedDeviceSpecs = append(unqualifiedDeviceSpecs, deviceSpec)
		}
	}
	if len(qualifiedDeviceSpecs) == 0 {
		// None of the specified devices were in the form that would be
		// handled by CDI, so we don't need to do anything here.
		return deviceSpecs, nil
	}
	if err := cdi.Configure(cdi.WithSpecDirs(configDirs...)); err != nil {
		return nil, fmt.Errorf("CDI default registry ignored configured directories %v: %w", configDirs, err)
	}
	leftoverDevices := slices.Clone(deviceSpecs)
	if err := cdi.Refresh(); err != nil {
		logrus.Warnf("CDI default registry refresh: %v", err)
	} else {
		leftoverDevices, err = cdi.InjectDevices(spec, qualifiedDeviceSpecs...)
		if err != nil {
			return nil, fmt.Errorf("CDI device injection (leftover devices: %v): %w", leftoverDevices, err)
		}
	}
	removed := slices.DeleteFunc(slices.Clone(deviceSpecs), func(t string) bool { return slices.Contains(leftoverDevices, t) })
	logrus.Debugf("CDI taking care of devices %v, leaving devices %v, skipped %v", removed, leftoverDevices, unqualifiedDeviceSpecs)
	return append(leftoverDevices, unqualifiedDeviceSpecs...), nil
}

// Extract the device list so that we can still try to make it work if
// we're running rootless and can't just mknod() the device nodes.
func separateDevicesFromRuntimeSpec(g *generate.Generator) define.ContainerDevices {
	var result define.ContainerDevices
	if g.Config != nil && g.Config.Linux != nil {
		for _, device := range g.Config.Linux.Devices {
			var bDevice define.BuildahDevice
			bDevice.Path = device.Path
			switch device.Type {
			case "b":
				bDevice.Type = 'b'
			case "c":
				bDevice.Type = 'c'
			case "u":
				bDevice.Type = 'u'
			case "p":
				bDevice.Type = 'p'
			}
			bDevice.Major = device.Major
			bDevice.Minor = device.Minor
			if device.FileMode != nil {
				bDevice.FileMode = *device.FileMode
			}
			if device.UID != nil {
				bDevice.Uid = *device.UID
			}
			if device.GID != nil {
				bDevice.Gid = *device.GID
			}
			bDevice.Source = device.Path
			bDevice.Destination = device.Path
			result = append(result, bDevice)
		}
	}
	g.ClearLinuxDevices()
	return result
}

// Run runs the specified command in the container's root filesystem.
func (b *Builder) Run(command []string, options RunOptions) error {
	var runArtifacts *runMountArtifacts
	if len(options.ExternalImageMounts) > 0 {
		defer func() {
			if runArtifacts == nil {
				// we didn't add ExternalImageMounts to the
				// list of images that we're going to unmount
				// yet and make a deferred call that cleans
				// them up, but the caller is expecting us to
				// unmount these for them because we offered to
				for _, image := range options.ExternalImageMounts {
					if _, err := b.store.UnmountImage(image, false); err != nil {
						logrus.Debugf("umounting image %q: %v", image, err)
					}
				}
			}
		}()
	}

	if os.Getenv("container") != "" {
		os, arch, variant, err := parse.Platform("")
		if err != nil {
			return fmt.Errorf("reading the current default platform")
		}
		platform := b.OCIv1.Platform
		if os != platform.OS || arch != platform.Architecture || variant != platform.Variant {
			binfmtRegistered.Do(func() {
				if err := binfmt.Register(nil); err != nil {
					logrus.Warnf("registering binfmt_misc interpreters: %v", err)
				}
			})
		}
	}

	p, err := os.MkdirTemp(tmpdir.GetTempDir(), define.Package)
	if err != nil {
		return err
	}
	// On some hosts like AH, /tmp is a symlink and we need an
	// absolute path.
	path, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	logrus.Debugf("using %q to hold bundle data", path)
	defer func() {
		if err2 := os.RemoveAll(path); err2 != nil {
			options.Logger.Error(err2)
		}
	}()

	gp, err := generate.New("linux")
	if err != nil {
		return fmt.Errorf("generating new 'linux' runtime spec: %w", err)
	}
	g := &gp

	isolation := options.Isolation
	if isolation == define.IsolationDefault {
		isolation = b.Isolation
		if isolation == define.IsolationDefault {
			isolation, err = parse.IsolationOption("")
			if err != nil {
				logrus.Debugf("got %v while trying to determine default isolation, guessing OCI", err)
				isolation = IsolationOCI
			} else if isolation == IsolationDefault {
				isolation = IsolationOCI
			}
		}
	}
	if err := checkAndOverrideIsolationOptions(isolation, &options); err != nil {
		return err
	}

	// hardwire the environment to match docker build to avoid subtle and hard-to-debug differences due to containers.conf
	b.configureEnvironment(g, options, []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"})

	if b.CommonBuildOpts == nil {
		return fmt.Errorf("invalid format on container you must recreate the container")
	}

	if err := addCommonOptsToSpec(b.CommonBuildOpts, g); err != nil {
		return err
	}

	workDir := b.WorkDir()
	if options.WorkingDir != "" {
		g.SetProcessCwd(options.WorkingDir)
		workDir = options.WorkingDir
	} else if b.WorkDir() != "" {
		g.SetProcessCwd(b.WorkDir())
		workDir = b.WorkDir()
	}
	if workDir == "" {
		workDir = string(os.PathSeparator)
	}
	setupSelinux(g, b.ProcessLabel, b.MountLabel)
	mountPoint, err := b.Mount(b.MountLabel)
	if err != nil {
		return fmt.Errorf("mounting container %q: %w", b.ContainerID, err)
	}
	defer func() {
		if err := b.Unmount(); err != nil {
			options.Logger.Errorf("error unmounting container: %v", err)
		}
	}()
	g.SetRootPath(mountPoint)
	if len(command) > 0 {
		command = runLookupPath(g, command)
		g.SetProcessArgs(command)
	} else {
		g.SetProcessArgs(nil)
	}

	// Combine the working container's set of devices with the ones for just this run.
	deviceSpecs := slices.Concat(options.DeviceSpecs, b.DeviceSpecs)
	deviceSpecs, err = b.cdiSetupDevicesInSpec(deviceSpecs, options.CDIConfigDir, g.Config) // makes changes to more than just the device list
	if err != nil {
		return err
	}
	devices := separateDevicesFromRuntimeSpec(g)
	for _, deviceSpec := range deviceSpecs {
		device, err := parse.DeviceFromPath(deviceSpec)
		if err != nil {
			return fmt.Errorf("setting up device %q: %w", deviceSpec, err)
		}
		devices = append(devices, device...)
	}
	devices = append(append(devices, options.Devices...), b.Devices...)

	// Mount devices, if any, and if we're rootless attempt to work around not
	// being able to create device nodes by bind-mounting them from the host, like podman does.
	if unshare.IsRootless() {
		// We are going to create bind mounts for devices
		// but we need to make sure that we don't override
		// anything which is already in OCI spec.
		mounts := make(map[string]any)
		for _, m := range g.Mounts() {
			mounts[m.Destination] = true
		}
		newMounts := []specs.Mount{}
		for _, d := range devices {
			// Default permission is read-only.
			perm := "ro"
			// Get permission configured for this device but only process `write`
			// permission in rootless since `mknod` is not supported anyways.
			if strings.Contains(string(d.Rule.Permissions), "w") {
				perm = "rw"
			}
			devMnt := specs.Mount{
				Destination: d.Destination,
				Type:        parse.TypeBind,
				Source:      d.Source,
				Options:     []string{"slave", "nosuid", "noexec", perm, "rbind"},
			}
			// Podman parity: podman skips these two devices hence we do the same.
			if d.Path == "/dev/ptmx" || strings.HasPrefix(d.Path, "/dev/tty") {
				continue
			}
			// Device is already in OCI spec do not re-mount.
			if _, found := mounts[d.Path]; found {
				continue
			}
			newMounts = append(newMounts, devMnt)
		}
		g.Config.Mounts = append(newMounts, g.Config.Mounts...)
	} else {
		for _, d := range devices {
			sDev := specs.LinuxDevice{
				Type:     string(d.Type),
				Path:     d.Path,
				Major:    d.Major,
				Minor:    d.Minor,
				FileMode: &d.FileMode,
				UID:      &d.Uid,
				GID:      &d.Gid,
			}
			g.AddDevice(sDev)
			g.AddLinuxResourcesDevice(true, string(d.Type), &d.Major, &d.Minor, string(d.Permissions))
		}
	}

	setupMaskedPaths(g, b.CommonBuildOpts)
	setupReadOnlyPaths(g)

	setupTerminal(g, options.Terminal, options.TerminalSize)

	configureNetwork, networkString, err := b.configureNamespaces(g, &options)
	if err != nil {
		return err
	}

	homeDir, err := b.configureUIDGID(g, mountPoint, options)
	if err != nil {
		return err
	}

	g.SetProcessNoNewPrivileges(b.CommonBuildOpts.NoNewPrivileges)

	g.SetProcessApparmorProfile(b.CommonBuildOpts.ApparmorProfile)

	// Now grab the spec from the generator.  Set the generator to nil so that future contributors
	// will quickly be able to tell that they're supposed to be modifying the spec directly from here.
	spec := g.Config
	g = nil

	// Set the seccomp configuration using the specified profile name.  Some syscalls are
	// allowed if certain capabilities are to be granted (example: CAP_SYS_CHROOT and chroot),
	// so we sorted out the capabilities lists first.
	if err = setupSeccomp(spec, b.CommonBuildOpts.SeccompProfilePath); err != nil {
		return err
	}

	uid, gid := spec.Process.User.UID, spec.Process.User.GID
	if spec.Linux != nil {
		uid, gid, err = util.GetHostIDs(spec.Linux.UIDMappings, spec.Linux.GIDMappings, uid, gid)
		if err != nil {
			return err
		}
	}

	idPair := &idtools.IDPair{UID: int(uid), GID: int(gid)}

	mode := os.FileMode(0o755)
	coptions := copier.MkdirOptions{
		ChownNew: idPair,
		ChmodNew: &mode,
	}
	if err := copier.Mkdir(mountPoint, filepath.Join(mountPoint, spec.Process.Cwd), coptions); err != nil {
		return err
	}

	bindFiles := make(map[string]string)
	volumes := b.Volumes()

	// Figure out who owns files that will appear to be owned by UID/GID 0 in the container.
	rootUID, rootGID, err := util.GetHostRootIDs(spec)
	if err != nil {
		return err
	}
	rootIDPair := &idtools.IDPair{UID: int(rootUID), GID: int(rootGID)}

	hostsFile := ""
	if !options.NoHosts && !slices.Contains(volumes, config.DefaultHostsFile) && options.ConfigureNetwork != define.NetworkDisabled {
		hostsFile, err = b.createHostsFile(path, rootIDPair)
		if err != nil {
			return err
		}
		bindFiles[config.DefaultHostsFile] = hostsFile

		// Only add entries here if we do not have to do setup network,
		// if we do we have to do it much later after the network setup.
		if !configureNetwork {
			var entries etchosts.HostEntries
			isHost := true
			if spec.Linux != nil {
				for _, ns := range spec.Linux.Namespaces {
					if ns.Type == specs.NetworkNamespace {
						isHost = false
						break
					}
				}
			}
			// add host entry for local ip when running in host network
			if spec.Hostname != "" && isHost {
				ip := netUtil.GetLocalIP()
				if ip != "" {
					entries = append(entries, etchosts.HostEntry{
						Names: []string{spec.Hostname},
						IP:    ip,
					})
				}
			}
			err = b.addHostsEntries(hostsFile, mountPoint, entries, nil, "")
			if err != nil {
				return err
			}
		}
	}

	if !options.NoHostname && !(slices.Contains(volumes, "/etc/hostname")) {
		hostnameFile, err := b.generateHostname(path, spec.Hostname, rootIDPair)
		if err != nil {
			return err
		}
		// Bind /etc/hostname
		bindFiles["/etc/hostname"] = hostnameFile
	}

	resolvFile := ""
	if !slices.Contains(volumes, resolvconf.DefaultResolvConf) && options.ConfigureNetwork != define.NetworkDisabled && (len(b.CommonBuildOpts.DNSServers) != 1 || strings.ToLower(b.CommonBuildOpts.DNSServers[0]) != "none") {
		resolvFile, err = b.createResolvConf(path, rootIDPair)
		if err != nil {
			return err
		}
		bindFiles[resolvconf.DefaultResolvConf] = resolvFile

		// Only add entries here if we do not have to do setup network,
		// if we do we have to do it much later after the network setup.
		if !configureNetwork {
			err = b.addResolvConfEntries(resolvFile, nil, spec, false, true)
			if err != nil {
				return err
			}
		}
	}
	// Empty file, so no need to recreate if it exists
	if _, ok := bindFiles["/run/.containerenv"]; !ok {
		containerenvPath := filepath.Join(path, "/run/.containerenv")
		if err = os.MkdirAll(filepath.Dir(containerenvPath), 0o755); err != nil {
			return err
		}

		rootless := 0
		if unshare.IsRootless() {
			rootless = 1
		}
		// Populate the .containerenv with container information
		containerenv := fmt.Sprintf(`
engine="buildah-%s"
name=%q
id=%q
image=%q
imageid=%q
rootless=%d
`, define.Version, b.Container, b.ContainerID, b.FromImage, b.FromImageID, rootless)

		if err = ioutils.AtomicWriteFile(containerenvPath, []byte(containerenv), 0o755); err != nil {
			return err
		}
		if err := relabel(containerenvPath, b.MountLabel, false); err != nil {
			return err
		}

		bindFiles["/run/.containerenv"] = containerenvPath
	}

	// Setup OCI hooks
	_, err = b.setupOCIHooks(spec, (len(options.Mounts) > 0 || len(volumes) > 0))
	if err != nil {
		return fmt.Errorf("unable to setup OCI hooks: %w", err)
	}

	runMountInfo := runMountInfo{
		WorkDir:          workDir,
		ContextDir:       options.ContextDir,
		Secrets:          options.Secrets,
		SSHSources:       options.SSHSources,
		StageMountPoints: options.StageMountPoints,
		SystemContext:    options.SystemContext,
	}

	runArtifacts, err = b.setupMounts(mountPoint, spec, path, options.Mounts, bindFiles, volumes, options.CompatBuiltinVolumes, b.CommonBuildOpts.Volumes, options.RunMounts, runMountInfo)
	if err != nil {
		return fmt.Errorf("resolving mountpoints for container %q: %w", b.ContainerID, err)
	}
	if runArtifacts.SSHAuthSock != "" {
		sshenv := "SSH_AUTH_SOCK=" + runArtifacts.SSHAuthSock
		spec.Process.Env = append(spec.Process.Env, sshenv)
	}

	// Create any mount points that we need that aren't already present in
	// the rootfs.
	createdMountTargets, err := b.createMountTargets(spec)
	if err != nil {
		return fmt.Errorf("ensuring mount targets for container %q: %w", b.ContainerID, err)
	}
	defer func() {
		// Attempt to clean up mount targets for the sake of builds
		// that don't commit and rebase at each step, and people using
		// `buildah run` more than once, who don't expect empty mount
		// points to stick around.  They'll still get filtered out at
		// commit-time if another concurrent Run() is keeping something
		// busy.
		if _, err := copier.ConditionalRemove(mountPoint, mountPoint, copier.ConditionalRemoveOptions{
			UIDMap: b.store.UIDMap(),
			GIDMap: b.store.GIDMap(),
			Paths:  createdMountTargets,
		}); err != nil {
			options.Logger.Errorf("unable to cleanup run mount targets %v", err)
		}
	}()

	// following run was called from `buildah run`
	// and some images were mounted for this run
	// add them to cleanup artifacts
	if len(options.ExternalImageMounts) > 0 {
		runArtifacts.MountedImages = append(runArtifacts.MountedImages, options.ExternalImageMounts...)
	}

	defer func() {
		if err := b.cleanupRunMounts(runArtifacts); err != nil {
			options.Logger.Errorf("unable to cleanup run mounts %v", err)
		}
	}()

	// Handle mount flags that request that the source locations for "bind" mountpoints be
	// relabeled, and filter those flags out of the list of mount options we pass to the
	// runtime.
	for i := range spec.Mounts {
		switch spec.Mounts[i].Type {
		default:
			continue
		case "bind", "rbind":
			// all good, keep going
		}
		zflag := ""
		for _, opt := range spec.Mounts[i].Options {
			if opt == "z" || opt == "Z" {
				zflag = opt
			}
		}
		if zflag == "" {
			continue
		}
		spec.Mounts[i].Options = slices.DeleteFunc(spec.Mounts[i].Options, func(opt string) bool {
			return opt == "z" || opt == "Z"
		})
		if err := relabel(spec.Mounts[i].Source, b.MountLabel, zflag == "z"); err != nil {
			return fmt.Errorf("setting file label %q on %q: %w", b.MountLabel, spec.Mounts[i].Source, err)
		}
	}

	switch isolation {
	case define.IsolationOCI:
		var moreCreateArgs []string
		if options.NoPivot {
			moreCreateArgs = append(moreCreateArgs, "--no-pivot")
		}
		err = b.runUsingRuntimeSubproc(isolation, options, configureNetwork, networkString, moreCreateArgs, spec,
			mountPoint, path, define.Package+"-"+filepath.Base(path), b.Container, hostsFile, resolvFile)
	case IsolationChroot:
		err = chroot.RunUsingChroot(spec, path, homeDir, options.Stdin, options.Stdout, options.Stderr, options.NoPivot)
	case IsolationOCIRootless:
		moreCreateArgs := []string{"--no-new-keyring"}
		if options.NoPivot {
			moreCreateArgs = append(moreCreateArgs, "--no-pivot")
		}
		err = b.runUsingRuntimeSubproc(isolation, options, configureNetwork, networkString, moreCreateArgs, spec,
			mountPoint, path, define.Package+"-"+filepath.Base(path), b.Container, hostsFile, resolvFile)
	default:
		err = errors.New("don't know how to run this command")
	}
	return err
}

func (b *Builder) setupOCIHooks(config *specs.Spec, hasVolumes bool) (map[string][]specs.Hook, error) {
	allHooks := make(map[string][]specs.Hook)
	if len(b.CommonBuildOpts.OCIHooksDir) == 0 {
		if unshare.IsRootless() {
			return nil, nil
		}
		for _, hDir := range []string{hooks.DefaultDir, hooks.OverrideDir} {
			manager, err := hooks.New(context.Background(), []string{hDir}, []string{})
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, err
			}
			ociHooks, err := manager.Hooks(config, b.ImageAnnotations, hasVolumes)
			if err != nil {
				return nil, err
			}
			if len(ociHooks) > 0 || config.Hooks != nil {
				logrus.Warnf("Implicit hook directories are deprecated; set --hooks-dir=%q explicitly to continue to load ociHooks from this directory", hDir)
			}
			maps.Copy(allHooks, ociHooks)
		}
	} else {
		manager, err := hooks.New(context.Background(), b.CommonBuildOpts.OCIHooksDir, []string{})
		if err != nil {
			return nil, err
		}

		allHooks, err = manager.Hooks(config, b.ImageAnnotations, hasVolumes)
		if err != nil {
			return nil, err
		}
	}

	hookErr, err := hooksExec.RuntimeConfigFilter(context.Background(), allHooks["precreate"], config, hooksExec.DefaultPostKillTimeout) //nolint:staticcheck
	if err != nil {
		logrus.Warnf("Container: precreate hook: %v", err)
		if hookErr != nil && hookErr != err {
			logrus.Debugf("container: precreate hook (hook error): %v", hookErr)
		}
		return nil, err
	}
	return allHooks, nil
}

func addCommonOptsToSpec(commonOpts *define.CommonBuildOptions, g *generate.Generator) error {
	// Resources - CPU
	if commonOpts.CPUPeriod != 0 {
		g.SetLinuxResourcesCPUPeriod(commonOpts.CPUPeriod)
	}
	if commonOpts.CPUQuota != 0 {
		g.SetLinuxResourcesCPUQuota(commonOpts.CPUQuota)
	}
	if commonOpts.CPUShares != 0 {
		g.SetLinuxResourcesCPUShares(commonOpts.CPUShares)
	}
	if commonOpts.CPUSetCPUs != "" {
		g.SetLinuxResourcesCPUCpus(commonOpts.CPUSetCPUs)
	}
	if commonOpts.CPUSetMems != "" {
		g.SetLinuxResourcesCPUMems(commonOpts.CPUSetMems)
	}

	// Resources - Memory
	if commonOpts.Memory != 0 {
		g.SetLinuxResourcesMemoryLimit(commonOpts.Memory)
	}
	if commonOpts.MemorySwap != 0 {
		g.SetLinuxResourcesMemorySwap(commonOpts.MemorySwap)
	}

	// cgroup membership
	if commonOpts.CgroupParent != "" {
		g.SetLinuxCgroupsPath(commonOpts.CgroupParent)
	}

	defaultContainerConfig, err := config.Default()
	if err != nil {
		return fmt.Errorf("failed to get container config: %w", err)
	}
	// Other process resource limits
	if err := addRlimits(commonOpts.Ulimit, g, defaultContainerConfig.Containers.DefaultUlimits.Get()); err != nil {
		return err
	}

	logrus.Debugf("Resources: %#v", commonOpts)
	return nil
}

func setupSlirp4netnsNetwork(config *config.Config, netns, cid string, options, hostnames []string) (func(), *netResult, error) {
	// we need the TmpDir for the slirp4netns code
	if err := os.MkdirAll(config.Engine.TmpDir, 0o751); err != nil {
		return nil, nil, fmt.Errorf("failed to create tempdir: %w", err)
	}
	res, err := slirp4netns.Setup(&slirp4netns.SetupOptions{
		Config:       config,
		ContainerID:  cid,
		Netns:        netns,
		ExtraOptions: options,
		Pdeathsig:    syscall.SIGKILL,
	})
	if err != nil {
		return nil, nil, err
	}

	ip, err := slirp4netns.GetIP(res.Subnet)
	if err != nil {
		return nil, nil, fmt.Errorf("get slirp4netns ip: %w", err)
	}

	dns, err := slirp4netns.GetDNS(res.Subnet)
	if err != nil {
		return nil, nil, fmt.Errorf("get slirp4netns dns ip: %w", err)
	}

	result := &netResult{
		entries:           etchosts.HostEntries{{IP: ip.String(), Names: hostnames}},
		dnsServers:        []string{dns.String()},
		ipv6:              res.IPv6,
		keepHostResolvers: true,
	}

	return func() {
		syscall.Kill(res.Pid, syscall.SIGKILL) //nolint:errcheck
		var status syscall.WaitStatus
		syscall.Wait4(res.Pid, &status, 0, nil) //nolint:errcheck
	}, result, nil
}

func setupPasta(config *config.Config, netns string, options, hostnames []string) (func(), *netResult, error) {
	res, err := pasta.Setup(&pasta.SetupOptions{
		Config:       config,
		Netns:        netns,
		ExtraOptions: options,
	})
	if err != nil {
		return nil, nil, err
	}

	var entries etchosts.HostEntries
	if len(res.IPAddresses) > 0 {
		entries = etchosts.HostEntries{{IP: res.IPAddresses[0].String(), Names: hostnames}}
	}

	mappedIP := ""
	if len(res.MapGuestAddrIPs) > 0 {
		mappedIP = res.MapGuestAddrIPs[0]
	}

	result := &netResult{
		entries:                           entries,
		dnsServers:                        res.DNSForwardIPs,
		excludeIPs:                        res.IPAddresses,
		ipv6:                              res.IPv6,
		keepHostResolvers:                 true,
		preferredHostContainersInternalIP: mappedIP,
	}

	return nil, result, nil
}

func (b *Builder) runConfigureNetwork(pid int, isolation define.Isolation, options RunOptions, network, containerName string, hostnames []string) (func(), *netResult, error) {
	netns := fmt.Sprintf("/proc/%d/ns/net", pid)
	var configureNetworks []string
	defConfig, err := config.Default()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get container config: %w", err)
	}

	name, networkOpts, hasOpts := strings.Cut(network, ":")
	var netOpts []string
	if hasOpts {
		netOpts = strings.Split(networkOpts, ",")
	}
	if isolation == IsolationOCIRootless && name == "" {
		switch defConfig.Network.DefaultRootlessNetworkCmd {
		case slirp4netns.BinaryName, "":
			name = slirp4netns.BinaryName
		case pasta.BinaryName:
			name = pasta.BinaryName
		default:
			return nil, nil, fmt.Errorf("invalid default_rootless_network_cmd option %q",
				defConfig.Network.DefaultRootlessNetworkCmd)
		}
	}

	switch {
	case name == slirp4netns.BinaryName:
		return setupSlirp4netnsNetwork(defConfig, netns, containerName, netOpts, hostnames)
	case name == pasta.BinaryName:
		return setupPasta(defConfig, netns, netOpts, hostnames)

	// Basically default case except we make sure to not split an empty
	// name as this would return a slice with one empty string which is
	// not a valid network name.
	case len(network) > 0:
		// old syntax allow comma separated network names
		configureNetworks = strings.Split(network, ",")
	}

	if isolation == IsolationOCIRootless {
		return nil, nil, errors.New("cannot use networks as rootless")
	}

	if len(configureNetworks) == 0 {
		configureNetworks = []string{b.NetworkInterface.DefaultNetworkName()}
	}

	// Make sure we can access the container's network namespace,
	// even after it exits, to successfully tear down the
	// interfaces.  Ensure this by opening a handle to the network
	// namespace, and using our copy to both configure and
	// deconfigure it.
	netFD, err := unix.Open(netns, unix.O_RDONLY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("opening network namespace: %w", err)
	}
	mynetns := fmt.Sprintf("/proc/%d/fd/%d", unix.Getpid(), netFD)

	networks := make(map[string]nettypes.PerNetworkOptions, len(configureNetworks))
	for i, network := range configureNetworks {
		networks[network] = nettypes.PerNetworkOptions{
			InterfaceName: fmt.Sprintf("eth%d", i),
		}
	}

	opts := nettypes.NetworkOptions{
		ContainerID:   containerName,
		ContainerName: containerName,
		Networks:      networks,
	}
	netStatus, err := b.NetworkInterface.Setup(mynetns, nettypes.SetupOptions{NetworkOptions: opts})
	if err != nil {
		return nil, nil, err
	}

	teardown := func() {
		err := b.NetworkInterface.Teardown(mynetns, nettypes.TeardownOptions{NetworkOptions: opts})
		if err != nil {
			options.Logger.Errorf("failed to cleanup network: %v", err)
		}
	}

	return teardown, netStatusToNetResult(netStatus, hostnames), nil
}

// Create pipes to use for relaying stdio.
func runMakeStdioPipe(uid, gid int) ([][]int, error) {
	stdioPipe := make([][]int, 3)
	for i := range stdioPipe {
		stdioPipe[i] = make([]int, 2)
		if err := unix.Pipe(stdioPipe[i]); err != nil {
			return nil, fmt.Errorf("creating pipe for container FD %d: %w", i, err)
		}
	}
	if err := unix.Fchown(stdioPipe[unix.Stdin][0], uid, gid); err != nil {
		return nil, fmt.Errorf("setting owner of stdin pipe descriptor: %w", err)
	}
	if err := unix.Fchown(stdioPipe[unix.Stdout][1], uid, gid); err != nil {
		return nil, fmt.Errorf("setting owner of stdout pipe descriptor: %w", err)
	}
	if err := unix.Fchown(stdioPipe[unix.Stderr][1], uid, gid); err != nil {
		return nil, fmt.Errorf("setting owner of stderr pipe descriptor: %w", err)
	}
	return stdioPipe, nil
}

func setupNamespaces(_ *logrus.Logger, g *generate.Generator, namespaceOptions define.NamespaceOptions, idmapOptions define.IDMappingOptions, policy define.NetworkConfigurationPolicy) (configureNetwork bool, networkString string, configureUTS bool, err error) {
	defaultContainerConfig, err := config.Default()
	if err != nil {
		return false, "", false, fmt.Errorf("failed to get container config: %w", err)
	}

	addSysctl := func(prefixes []string) error {
		for _, sysctl := range defaultContainerConfig.Sysctls() {
			splitn := strings.SplitN(sysctl, "=", 2)
			if len(splitn) > 2 {
				return fmt.Errorf("sysctl %q defined in containers.conf must be formatted name=value", sysctl)
			}
			for _, prefix := range prefixes {
				if strings.HasPrefix(splitn[0], prefix) {
					g.AddLinuxSysctl(splitn[0], splitn[1])
				}
			}
		}
		return nil
	}

	// Set namespace options in the container configuration.
	configureUserns := false
	specifiedNetwork := false
	for _, namespaceOption := range namespaceOptions {
		switch namespaceOption.Name {
		case string(specs.IPCNamespace):
			if !namespaceOption.Host {
				if err := addSysctl([]string{"fs.mqueue"}); err != nil {
					return false, "", false, err
				}
			}
		case string(specs.UserNamespace):
			configureUserns = false
			if !namespaceOption.Host && namespaceOption.Path == "" {
				configureUserns = true
			}
		case string(specs.NetworkNamespace):
			specifiedNetwork = true
			configureNetwork = false
			if !namespaceOption.Host && (namespaceOption.Path == "" || !filepath.IsAbs(namespaceOption.Path)) {
				if namespaceOption.Path != "" && !filepath.IsAbs(namespaceOption.Path) {
					networkString = namespaceOption.Path
					namespaceOption.Path = ""
				}
				configureNetwork = (policy != define.NetworkDisabled)
			}
		case string(specs.UTSNamespace):
			configureUTS = false
			if !namespaceOption.Host {
				if namespaceOption.Path == "" {
					configureUTS = true
				}
				if err := addSysctl([]string{"kernel.hostname", "kernel.domainame"}); err != nil {
					return false, "", false, err
				}
			}
		}
		if namespaceOption.Host {
			if err := g.RemoveLinuxNamespace(namespaceOption.Name); err != nil {
				return false, "", false, fmt.Errorf("removing %q namespace for run: %w", namespaceOption.Name, err)
			}
		} else if err := g.AddOrReplaceLinuxNamespace(namespaceOption.Name, namespaceOption.Path); err != nil {
			if namespaceOption.Path == "" {
				return false, "", false, fmt.Errorf("adding new %q namespace for run: %w", namespaceOption.Name, err)
			}
			return false, "", false, fmt.Errorf("adding %q namespace %q for run: %w", namespaceOption.Name, namespaceOption.Path, err)
		}
	}

	// If we've got mappings, we're going to have to create a user namespace.
	if len(idmapOptions.UIDMap) > 0 || len(idmapOptions.GIDMap) > 0 || configureUserns {
		if err := g.AddOrReplaceLinuxNamespace(string(specs.UserNamespace), ""); err != nil {
			return false, "", false, fmt.Errorf("adding new %q namespace for run: %w", string(specs.UserNamespace), err)
		}
		hostUidmap, hostGidmap, err := unshare.GetHostIDMappings("")
		if err != nil {
			return false, "", false, err
		}
		for _, m := range idmapOptions.UIDMap {
			g.AddLinuxUIDMapping(m.HostID, m.ContainerID, m.Size)
		}
		if len(idmapOptions.UIDMap) == 0 {
			for _, m := range hostUidmap {
				g.AddLinuxUIDMapping(m.ContainerID, m.ContainerID, m.Size)
			}
		}
		for _, m := range idmapOptions.GIDMap {
			g.AddLinuxGIDMapping(m.HostID, m.ContainerID, m.Size)
		}
		if len(idmapOptions.GIDMap) == 0 {
			for _, m := range hostGidmap {
				g.AddLinuxGIDMapping(m.ContainerID, m.ContainerID, m.Size)
			}
		}
		if !specifiedNetwork {
			if err := g.AddOrReplaceLinuxNamespace(string(specs.NetworkNamespace), ""); err != nil {
				return false, "", false, fmt.Errorf("adding new %q namespace for run: %w", string(specs.NetworkNamespace), err)
			}
			configureNetwork = (policy != define.NetworkDisabled)
		}
	} else {
		if err := g.RemoveLinuxNamespace(string(specs.UserNamespace)); err != nil {
			return false, "", false, fmt.Errorf("removing %q namespace for run: %w", string(specs.UserNamespace), err)
		}
		if !specifiedNetwork {
			if err := g.RemoveLinuxNamespace(string(specs.NetworkNamespace)); err != nil {
				return false, "", false, fmt.Errorf("removing %q namespace for run: %w", string(specs.NetworkNamespace), err)
			}
		}
	}
	if configureNetwork {
		if err := addSysctl([]string{"net"}); err != nil {
			return false, "", false, err
		}
	}
	return configureNetwork, networkString, configureUTS, nil
}

func (b *Builder) configureNamespaces(g *generate.Generator, options *RunOptions) (bool, string, error) {
	defaultNamespaceOptions, err := DefaultNamespaceOptions()
	if err != nil {
		return false, "", err
	}

	namespaceOptions := defaultNamespaceOptions
	namespaceOptions.AddOrReplace(b.NamespaceOptions...)
	namespaceOptions.AddOrReplace(options.NamespaceOptions...)

	networkPolicy := options.ConfigureNetwork
	// Nothing was specified explicitly so network policy should be inherited from builder
	if networkPolicy == NetworkDefault {
		networkPolicy = b.ConfigureNetwork

		// If builder policy was NetworkDisabled and
		// we want to disable network for this run.
		// reset options.ConfigureNetwork to NetworkDisabled
		// since it will be treated as source of truth later.
		if networkPolicy == NetworkDisabled {
			options.ConfigureNetwork = networkPolicy
		}
	}
	if networkPolicy == NetworkDisabled {
		namespaceOptions.AddOrReplace(define.NamespaceOptions{{Name: string(specs.NetworkNamespace), Host: false}}...)
	}
	configureNetwork, networkString, configureUTS, err := setupNamespaces(options.Logger, g, namespaceOptions, b.IDMappingOptions, networkPolicy)
	if err != nil {
		return false, "", err
	}

	if configureUTS {
		if options.Hostname != "" {
			g.SetHostname(options.Hostname)
		} else if b.Hostname() != "" {
			g.SetHostname(b.Hostname())
		} else {
			hostname := stringid.TruncateID(b.ContainerID)
			defConfig, err := config.Default()
			if err != nil {
				return false, "", fmt.Errorf("failed to get container config: %w", err)
			}
			if defConfig.Containers.ContainerNameAsHostName {
				if mapped := mapContainerNameToHostname(b.Container); mapped != "" {
					hostname = mapped
				}
			}
			g.SetHostname(hostname)
		}
	} else {
		g.SetHostname("")
	}

	found := false
	spec := g.Config
	for i := range spec.Process.Env {
		if strings.HasPrefix(spec.Process.Env[i], "HOSTNAME=") {
			found = true
			break
		}
	}
	if !found {
		spec.Process.Env = append(spec.Process.Env, fmt.Sprintf("HOSTNAME=%s", spec.Hostname))
	}

	return configureNetwork, networkString, nil
}

func runSetupBoundFiles(bundlePath string, bindFiles map[string]string) (mounts []specs.Mount) {
	for dest, src := range bindFiles {
		options := []string{"rbind"}
		if strings.HasPrefix(src, bundlePath) {
			options = append(options, bind.NoBindOption)
		}
		mounts = append(mounts, specs.Mount{
			Source:      src,
			Destination: dest,
			Type:        "bind",
			Options:     options,
		})
	}
	return mounts
}

func addRlimits(ulimit []string, g *generate.Generator, defaultUlimits []string) error {
	var (
		ul  *units.Ulimit
		err error
		// setup rlimits
		nofileSet bool
		nprocSet  bool
	)

	ulimit = append(defaultUlimits, ulimit...)
	for _, u := range ulimit {
		if ul, err = butil.ParseUlimit(u); err != nil {
			return fmt.Errorf("ulimit option %q requires name=SOFT:HARD, failed to be parsed: %w", u, err)
		}

		if strings.ToUpper(ul.Name) == "NOFILE" {
			nofileSet = true
		}
		if strings.ToUpper(ul.Name) == "NPROC" {
			nprocSet = true
		}
		g.AddProcessRlimits("RLIMIT_"+strings.ToUpper(ul.Name), uint64(ul.Hard), uint64(ul.Soft))
	}
	if !nofileSet {
		lim := define.RLimitDefaultValue
		var rlimit unix.Rlimit
		if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &rlimit); err == nil {
			if lim < rlimit.Max || unshare.IsRootless() {
				lim = rlimit.Max
			}
		} else {
			logrus.Warnf("Failed to return RLIMIT_NOFILE ulimit %q", err)
		}
		g.AddProcessRlimits("RLIMIT_NOFILE", lim, lim)
	}
	if !nprocSet {
		lim := define.RLimitDefaultValue
		var rlimit unix.Rlimit
		if err := unix.Getrlimit(unix.RLIMIT_NPROC, &rlimit); err == nil {
			if lim < rlimit.Max || unshare.IsRootless() {
				lim = rlimit.Max
			}
		} else {
			logrus.Warnf("Failed to return RLIMIT_NPROC ulimit %q", err)
		}
		g.AddProcessRlimits("RLIMIT_NPROC", lim, lim)
	}

	return nil
}

func (b *Builder) runSetupVolumeMounts(mountLabel string, volumeMounts []string, optionMounts []specs.Mount, idMaps IDMaps) (mounts []specs.Mount, overlayDirs []string, Err error) {
	// Make sure the overlay directory is clean before running
	containerDir, err := b.store.ContainerDirectory(b.ContainerID)
	if err != nil {
		return nil, nil, fmt.Errorf("looking up container directory for %s: %w", b.ContainerID, err)
	}
	if err := overlay.CleanupContent(containerDir); err != nil {
		return nil, nil, fmt.Errorf("cleaning up overlay content for %s: %w", b.ContainerID, err)
	}

	parseMount := func(mountType, host, container string, options []string) (specs.Mount, error) {
		var foundrw, foundro, foundz, foundZ, foundO, foundU bool
		var rootProp, upperDir, workDir string
		for _, opt := range options {
			switch opt {
			case "rw":
				foundrw = true
			case "ro":
				foundro = true
			case "z":
				foundz = true
			case "Z":
				foundZ = true
			case "O":
				foundO = true
			case "U":
				foundU = true
			case "private", "rprivate", "slave", "rslave", "shared", "rshared":
				rootProp = opt
			}

			if strings.HasPrefix(opt, "upperdir") {
				splitOpt := strings.SplitN(opt, "=", 2)
				if len(splitOpt) > 1 {
					upperDir = splitOpt[1]
				}
			}
			if strings.HasPrefix(opt, "workdir") {
				splitOpt := strings.SplitN(opt, "=", 2)
				if len(splitOpt) > 1 {
					workDir = splitOpt[1]
				}
			}
		}
		if !foundrw && !foundro {
			options = append(options, "rw")
		}
		if foundz {
			if err := relabel(host, mountLabel, true); err != nil {
				return specs.Mount{}, err
			}
			options = slices.DeleteFunc(options, func(o string) bool { return o == "z" })
		}
		if foundZ {
			if err := relabel(host, mountLabel, false); err != nil {
				return specs.Mount{}, err
			}
			options = slices.DeleteFunc(options, func(o string) bool { return o == "Z" })
		}
		if foundU {
			if err := chown.ChangeHostPathOwnership(host, true, idMaps.processUID, idMaps.processGID); err != nil {
				return specs.Mount{}, err
			}
			options = slices.DeleteFunc(options, func(o string) bool { return o == "U" })
		}
		if foundO {
			if (upperDir != "" && workDir == "") || (workDir != "" && upperDir == "") {
				return specs.Mount{}, errors.New("if specifying upperdir then workdir must be specified or vice versa")
			}

			containerDir, err := b.store.ContainerDirectory(b.ContainerID)
			if err != nil {
				return specs.Mount{}, err
			}

			contentDir, err := overlay.TempDir(containerDir, idMaps.rootUID, idMaps.rootGID)
			if err != nil {
				return specs.Mount{}, fmt.Errorf("failed to create TempDir in the %s directory: %w", containerDir, err)
			}

			overlayOpts := overlay.Options{
				RootUID:                idMaps.rootUID,
				RootGID:                idMaps.rootGID,
				UpperDirOptionFragment: upperDir,
				WorkDirOptionFragment:  workDir,
				GraphOpts:              slices.Clone(b.store.GraphOptions()),
			}

			overlayMount, err := overlay.MountWithOptions(contentDir, host, container, &overlayOpts)
			if err == nil {
				overlayDirs = append(overlayDirs, contentDir)
			}

			// If chown true, add correct ownership to the overlay temp directories.
			if err == nil && foundU {
				if err := chown.ChangeHostPathOwnership(contentDir, true, idMaps.processUID, idMaps.processGID); err != nil {
					return specs.Mount{}, err
				}
			}

			return overlayMount, err
		}
		if rootProp == "" {
			options = append(options, "private")
		}
		if mountType != "tmpfs" {
			mountType = "bind"
			options = append(options, "rbind")
		}
		return specs.Mount{
			Destination: container,
			Type:        mountType,
			Source:      host,
			Options:     options,
		}, nil
	}

	// Bind mount volumes specified for this particular Run() invocation
	for _, i := range optionMounts {
		logrus.Debugf("setting up mounted volume at %q", i.Destination)
		mount, err := parseMount(i.Type, i.Source, i.Destination, i.Options)
		if err != nil {
			return nil, nil, err
		}
		mounts = append(mounts, mount)
	}
	// Bind mount volumes given by the user when the container was created
	for _, i := range volumeMounts {
		var options []string
		spliti := parse.SplitStringWithColonEscape(i)
		if len(spliti) > 2 {
			options = strings.Split(spliti[2], ",")
		}
		options = append(options, "rbind")
		mount, err := parseMount("bind", spliti[0], spliti[1], options)
		if err != nil {
			return nil, nil, err
		}
		mounts = append(mounts, mount)
	}
	return mounts, overlayDirs, nil
}

func setupMaskedPaths(g *generate.Generator, opts *define.CommonBuildOptions) {
	if slices.Contains(opts.Unmasks, "all") {
		return
	}
nextMaskedPath:
	for _, mp := range append(config.DefaultMaskedPaths(), opts.Masks...) {
		for _, unmask := range opts.Unmasks {
			match, err := filepath.Match(unmask, mp)
			if err != nil {
				logrus.Warnf("Invalid unmask pattern %q: %v", unmask, err)
				continue
			}
			if match {
				continue nextMaskedPath
			}
		}
		g.AddLinuxMaskedPaths(mp)
	}
}

func setupReadOnlyPaths(g *generate.Generator) {
	for _, rp := range config.DefaultReadOnlyPaths {
		g.AddLinuxReadonlyPaths(rp)
	}
}

func setupCapAdd(g *generate.Generator, caps ...string) error {
	for _, cap := range caps {
		if err := g.AddProcessCapabilityBounding(cap); err != nil {
			return fmt.Errorf("adding %q to the bounding capability set: %w", cap, err)
		}
		if err := g.AddProcessCapabilityEffective(cap); err != nil {
			return fmt.Errorf("adding %q to the effective capability set: %w", cap, err)
		}
		if err := g.AddProcessCapabilityPermitted(cap); err != nil {
			return fmt.Errorf("adding %q to the permitted capability set: %w", cap, err)
		}
	}
	return nil
}

func setupCapDrop(g *generate.Generator, caps ...string) error {
	for _, cap := range caps {
		if err := g.DropProcessCapabilityBounding(cap); err != nil {
			return fmt.Errorf("removing %q from the bounding capability set: %w", cap, err)
		}
		if err := g.DropProcessCapabilityEffective(cap); err != nil {
			return fmt.Errorf("removing %q from the effective capability set: %w", cap, err)
		}
		if err := g.DropProcessCapabilityPermitted(cap); err != nil {
			return fmt.Errorf("removing %q from the permitted capability set: %w", cap, err)
		}
	}
	return nil
}

func setupCapabilities(g *generate.Generator, defaultCapabilities, adds, drops []string) error {
	g.ClearProcessCapabilities()
	if err := setupCapAdd(g, defaultCapabilities...); err != nil {
		return err
	}
	for _, c := range adds {
		if strings.ToLower(c) == "all" {
			adds = capabilities.AllCapabilities()
			break
		}
	}
	for _, c := range drops {
		if strings.ToLower(c) == "all" {
			g.ClearProcessCapabilities()
			return nil
		}
	}
	if err := setupCapAdd(g, adds...); err != nil {
		return err
	}
	return setupCapDrop(g, drops...)
}

func addOrReplaceMount(mounts []specs.Mount, mount specs.Mount) []specs.Mount {
	for i := range mounts {
		if mounts[i].Destination == mount.Destination {
			mounts[i] = mount
			return mounts
		}
	}
	return append(mounts, mount)
}

// setupSpecialMountSpecChanges creates special mounts for depending on the namespaces
// logic taken from podman and adapted for buildah
// https://github.com/containers/podman/blob/4ba71f955a944790edda6e007e6d074009d437a7/pkg/specgen/generate/oci.go#L178
func setupSpecialMountSpecChanges(spec *specs.Spec, shmSize string) ([]specs.Mount, error) {
	mounts := spec.Mounts
	isRootless := unshare.IsRootless()
	isNewUserns := false
	isNetns := false
	isPidns := false
	isIpcns := false

	for _, namespace := range spec.Linux.Namespaces {
		switch namespace.Type {
		case specs.NetworkNamespace:
			isNetns = true
		case specs.UserNamespace:
			isNewUserns = true
		case specs.PIDNamespace:
			isPidns = true
		case specs.IPCNamespace:
			isIpcns = true
		}
	}

	addCgroup := true
	// mount sys when root and no userns or when a new netns is created
	canMountSys := (!isRootless && !isNewUserns) || isNetns
	if !canMountSys {
		addCgroup = false
		sys := "/sys"
		sysMnt := specs.Mount{
			Destination: sys,
			Type:        "bind",
			Source:      sys,
			Options:     []string{bind.NoBindOption, "rprivate", "nosuid", "noexec", "nodev", "ro", "rbind"},
		}
		mounts = addOrReplaceMount(mounts, sysMnt)
	}

	gid5Available := true
	if isRootless {
		_, gids, err := unshare.GetHostIDMappings("")
		if err != nil {
			return nil, err
		}
		gid5Available = checkIDsGreaterThan5(gids)
	}
	if gid5Available && len(spec.Linux.GIDMappings) > 0 {
		gid5Available = checkIDsGreaterThan5(spec.Linux.GIDMappings)
	}
	if !gid5Available {
		// If we have no GID mappings, the gid=5 default option would fail, so drop it.
		devPts := specs.Mount{
			Destination: "/dev/pts",
			Type:        "devpts",
			Source:      "devpts",
			Options:     []string{"rprivate", "nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620"},
		}
		mounts = addOrReplaceMount(mounts, devPts)
	}

	isUserns := isNewUserns || isRootless

	if isUserns && !isIpcns {
		devMqueue := "/dev/mqueue"
		devMqueueMnt := specs.Mount{
			Destination: devMqueue,
			Type:        "bind",
			Source:      devMqueue,
			Options:     []string{bind.NoBindOption, "bind", "nosuid", "noexec", "nodev"},
		}
		mounts = addOrReplaceMount(mounts, devMqueueMnt)
	}
	if isUserns && !isPidns {
		proc := "/proc"
		procMount := specs.Mount{
			Destination: proc,
			Type:        "bind",
			Source:      proc,
			Options:     []string{bind.NoBindOption, "rbind", "nosuid", "noexec", "nodev"},
		}
		mounts = addOrReplaceMount(mounts, procMount)
	}

	if addCgroup {
		cgroupMnt := specs.Mount{
			Destination: "/sys/fs/cgroup",
			Type:        "cgroup",
			Source:      "cgroup",
			Options:     []string{"rprivate", "nosuid", "noexec", "nodev", "relatime", "rw"},
		}
		mounts = addOrReplaceMount(mounts, cgroupMnt)
	}

	// if userns and host ipc bind mount shm
	if isUserns && !isIpcns {
		// bind mount /dev/shm when it exists
		if err := fileutils.Exists("/dev/shm"); err == nil {
			shmMount := specs.Mount{
				Source:      "/dev/shm",
				Type:        "bind",
				Destination: "/dev/shm",
				Options:     []string{bind.NoBindOption, "rbind", "nosuid", "noexec", "nodev"},
			}
			mounts = addOrReplaceMount(mounts, shmMount)
		}
	} else if shmSize != "" {
		shmMount := specs.Mount{
			Source:      "shm",
			Destination: "/dev/shm",
			Type:        "tmpfs",
			Options:     []string{"private", "nodev", "noexec", "nosuid", "mode=1777", "size=" + shmSize},
		}
		mounts = addOrReplaceMount(mounts, shmMount)
	}

	return mounts, nil
}

func checkIDsGreaterThan5(ids []specs.LinuxIDMapping) bool {
	for _, r := range ids {
		if r.ContainerID <= 5 && 5 < r.ContainerID+r.Size {
			return true
		}
	}
	return false
}

// Returns a Mount to add to the runtime spec's list of mounts, the ID of an
// image, the path to a mounted filesystem, and the path to an overlay
// filesystem, and an optional lock, or an error.
//
// The caller is expected to, after the command which uses the mount exits,
// clean up the overlay filesystem (if we returned one), unmount the mounted
// filesystem (if we provided the path to its mountpoint) and remove its
// mountpoint, unmount the image (if we mounted one), and release the lock (if
// we took one).
func (b *Builder) getCacheMount(tokens []string, sys *types.SystemContext, stageMountPoints map[string]internal.StageMountDetails, idMaps IDMaps, workDir, tmpDir string) (*specs.Mount, string, string, string, *lockfile.LockFile, error) {
	var optionMounts []specs.Mount
	optionMount, mountedImage, intermediateMount, overlayMount, targetLock, err := volumes.GetCacheMount(sys, tokens, b.store, b.MountLabel, stageMountPoints, idMaps.uidmap, idMaps.gidmap, workDir, tmpDir)
	if err != nil {
		return nil, "", "", "", nil, err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			if overlayMount != "" {
				if err := overlay.RemoveTemp(overlayMount); err != nil {
					b.Logger.Debug(err.Error())
				}
			}
			if intermediateMount != "" {
				if err := mount.Unmount(intermediateMount); err != nil {
					b.Logger.Debugf("unmounting %q: %v", intermediateMount, err)
				}
				if err := os.Remove(intermediateMount); err != nil {
					b.Logger.Debugf("removing should-be-empty directory %q: %v", intermediateMount, err)
				}
			}
			if mountedImage != "" {
				if _, err := b.store.UnmountImage(mountedImage, false); err != nil {
					b.Logger.Debugf("unmounting image %q: %v", mountedImage, err)
				}
			}
			if targetLock != nil {
				targetLock.Unlock()
			}
		}
	}()
	optionMounts = append(optionMounts, optionMount)
	volumes, overlayDirs, err := b.runSetupVolumeMounts(b.MountLabel, nil, optionMounts, idMaps)
	if err != nil {
		return nil, "", "", "", nil, err
	}
	if len(overlayDirs) != 0 {
		return nil, "", "", "", nil, errors.New("internal error: did not expect a resolved cache mount to use the O flag")
	}
	succeeded = true
	return &volumes[0], mountedImage, intermediateMount, overlayMount, targetLock, nil
}
