//go:build freebsd

package buildah

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unsafe"

	"github.com/containers/buildah/bind"
	"github.com/containers/buildah/chroot"
	"github.com/containers/buildah/copier"
	"github.com/containers/buildah/define"
	"github.com/containers/buildah/internal"
	"github.com/containers/buildah/internal/tmpdir"
	"github.com/containers/buildah/pkg/jail"
	"github.com/containers/buildah/pkg/overlay"
	"github.com/containers/buildah/pkg/parse"
	butil "github.com/containers/buildah/pkg/util"
	"github.com/containers/buildah/util"
	"github.com/docker/go-units"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/etchosts"
	"go.podman.io/common/libnetwork/resolvconf"
	nettypes "go.podman.io/common/libnetwork/types"
	netUtil "go.podman.io/common/libnetwork/util"
	"go.podman.io/common/pkg/config"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/lockfile"
	"go.podman.io/storage/pkg/stringid"
	"golang.org/x/sys/unix"
)

const (
	P_PID             = 0
	P_PGID            = 2
	PROC_REAP_ACQUIRE = 2
	PROC_REAP_RELEASE = 3
)

func procctl(idtype int, id int, cmd int, arg *byte) error {
	_, _, e1 := unix.Syscall6(
		unix.SYS_PROCCTL, uintptr(idtype), uintptr(id),
		uintptr(cmd), uintptr(unsafe.Pointer(arg)), 0, 0)
	if e1 != 0 {
		return unix.Errno(e1)
	}
	return nil
}

func setChildProcess() error {
	if err := procctl(P_PID, unix.Getpid(), PROC_REAP_ACQUIRE, nil); err != nil {
		fmt.Fprintf(os.Stderr, "procctl(PROC_REAP_ACQUIRE): %v\n", err)
		return err
	}
	return nil
}

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
			logrus.Errorf("error removing %q: %v", path, err2)
		}
	}()

	gp, err := generate.New("freebsd")
	if err != nil {
		return fmt.Errorf("generating new 'freebsd' runtime spec: %w", err)
	}
	g := &gp

	isolation := options.Isolation
	if isolation == IsolationDefault {
		isolation = b.Isolation
		if isolation == IsolationDefault {
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
	mountPoint, err := b.Mount(b.MountLabel)
	if err != nil {
		return fmt.Errorf("mounting container %q: %w", b.ContainerID, err)
	}
	defer func() {
		if err := b.Unmount(); err != nil {
			logrus.Errorf("error unmounting container: %v", err)
		}
	}()
	g.SetRootPath(mountPoint)
	if len(command) > 0 {
		command = runLookupPath(g, command)
		g.SetProcessArgs(command)
	} else {
		g.SetProcessArgs(nil)
	}

	setupTerminal(g, options.Terminal, options.TerminalSize)

	configureNetwork, networkString, err := b.configureNamespaces(g, &options)
	if err != nil {
		return err
	}

	containerName := Package + "-" + filepath.Base(path)
	if configureNetwork {
		if jail.NeedVnetJail() {
			g.AddAnnotation("org.freebsd.parentJail", containerName+"-vnet")
		} else {
			g.AddAnnotation("org.freebsd.jail.vnet", "new")
		}
	}

	homeDir, err := b.configureUIDGID(g, mountPoint, options)
	if err != nil {
		return err
	}

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

		// Only add entries here if we do not have to setup network,
		// if we do we have to do it much later after the network setup.
		if !configureNetwork {
			var entries etchosts.HostEntries
			// add host entry for local ip when running in host network
			if spec.Hostname != "" {
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

	resolvFile := ""
	if !slices.Contains(volumes, resolvconf.DefaultResolvConf) && options.ConfigureNetwork != define.NetworkDisabled && !(len(b.CommonBuildOpts.DNSServers) == 1 && strings.ToLower(b.CommonBuildOpts.DNSServers[0]) == "none") {
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

	// If we are creating a network, make the vnet here so that we can
	// execute the OCI runtime inside it. For FreeBSD-13.3 and later, we can
	// configure the container network settings from outside the jail, which
	// removes the need for a separate jail to manage the vnet.
	if configureNetwork && jail.NeedVnetJail() {
		mynetns := containerName + "-vnet"

		jconf := jail.NewConfig()
		jconf.Set("name", mynetns)
		jconf.Set("vnet", jail.NEW)
		jconf.Set("children.max", 1)
		jconf.Set("persist", true)
		jconf.Set("enforce_statfs", 0)
		jconf.Set("devfs_ruleset", 4)
		jconf.Set("allow.raw_sockets", true)
		jconf.Set("allow.chflags", true)
		jconf.Set("securelevel", -1)
		netjail, err := jail.Create(jconf)
		if err != nil {
			return err
		}
		defer func() {
			jconf := jail.NewConfig()
			jconf.Set("persist", false)
			err2 := netjail.Set(jconf)
			if err2 != nil {
				logrus.Errorf("error releasing vnet jail %q: %v", mynetns, err2)
			}
		}()
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

	switch isolation {
	case IsolationOCI:
		var moreCreateArgs []string
		if options.NoPivot {
			moreCreateArgs = []string{"--no-pivot"}
		} else {
			moreCreateArgs = nil
		}
		err = b.runUsingRuntimeSubproc(isolation, options, configureNetwork, networkString, moreCreateArgs, spec, mountPoint, path, containerName, b.Container, hostsFile, resolvFile)
	case IsolationChroot:
		err = chroot.RunUsingChroot(spec, path, homeDir, options.Stdin, options.Stdout, options.Stderr, options.NoPivot)
	default:
		err = errors.New("don't know how to run this command")
	}
	return err
}

func addCommonOptsToSpec(commonOpts *define.CommonBuildOptions, g *generate.Generator) error {
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

// setupSpecialMountSpecChanges creates special mounts for depending
// on the namespaces - nothing yet for freebsd
func setupSpecialMountSpecChanges(spec *specs.Spec, shmSize string) ([]specs.Mount, error) {
	return spec.Mounts, nil
}

// If this succeeded, the caller would be expected to, after the command which
// uses the mount exits, clean up the overlay filesystem (if we returned one),
// unmount the mounted filesystem (if we provided the path to its mountpoint)
// and remove its mountpoint, unmount the image (if we mounted one), and
// release the lock (if we took one).
func (b *Builder) getCacheMount(tokens []string, sys *types.SystemContext, stageMountPoints map[string]internal.StageMountDetails, idMaps IDMaps, workDir, tmpDir string) (*specs.Mount, string, string, string, *lockfile.LockFile, error) {
	return nil, "", "", "", nil, errors.New("cache mounts not supported on freebsd")
}

func (b *Builder) runSetupVolumeMounts(mountLabel string, volumeMounts []string, optionMounts []specs.Mount, idMaps IDMaps) (mounts []specs.Mount, overlayDirs []string, Err error) {
	// Make sure the overlay directory is clean before running
	_, err := b.store.ContainerDirectory(b.ContainerID)
	if err != nil {
		return nil, nil, fmt.Errorf("looking up container directory for %s: %w", b.ContainerID, err)
	}

	parseMount := func(mountType, host, container string, options []string) (specs.Mount, error) {
		var foundrw, foundro, foundO bool
		var upperDir string
		for _, opt := range options {
			switch opt {
			case "rw":
				foundrw = true
			case "ro":
				foundro = true
			case "O":
				foundO = true
			}
			if strings.HasPrefix(opt, "upperdir") {
				splitOpt := strings.SplitN(opt, "=", 2)
				if len(splitOpt) > 1 {
					upperDir = splitOpt[1]
				}
			}
		}
		if !foundrw && !foundro {
			options = append(options, "rw")
		}
		if mountType == "bind" || mountType == "rbind" {
			mountType = "nullfs"
		}
		if foundO {
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
				GraphOpts:              b.store.GraphOptions(),
			}

			overlayMount, err := overlay.MountWithOptions(contentDir, host, container, &overlayOpts)
			if err == nil {
				overlayDirs = append(overlayDirs, contentDir)
			}
			return overlayMount, err
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
		spliti := strings.Split(i, ":")
		if len(spliti) > 2 {
			options = strings.Split(spliti[2], ",")
		}
		mount, err := parseMount("nullfs", spliti[0], spliti[1], options)
		if err != nil {
			return nil, nil, err
		}
		mounts = append(mounts, mount)
	}
	return mounts, overlayDirs, nil
}

func setupCapabilities(g *generate.Generator, defaultCapabilities, adds, drops []string) error {
	return nil
}

func (b *Builder) runConfigureNetwork(pid int, isolation define.Isolation, options RunOptions, networkString string, containerName string, hostnames []string) (func(), *netResult, error) {
	//if isolation == IsolationOCIRootless {
	//return setupRootlessNetwork(pid)
	//}

	var configureNetworks []string
	if len(networkString) > 0 {
		configureNetworks = strings.Split(networkString, ",")
	}

	if len(configureNetworks) == 0 {
		configureNetworks = []string{b.NetworkInterface.DefaultNetworkName()}
	}
	logrus.Debugf("configureNetworks: %v", configureNetworks)

	var mynetns string
	if jail.NeedVnetJail() {
		mynetns = containerName + "-vnet"
	} else {
		mynetns = containerName
	}

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
			logrus.Errorf("failed to cleanup network: %v", err)
		}
	}

	return teardown, netStatusToNetResult(netStatus, hostnames), nil
}

func setupNamespaces(logger *logrus.Logger, g *generate.Generator, namespaceOptions define.NamespaceOptions, idmapOptions define.IDMappingOptions, policy define.NetworkConfigurationPolicy) (configureNetwork bool, networkString string, configureUTS bool, err error) {
	// Set namespace options in the container configuration.
	for _, namespaceOption := range namespaceOptions {
		switch namespaceOption.Name {
		case string(specs.NetworkNamespace):
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
			if !namespaceOption.Host && namespaceOption.Path == "" {
				configureUTS = true
			}
		}
		// TODO: re-visit this when there is consensus on a
		// FreeBSD runtime-spec. FreeBSD jails have rough
		// equivalents for UTS and and network namespaces.
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
		options := []string{}
		if strings.HasPrefix(src, bundlePath) {
			options = append(options, bind.NoBindOption)
		}
		mounts = append(mounts, specs.Mount{
			Source:      src,
			Destination: dest,
			Type:        "nullfs",
			Options:     options,
		})
	}
	return mounts
}

func addRlimits(ulimit []string, g *generate.Generator, defaultUlimits []string) error {
	var (
		ul  *units.Ulimit
		err error
	)

	ulimit = append(defaultUlimits, ulimit...)
	for _, u := range ulimit {
		if ul, err = butil.ParseUlimit(u); err != nil {
			return fmt.Errorf("ulimit option %q requires name=SOFT:HARD, failed to be parsed: %w", u, err)
		}

		g.AddProcessRlimits("RLIMIT_"+strings.ToUpper(ul.Name), uint64(ul.Hard), uint64(ul.Soft))
	}
	return nil
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
	return stdioPipe, nil
}
