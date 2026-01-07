//go:build linux || freebsd

package buildah

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/containers/buildah/bind"
	"github.com/containers/buildah/copier"
	"github.com/containers/buildah/define"
	"github.com/containers/buildah/internal"
	"github.com/containers/buildah/internal/tmpdir"
	"github.com/containers/buildah/internal/volumes"
	"github.com/containers/buildah/pkg/overlay"
	"github.com/containers/buildah/pkg/sshagent"
	"github.com/containers/buildah/util"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/etchosts"
	"go.podman.io/common/libnetwork/network"
	"go.podman.io/common/libnetwork/resolvconf"
	netTypes "go.podman.io/common/libnetwork/types"
	netUtil "go.podman.io/common/libnetwork/util"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/subscriptions"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/pkg/lockfile"
	"go.podman.io/storage/pkg/mount"
	"go.podman.io/storage/pkg/reexec"
	"go.podman.io/storage/pkg/regexp"
	"go.podman.io/storage/pkg/unshare"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const maxHostnameLen = 64

var validHostnames = regexp.Delayed("[A-Za-z0-9][A-Za-z0-9.-]+")

func (b *Builder) createResolvConf(rdir string, chownOpts *idtools.IDPair) (string, error) {
	cfile := filepath.Join(rdir, "resolv.conf")
	f, err := os.Create(cfile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	uid := 0
	gid := 0
	if chownOpts != nil {
		uid = chownOpts.UID
		gid = chownOpts.GID
	}
	if err = f.Chown(uid, gid); err != nil {
		return "", err
	}

	if err := relabel(cfile, b.MountLabel, false); err != nil {
		return "", err
	}
	return cfile, nil
}

// addResolvConf copies files from host and sets them up to bind mount into container
func (b *Builder) addResolvConfEntries(file string, networkNameServer []string,
	spec *specs.Spec, keepHostServers, ipv6 bool,
) error {
	defaultConfig, err := config.Default()
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	var namespaces []specs.LinuxNamespace
	if spec.Linux != nil {
		namespaces = spec.Linux.Namespaces
	}

	dnsServers, dnsSearch, dnsOptions := b.CommonBuildOpts.DNSServers, b.CommonBuildOpts.DNSSearch, b.CommonBuildOpts.DNSOptions
	nameservers := make([]string, 0, len(defaultConfig.Containers.DNSServers.Get())+len(dnsServers))
	nameservers = append(nameservers, defaultConfig.Containers.DNSServers.Get()...)
	nameservers = append(nameservers, dnsServers...)

	searches := make([]string, 0, len(defaultConfig.Containers.DNSSearches.Get())+len(dnsSearch))
	searches = append(searches, defaultConfig.Containers.DNSSearches.Get()...)
	searches = append(searches, dnsSearch...)

	options := make([]string, 0, len(defaultConfig.Containers.DNSOptions.Get())+len(dnsOptions))
	options = append(options, defaultConfig.Containers.DNSOptions.Get()...)
	options = append(options, dnsOptions...)

	if len(nameservers) == 0 {
		nameservers = networkNameServer
	}

	if err := resolvconf.New(&resolvconf.Params{
		Path:            file,
		Namespaces:      namespaces,
		IPv6Enabled:     ipv6,
		KeepHostServers: keepHostServers,
		Nameservers:     nameservers,
		Searches:        searches,
		Options:         options,
	}); err != nil {
		return fmt.Errorf("building resolv.conf for container %s: %w", b.ContainerID, err)
	}

	return nil
}

// createHostsFile creates a containers hosts file
func (b *Builder) createHostsFile(rdir string, chownOpts *idtools.IDPair) (string, error) {
	targetfile := filepath.Join(rdir, "hosts")
	f, err := os.Create(targetfile)
	if err != nil {
		return "", err
	}
	defer f.Close()
	uid := 0
	gid := 0
	if chownOpts != nil {
		uid = chownOpts.UID
		gid = chownOpts.GID
	}
	if err := f.Chown(uid, gid); err != nil {
		return "", err
	}
	if err := relabel(targetfile, b.MountLabel, false); err != nil {
		return "", err
	}

	return targetfile, nil
}

func (b *Builder) addHostsEntries(file, imageRoot string, entries etchosts.HostEntries, exclude []net.IP, preferIP string) error {
	conf, err := config.Default()
	if err != nil {
		return err
	}

	base, err := etchosts.GetBaseHostFile(conf.Containers.BaseHostsFile, imageRoot)
	if err != nil {
		return err
	}
	return etchosts.New(&etchosts.Params{
		BaseFile:   base,
		ExtraHosts: b.CommonBuildOpts.AddHost,
		HostContainersInternalIP: etchosts.GetHostContainersInternalIP(etchosts.HostContainersInternalOptions{
			Conf:     conf,
			Exclude:  exclude,
			PreferIP: preferIP,
		}),
		TargetFile:   file,
		ContainerIPs: entries,
	})
}

// generateHostname creates a containers /etc/hostname file
func (b *Builder) generateHostname(rdir, hostname string, chownOpts *idtools.IDPair) (string, error) {
	cfile := filepath.Join(rdir, "hostname")
	if err := ioutils.AtomicWriteFile(cfile, append([]byte(hostname), '\n'), 0o644); err != nil {
		return "", fmt.Errorf("writing /etc/hostname into the container: %w", err)
	}

	uid := 0
	gid := 0
	if chownOpts != nil {
		uid = chownOpts.UID
		gid = chownOpts.GID
	}
	if err := os.Chown(cfile, uid, gid); err != nil {
		return "", err
	}
	if err := relabel(cfile, b.MountLabel, false); err != nil {
		return "", err
	}

	return cfile, nil
}

func setupTerminal(g *generate.Generator, terminalPolicy TerminalPolicy, terminalSize *specs.Box) {
	switch terminalPolicy {
	case DefaultTerminal:
		onTerminal := term.IsTerminal(unix.Stdin) && term.IsTerminal(unix.Stdout) && term.IsTerminal(unix.Stderr)
		if onTerminal {
			logrus.Debugf("stdio is a terminal, defaulting to using a terminal")
		} else {
			logrus.Debugf("stdio is not a terminal, defaulting to not using a terminal")
		}
		g.SetProcessTerminal(onTerminal)
	case WithTerminal:
		g.SetProcessTerminal(true)
	case WithoutTerminal:
		g.SetProcessTerminal(false)
	}
	if terminalSize != nil {
		g.SetProcessConsoleSize(terminalSize.Width, terminalSize.Height)
	}
}

// Search for a command that isn't given as an absolute path using the $PATH
// under the rootfs.  We can't resolve absolute symbolic links without
// chroot()ing, which we may not be able to do, so just accept a link as a
// valid resolution.
func runLookupPath(g *generate.Generator, command []string) []string {
	// Look for the configured $PATH.
	spec := g.Config
	envPath := ""
	for i := range spec.Process.Env {
		if strings.HasPrefix(spec.Process.Env[i], "PATH=") {
			envPath = spec.Process.Env[i]
		}
	}
	// If there is no configured $PATH, supply one.
	if envPath == "" {
		defaultPath := "/usr/local/bin:/usr/local/sbin:/usr/bin:/usr/sbin:/bin:/sbin"
		envPath = "PATH=" + defaultPath
		g.AddProcessEnv("PATH", defaultPath)
	}
	// No command, nothing to do.
	if len(command) == 0 {
		return command
	}
	// Command is already an absolute path, use it as-is.
	if filepath.IsAbs(command[0]) {
		return command
	}
	// For each element in the PATH,
	for _, pathEntry := range filepath.SplitList(envPath[5:]) {
		// if it's the empty string, it's ".", which is the Cwd,
		if pathEntry == "" {
			pathEntry = spec.Process.Cwd
		}
		// build the absolute path which it might be,
		candidate := filepath.Join(pathEntry, command[0])
		// check if it's there,
		if fi, err := os.Lstat(filepath.Join(spec.Root.Path, candidate)); fi != nil && err == nil {
			// and if it's not a directory, and either a symlink or executable,
			if !fi.IsDir() && ((fi.Mode()&os.ModeSymlink != 0) || (fi.Mode()&0o111 != 0)) {
				// use that.
				return append([]string{candidate}, command[1:]...)
			}
		}
	}
	return command
}

func (b *Builder) configureUIDGID(g *generate.Generator, mountPoint string, options RunOptions) (string, error) {
	// Set the user UID/GID/supplemental group list/capabilities lists.
	user, homeDir, err := b.userForRun(mountPoint, options.User)
	if err != nil {
		return "", err
	}
	if err := setupCapabilities(g, b.Capabilities, options.AddCapabilities, options.DropCapabilities); err != nil {
		return "", err
	}
	g.SetProcessUID(user.UID)
	g.SetProcessGID(user.GID)
	g.AddProcessAdditionalGid(user.GID)
	for _, gid := range user.AdditionalGids {
		g.AddProcessAdditionalGid(gid)
	}
	for _, group := range b.GroupAdd {
		if group == "keep-groups" {
			if len(b.GroupAdd) > 1 {
				return "", errors.New("the '--group-add keep-groups' option is not allowed with any other --group-add options")
			}
			g.AddAnnotation("run.oci.keep_original_groups", "1")
			continue
		}
		gid, err := strconv.ParseUint(group, 10, 32)
		if err != nil {
			return "", err
		}
		g.AddProcessAdditionalGid(uint32(gid))
	}

	// Remove capabilities if not running as root except Bounding set
	if user.UID != 0 && g.Config.Process.Capabilities != nil {
		bounding := g.Config.Process.Capabilities.Bounding
		g.ClearProcessCapabilities()
		g.Config.Process.Capabilities.Bounding = bounding
	}

	return homeDir, nil
}

func (b *Builder) configureEnvironment(g *generate.Generator, options RunOptions, defaultEnv []string) {
	g.ClearProcessEnv()

	if b.CommonBuildOpts.HTTPProxy {
		for _, envSpec := range config.ProxyEnv {
			if envVal, ok := os.LookupEnv(envSpec); ok {
				g.AddProcessEnv(envSpec, envVal)
			}
		}
	}

	for _, envSpec := range util.MergeEnv(util.MergeEnv(defaultEnv, b.Env()), options.Env) {
		env := strings.SplitN(envSpec, "=", 2)
		if len(env) > 1 {
			g.AddProcessEnv(env[0], env[1])
		}
	}
}

// getNetworkInterface creates the network interface
func getNetworkInterface(store storage.Store, cniConfDir, cniPluginPath string) (netTypes.ContainerNetwork, error) {
	conf, err := config.Default()
	if err != nil {
		return nil, err
	}
	// copy the config to not modify the default by accident
	newconf := *conf
	if len(cniConfDir) > 0 {
		newconf.Network.NetworkConfigDir = cniConfDir
	}
	if len(cniPluginPath) > 0 {
		plugins := strings.Split(cniPluginPath, string(os.PathListSeparator))
		newconf.Network.CNIPluginDirs.Set(plugins)
	}

	_, netInt, err := network.NetworkBackend(store, &newconf, false)
	if err != nil {
		return nil, err
	}
	return netInt, nil
}

func netStatusToNetResult(netStatus map[string]netTypes.StatusBlock, hostnames []string) *netResult {
	result := &netResult{
		keepHostResolvers: false,
	}
	for _, status := range netStatus {
		for _, dns := range status.DNSServerIPs {
			result.dnsServers = append(result.dnsServers, dns.String())
		}
		for _, netInt := range status.Interfaces {
			for _, netAddress := range netInt.Subnets {
				e := etchosts.HostEntry{IP: netAddress.IPNet.IP.String(), Names: hostnames}
				result.entries = append(result.entries, e)
				if !result.ipv6 && netUtil.IsIPv6(netAddress.IPNet.IP) {
					result.ipv6 = true
				}
			}
		}
	}
	return result
}

// DefaultNamespaceOptions returns the default namespace settings from the
// runtime-tools generator library.
func DefaultNamespaceOptions() (define.NamespaceOptions, error) {
	cfg, err := config.Default()
	if err != nil {
		return nil, fmt.Errorf("failed to get container config: %w", err)
	}
	options := define.NamespaceOptions{
		{Name: string(specs.CgroupNamespace), Host: cfg.CgroupNS() == "host"},
		{Name: string(specs.IPCNamespace), Host: cfg.IPCNS() == "host"},
		{Name: string(specs.MountNamespace), Host: false},
		{Name: string(specs.NetworkNamespace), Host: cfg.NetNS() == "host"},
		{Name: string(specs.PIDNamespace), Host: cfg.PidNS() == "host"},
		{Name: string(specs.UserNamespace), Host: cfg.Containers.UserNS == "" || cfg.Containers.UserNS == "host"},
		{Name: string(specs.UTSNamespace), Host: cfg.UTSNS() == "host"},
	}
	return options, nil
}

func checkAndOverrideIsolationOptions(isolation define.Isolation, options *RunOptions) error {
	switch isolation {
	case IsolationOCIRootless:
		// only change the netns if the caller did not set it
		if ns := options.NamespaceOptions.Find(string(specs.NetworkNamespace)); ns == nil {
			if _, err := exec.LookPath("slirp4netns"); err != nil {
				// if slirp4netns is not installed we have to use the hosts net namespace
				options.NamespaceOptions.AddOrReplace(define.NamespaceOption{Name: string(specs.NetworkNamespace), Host: true})
			}
		}
		fallthrough
	case IsolationOCI:
		pidns := options.NamespaceOptions.Find(string(specs.PIDNamespace))
		userns := options.NamespaceOptions.Find(string(specs.UserNamespace))
		if (pidns != nil && pidns.Host) && (userns != nil && !userns.Host) {
			return fmt.Errorf("not allowed to mix host PID namespace with container user namespace")
		}
	case IsolationChroot:
		logrus.Info("network namespace isolation not supported with chroot isolation, forcing host network")
		options.NamespaceOptions.AddOrReplace(define.NamespaceOption{Name: string(specs.NetworkNamespace), Host: true})
	}
	return nil
}

// fileCloser is a helper struct to prevent closing the file twice in the code
// users must call (fileCloser).Close() and not fileCloser.File.Close()
type fileCloser struct {
	file   *os.File
	closed bool
}

func (f *fileCloser) Close() {
	if !f.closed {
		if err := f.file.Close(); err != nil {
			logrus.Errorf("failed to close file: %v", err)
		}
		f.closed = true
	}
}

// waitForSync waits for a maximum of 4 minutes to read something from the file
func waitForSync(pipeR *os.File) error {
	if err := pipeR.SetDeadline(time.Now().Add(4 * time.Minute)); err != nil {
		return err
	}
	b := make([]byte, 16)
	_, err := pipeR.Read(b)
	return err
}

func runUsingRuntime(options RunOptions, configureNetwork bool, moreCreateArgs []string, spec *specs.Spec, bundlePath, containerName string,
	containerCreateW io.WriteCloser, containerStartR io.ReadCloser,
) (wstatus unix.WaitStatus, err error) {
	if options.Logger == nil {
		options.Logger = logrus.StandardLogger()
	}

	// Lock the caller to a single OS-level thread.
	runtime.LockOSThread()
	defer reapStrays()

	// Set up bind mounts for things that a namespaced user might not be able to get to directly.
	unmountAll, err := bind.SetupIntermediateMountNamespace(spec, bundlePath)
	if unmountAll != nil {
		defer func() {
			if err := unmountAll(); err != nil {
				options.Logger.Error(err)
			}
		}()
	}
	if err != nil {
		return 1, err
	}

	// Write the runtime configuration.
	specbytes, err := json.Marshal(spec)
	if err != nil {
		return 1, fmt.Errorf("encoding configuration %#v as json: %w", spec, err)
	}
	if err = ioutils.AtomicWriteFile(filepath.Join(bundlePath, "config.json"), specbytes, 0o600); err != nil {
		return 1, fmt.Errorf("storing runtime configuration: %w", err)
	}

	logrus.Debugf("config = %v", string(specbytes))

	// Decide which runtime to use.
	runtime := options.Runtime
	if runtime == "" {
		runtime = util.Runtime()
	}
	localRuntime := util.FindLocalRuntime(runtime)
	if localRuntime != "" {
		runtime = localRuntime
	}

	// Default to just passing down our stdio.
	getCreateStdio := func() (io.ReadCloser, io.WriteCloser, io.WriteCloser) {
		return os.Stdin, os.Stdout, os.Stderr
	}

	// Figure out how we're doing stdio handling, and create pipes and sockets.
	var stdio sync.WaitGroup
	var consoleListener *net.UnixListener
	var errorFds, closeBeforeReadingErrorFds []int
	stdioPipe := make([][]int, 3)
	copyConsole := false
	copyPipes := false
	finishCopy := make([]int, 2)
	if err = unix.Pipe(finishCopy); err != nil {
		return 1, fmt.Errorf("creating pipe for notifying to stop stdio: %w", err)
	}
	finishedCopy := make(chan struct{}, 1)
	var pargs []string
	if spec.Process != nil {
		pargs = spec.Process.Args
		if spec.Process.Terminal {
			copyConsole = true
			// Create a listening socket for accepting the container's terminal's PTY master.
			socketPath := filepath.Join(bundlePath, "console.sock")
			consoleListener, err = net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
			if err != nil {
				return 1, fmt.Errorf("creating socket %q to receive terminal descriptor: %w", consoleListener.Addr(), err)
			}
			// Add console socket arguments.
			moreCreateArgs = append(moreCreateArgs, "--console-socket", socketPath)
		} else {
			copyPipes = true
			// Figure out who should own the pipes.
			uid, gid, err := util.GetHostRootIDs(spec)
			if err != nil {
				return 1, err
			}
			// Create stdio pipes.
			if stdioPipe, err = runMakeStdioPipe(int(uid), int(gid)); err != nil {
				return 1, err
			}
			if spec.Linux != nil {
				if err = runLabelStdioPipes(stdioPipe, spec.Process.SelinuxLabel, spec.Linux.MountLabel); err != nil {
					return 1, err
				}
			}
			errorFds = []int{stdioPipe[unix.Stdout][0], stdioPipe[unix.Stderr][0]}
			closeBeforeReadingErrorFds = []int{stdioPipe[unix.Stdout][1], stdioPipe[unix.Stderr][1]}
			// Set stdio to our pipes.
			getCreateStdio = func() (io.ReadCloser, io.WriteCloser, io.WriteCloser) {
				stdin := os.NewFile(uintptr(stdioPipe[unix.Stdin][0]), "/dev/stdin")
				stdout := os.NewFile(uintptr(stdioPipe[unix.Stdout][1]), "/dev/stdout")
				stderr := os.NewFile(uintptr(stdioPipe[unix.Stderr][1]), "/dev/stderr")
				return stdin, stdout, stderr
			}
		}
	} else {
		if options.Quiet {
			// Discard stdout.
			getCreateStdio = func() (io.ReadCloser, io.WriteCloser, io.WriteCloser) {
				return os.Stdin, nil, os.Stderr
			}
		}
	}

	runtimeArgs := slices.Clone(options.Args)
	if options.CgroupManager == config.SystemdCgroupsManager {
		runtimeArgs = append(runtimeArgs, "--systemd-cgroup")
	}

	// Build the commands that we'll execute.
	pidFile := filepath.Join(bundlePath, "pid")
	args := append(append(append(runtimeArgs, "create", "--bundle", bundlePath, "--pid-file", pidFile), moreCreateArgs...), containerName)
	create := exec.Command(runtime, args...)
	setPdeathsig(create)
	create.Dir = bundlePath
	stdin, stdout, stderr := getCreateStdio()
	create.Stdin, create.Stdout, create.Stderr = stdin, stdout, stderr

	args = append(options.Args, "start", containerName)
	start := exec.Command(runtime, args...)
	setPdeathsig(start)
	start.Dir = bundlePath
	start.Stderr = os.Stderr

	kill := func(signal string) *exec.Cmd {
		args := append(options.Args, "kill", containerName)
		if signal != "" {
			args = append(args, signal)
		}
		kill := exec.Command(runtime, args...)
		kill.Dir = bundlePath
		kill.Stderr = os.Stderr
		return kill
	}

	args = append(options.Args, "delete", containerName)
	del := exec.Command(runtime, args...)
	del.Dir = bundlePath
	del.Stderr = os.Stderr

	// Actually create the container.
	logrus.Debugf("Running %q", create.Args)
	err = create.Run()
	if err != nil {
		return 1, fmt.Errorf("from %s creating container for %v: %s: %w", runtime, pargs, runCollectOutput(options.Logger, errorFds, closeBeforeReadingErrorFds), err)
	}
	defer func() {
		err2 := del.Run()
		if err2 != nil {
			if err == nil {
				err = fmt.Errorf("deleting container: %w", err2)
			} else {
				options.Logger.Infof("error from %s deleting container: %v", runtime, err2)
			}
		}
	}()

	// Make sure we read the container's exit status when it exits.
	pidValue, err := os.ReadFile(pidFile)
	if err != nil {
		return 1, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidValue)))
	if err != nil {
		return 1, fmt.Errorf("parsing pid %s as a number: %w", string(pidValue), err)
	}
	var stopped uint32
	var reaping sync.WaitGroup
	reaping.Add(1)
	go func() {
		defer reaping.Done()
		var err error
		_, err = unix.Wait4(pid, &wstatus, 0, nil)
		if err != nil {
			wstatus = 0
			options.Logger.Errorf("error waiting for container child process %d: %v\n", pid, err)
		}
		atomic.StoreUint32(&stopped, 1)
	}()

	if configureNetwork {
		if _, err := containerCreateW.Write([]byte{1}); err != nil {
			return 1, err
		}
		containerCreateW.Close()
		logrus.Debug("waiting for parent start message")
		b := make([]byte, 1)
		if _, err := containerStartR.Read(b); err != nil {
			return 1, fmt.Errorf("did not get container start message from parent: %w", err)
		}
		containerStartR.Close()
	}

	if copyPipes {
		// We don't need the ends of the pipes that belong to the container.
		stdin.Close()
		if stdout != nil {
			stdout.Close()
		}
		stderr.Close()
	}

	// Handle stdio for the container in the background.
	stdio.Add(1)
	go runCopyStdio(options.Logger, &stdio, copyPipes, stdioPipe, copyConsole, consoleListener, finishCopy, finishedCopy, spec)

	// Start the container.
	logrus.Debugf("Running %q", start.Args)
	err = start.Run()
	if err != nil {
		return 1, fmt.Errorf("from %s starting container: %w", runtime, err)
	}
	defer func() {
		if atomic.LoadUint32(&stopped) == 0 {
			if err := kill("").Run(); err != nil {
				options.Logger.Infof("error from %s stopping container: %v", runtime, err)
			}
			atomic.StoreUint32(&stopped, 1)
		}
	}()

	// Wait for the container to exit.
	interrupted := make(chan os.Signal, 100)
	go func() {
		for range interrupted {
			if err := kill("SIGKILL").Run(); err != nil {
				logrus.Errorf("%v sending SIGKILL", err)
			}
		}
	}()
	signal.Notify(interrupted, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	for {
		now := time.Now()
		var state specs.State
		args = append(options.Args, "state", containerName)
		stat := exec.Command(runtime, args...)
		stat.Dir = bundlePath
		stat.Stderr = os.Stderr
		stateOutput, err := stat.Output()
		if err != nil {
			if atomic.LoadUint32(&stopped) != 0 {
				// container exited
				break
			}
			return 1, fmt.Errorf("reading container state from %s (got output: %q): %w", runtime, string(stateOutput), err)
		}
		if err = json.Unmarshal(stateOutput, &state); err != nil {
			return 1, fmt.Errorf("parsing container state %q from %s: %w", string(stateOutput), runtime, err)
		}
		switch state.Status {
		case specs.StateCreating, specs.StateCreated, specs.StateRunning:
			// all fine
		case specs.StateStopped:
			atomic.StoreUint32(&stopped, 1)
		default:
			return 1, fmt.Errorf("container status unexpectedly changed to %q", state.Status)
		}
		if atomic.LoadUint32(&stopped) != 0 {
			break
		}
		select {
		case <-finishedCopy:
			atomic.StoreUint32(&stopped, 1)
		case <-time.After(time.Until(now.Add(100 * time.Millisecond))):
			continue
		}
		if atomic.LoadUint32(&stopped) != 0 {
			break
		}
	}
	signal.Stop(interrupted)
	close(interrupted)

	// Close the writing end of the stop-handling-stdio notification pipe.
	unix.Close(finishCopy[1])
	// Wait for the stdio copy goroutine to flush.
	stdio.Wait()
	// Wait until we finish reading the exit status.
	reaping.Wait()

	return wstatus, nil
}

func runCollectOutput(logger *logrus.Logger, fds, closeBeforeReadingFds []int) string {
	for _, fd := range closeBeforeReadingFds {
		unix.Close(fd)
	}
	var b bytes.Buffer
	buf := make([]byte, 8192)
	for _, fd := range fds {
		nread, err := unix.Read(fd, buf)
		if err != nil {
			if errno, isErrno := err.(syscall.Errno); isErrno {
				switch errno {
				default:
					logger.Errorf("error reading from pipe %d: %v", fd, err)
				case syscall.EINTR, syscall.EAGAIN:
				}
			} else {
				logger.Errorf("unable to wait for data from pipe %d: %v", fd, err)
			}
			continue
		}
		for nread > 0 {
			r := buf[:nread]
			if nwritten, err := b.Write(r); err != nil || nwritten != len(r) {
				if nwritten != len(r) {
					logger.Errorf("error buffering data from pipe %d: %v", fd, err)
					break
				}
			}
			nread, err = unix.Read(fd, buf)
			if err != nil {
				if errno, isErrno := err.(syscall.Errno); isErrno {
					switch errno {
					default:
						logger.Errorf("error reading from pipe %d: %v", fd, err)
					case syscall.EINTR, syscall.EAGAIN:
					}
				} else {
					logger.Errorf("unable to wait for data from pipe %d: %v", fd, err)
				}
				break
			}
		}
	}
	return b.String()
}

func setNonblock(logger *logrus.Logger, fd int, description string, nonblocking bool) (bool, error) {
	mask, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return false, err
	}
	blocked := mask&unix.O_NONBLOCK == 0

	if err := unix.SetNonblock(fd, nonblocking); err != nil {
		if nonblocking {
			logger.Errorf("error setting %s to nonblocking: %v", description, err)
		} else {
			logger.Errorf("error setting descriptor %s blocking: %v", description, err)
		}
	}
	return blocked, err
}

func runCopyStdio(logger *logrus.Logger, stdio *sync.WaitGroup, copyPipes bool, stdioPipe [][]int, copyConsole bool, consoleListener *net.UnixListener, finishCopy []int, finishedCopy chan struct{}, spec *specs.Spec) {
	defer func() {
		unix.Close(finishCopy[0])
		if copyPipes {
			unix.Close(stdioPipe[unix.Stdin][1])
			unix.Close(stdioPipe[unix.Stdout][0])
			unix.Close(stdioPipe[unix.Stderr][0])
		}
		stdio.Done()
		finishedCopy <- struct{}{}
		close(finishedCopy)
	}()
	// Map describing where data on an incoming descriptor should go.
	relayMap := make(map[int]int)
	// Map describing incoming and outgoing descriptors.
	readDesc := make(map[int]string)
	writeDesc := make(map[int]string)
	// Buffers.
	relayBuffer := make(map[int]*bytes.Buffer)
	// Set up the terminal descriptor or pipes for polling.
	if copyConsole {
		// Accept a connection over our listening socket.
		fd, err := runAcceptTerminal(logger, consoleListener, spec.Process.ConsoleSize)
		if err != nil {
			logger.Errorf("%v", err)
			return
		}
		terminalFD := fd
		// Input from our stdin, output from the terminal descriptor.
		relayMap[unix.Stdin] = terminalFD
		readDesc[unix.Stdin] = "stdin"
		relayBuffer[terminalFD] = new(bytes.Buffer)
		writeDesc[terminalFD] = "container terminal input"
		relayMap[terminalFD] = unix.Stdout
		readDesc[terminalFD] = "container terminal output"
		relayBuffer[unix.Stdout] = new(bytes.Buffer)
		writeDesc[unix.Stdout] = "output"
		// Set our terminal's mode to raw, to pass handling of special
		// terminal input to the terminal in the container.
		if term.IsTerminal(unix.Stdin) {
			if state, err := term.MakeRaw(unix.Stdin); err != nil {
				logger.Warnf("error setting terminal state: %v", err)
			} else {
				defer func() {
					if err = term.Restore(unix.Stdin, state); err != nil {
						logger.Errorf("unable to restore terminal state: %v", err)
					}
				}()
			}
		}
	}
	if copyPipes {
		// Input from our stdin, output from the stdout and stderr pipes.
		relayMap[unix.Stdin] = stdioPipe[unix.Stdin][1]
		readDesc[unix.Stdin] = "stdin"
		relayBuffer[stdioPipe[unix.Stdin][1]] = new(bytes.Buffer)
		writeDesc[stdioPipe[unix.Stdin][1]] = "container stdin"
		relayMap[stdioPipe[unix.Stdout][0]] = unix.Stdout
		readDesc[stdioPipe[unix.Stdout][0]] = "container stdout"
		relayBuffer[unix.Stdout] = new(bytes.Buffer)
		writeDesc[unix.Stdout] = "stdout"
		relayMap[stdioPipe[unix.Stderr][0]] = unix.Stderr
		readDesc[stdioPipe[unix.Stderr][0]] = "container stderr"
		relayBuffer[unix.Stderr] = new(bytes.Buffer)
		writeDesc[unix.Stderr] = "stderr"
	}
	// Set our reading descriptors to non-blocking.
	for rfd, wfd := range relayMap {
		blocked, err := setNonblock(logger, rfd, readDesc[rfd], true)
		if err != nil {
			return
		}
		if blocked {
			defer setNonblock(logger, rfd, readDesc[rfd], false) //nolint:errcheck
		}
		setNonblock(logger, wfd, writeDesc[wfd], false) //nolint:errcheck
	}

	if copyPipes {
		setNonblock(logger, stdioPipe[unix.Stdin][1], writeDesc[stdioPipe[unix.Stdin][1]], true) //nolint:errcheck
	}

	runCopyStdioPassData(copyPipes, stdioPipe, finishCopy, relayMap, relayBuffer, readDesc, writeDesc)
}

func canRetry(err error) bool {
	if errno, isErrno := err.(syscall.Errno); isErrno {
		return errno == syscall.EINTR || errno == syscall.EAGAIN
	}
	return false
}

func runCopyStdioPassData(copyPipes bool, stdioPipe [][]int, finishCopy []int, relayMap map[int]int, relayBuffer map[int]*bytes.Buffer, readDesc map[int]string, writeDesc map[int]string) {
	closeStdin := false

	// Pass data back and forth.
	pollTimeout := -1
	for len(relayMap) > 0 {
		// Start building the list of descriptors to poll.
		pollFds := make([]unix.PollFd, 0, len(relayMap)+1)
		// Poll for a notification that we should stop handling stdio.
		pollFds = append(pollFds, unix.PollFd{Fd: int32(finishCopy[0]), Events: unix.POLLIN | unix.POLLHUP})
		// Poll on our reading descriptors.
		for rfd := range relayMap {
			pollFds = append(pollFds, unix.PollFd{Fd: int32(rfd), Events: unix.POLLIN | unix.POLLHUP})
		}
		buf := make([]byte, 8192)
		// Wait for new data from any input descriptor, or a notification that we're done.
		_, err := unix.Poll(pollFds, pollTimeout)
		if !util.LogIfNotRetryable(err, fmt.Sprintf("error waiting for stdio/terminal data to relay: %v", err)) {
			return
		}
		removes := make(map[int]struct{})
		for _, pollFd := range pollFds {
			// If this descriptor's just been closed from the other end, mark it for
			// removal from the set that we're checking for.
			if pollFd.Revents&unix.POLLHUP == unix.POLLHUP {
				removes[int(pollFd.Fd)] = struct{}{}
			}
			// If the descriptor was closed elsewhere, remove it from our list.
			if pollFd.Revents&unix.POLLNVAL != 0 {
				logrus.Debugf("error polling descriptor %s: closed?", readDesc[int(pollFd.Fd)])
				removes[int(pollFd.Fd)] = struct{}{}
			}
			// If the POLLIN flag isn't set, then there's no data to be read from this descriptor.
			if pollFd.Revents&unix.POLLIN == 0 {
				continue
			}
			// Read whatever there is to be read.
			readFD := int(pollFd.Fd)
			writeFD, needToRelay := relayMap[readFD]
			if needToRelay {
				n, err := unix.Read(readFD, buf)
				if !util.LogIfNotRetryable(err, fmt.Sprintf("unable to read %s data: %v", readDesc[readFD], err)) {
					return
				}
				// If it's zero-length on our stdin and we're
				// using pipes, it's an EOF, so close the stdin
				// pipe's writing end.
				if n == 0 && !canRetry(err) && int(pollFd.Fd) == unix.Stdin {
					removes[int(pollFd.Fd)] = struct{}{}
				} else if n > 0 {
					// Buffer the data in case we get blocked on where they need to go.
					nwritten, err := relayBuffer[writeFD].Write(buf[:n])
					if err != nil {
						logrus.Debugf("buffer: %v", err)
						continue
					}
					if nwritten != n {
						logrus.Debugf("buffer: expected to buffer %d bytes, wrote %d", n, nwritten)
						continue
					}
					// If this is the last of the data we'll be able to read from this
					// descriptor, read all that there is to read.
					for pollFd.Revents&unix.POLLHUP == unix.POLLHUP {
						nr, err := unix.Read(readFD, buf)
						util.LogIfUnexpectedWhileDraining(err, fmt.Sprintf("read %s: %v", readDesc[readFD], err))
						if nr <= 0 {
							break
						}
						nwritten, err := relayBuffer[writeFD].Write(buf[:nr])
						if err != nil {
							logrus.Debugf("buffer: %v", err)
							break
						}
						if nwritten != nr {
							logrus.Debugf("buffer: expected to buffer %d bytes, wrote %d", nr, nwritten)
							break
						}
					}
				}
			}
		}
		// Try to drain the output buffers.  Set the default timeout
		// for the next poll() to 100ms if we still have data to write.
		pollTimeout = -1
		for writeFD := range relayBuffer {
			if relayBuffer[writeFD].Len() > 0 {
				n, err := unix.Write(writeFD, relayBuffer[writeFD].Bytes())
				if !util.LogIfNotRetryable(err, fmt.Sprintf("unable to write %s data: %v", writeDesc[writeFD], err)) {
					return
				}
				if n > 0 {
					relayBuffer[writeFD].Next(n)
				}
				if closeStdin && writeFD == stdioPipe[unix.Stdin][1] && stdioPipe[unix.Stdin][1] >= 0 && relayBuffer[stdioPipe[unix.Stdin][1]].Len() == 0 {
					logrus.Debugf("closing stdin")
					unix.Close(stdioPipe[unix.Stdin][1])
					stdioPipe[unix.Stdin][1] = -1
				}
			}
			if relayBuffer[writeFD].Len() > 0 {
				pollTimeout = 100
			}
		}
		// Remove any descriptors which we don't need to poll any more from the poll descriptor list.
		for remove := range removes {
			if copyPipes && remove == unix.Stdin {
				closeStdin = true
				if relayBuffer[stdioPipe[unix.Stdin][1]].Len() == 0 {
					logrus.Debugf("closing stdin")
					unix.Close(stdioPipe[unix.Stdin][1])
					stdioPipe[unix.Stdin][1] = -1
				}
			}
			delete(relayMap, remove)
		}
		// If the we-can-return pipe had anything for us, we're done.
		for _, pollFd := range pollFds {
			if int(pollFd.Fd) == finishCopy[0] && pollFd.Revents != 0 {
				// The pipe is closed, indicating that we can stop now.
				return
			}
		}
	}
}

func runAcceptTerminal(logger *logrus.Logger, consoleListener *net.UnixListener, terminalSize *specs.Box) (int, error) {
	defer consoleListener.Close()
	c, err := consoleListener.AcceptUnix()
	if err != nil {
		return -1, fmt.Errorf("accepting socket descriptor connection: %w", err)
	}
	defer c.Close()
	// Expect a control message over our new connection.
	b := make([]byte, 8192)
	oob := make([]byte, 8192)
	n, oobn, _, _, err := c.ReadMsgUnix(b, oob)
	if err != nil {
		return -1, fmt.Errorf("reading socket descriptor: %w", err)
	}
	if n > 0 {
		logrus.Debugf("socket descriptor is for %q", string(b[:n]))
	}
	if oobn > len(oob) {
		return -1, fmt.Errorf("too much out-of-bounds data (%d bytes)", oobn)
	}
	// Parse the control message.
	scm, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return -1, fmt.Errorf("parsing out-of-bound data as a socket control message: %w", err)
	}
	logrus.Debugf("control messages: %v", scm)
	// Expect to get a descriptor.
	terminalFD := -1
	for i := range scm {
		fds, err := unix.ParseUnixRights(&scm[i])
		if err != nil {
			return -1, fmt.Errorf("parsing unix rights control message: %v: %w", &scm[i], err)
		}
		logrus.Debugf("fds: %v", fds)
		if len(fds) == 0 {
			continue
		}
		terminalFD = fds[0]
		break
	}
	if terminalFD == -1 {
		return -1, fmt.Errorf("unable to read terminal descriptor")
	}
	// Set the pseudoterminal's size to the configured size, or our own.
	winsize := &unix.Winsize{}
	if terminalSize != nil {
		// Use configured sizes.
		winsize.Row = uint16(terminalSize.Height)
		winsize.Col = uint16(terminalSize.Width)
	} else {
		if term.IsTerminal(unix.Stdin) {
			// Use the size of our terminal.
			if winsize, err = unix.IoctlGetWinsize(unix.Stdin, unix.TIOCGWINSZ); err != nil {
				logger.Warnf("error reading size of controlling terminal: %v", err)
				winsize.Row = 0
				winsize.Col = 0
			}
		}
	}
	if winsize.Row != 0 && winsize.Col != 0 {
		if err = unix.IoctlSetWinsize(terminalFD, unix.TIOCSWINSZ, winsize); err != nil {
			logger.Warnf("error setting size of container pseudoterminal: %v", err)
		}
		// FIXME - if we're connected to a terminal, we should
		// be passing the updated terminal size down when we
		// receive a SIGWINCH.
	}
	return terminalFD, nil
}

func reapStrays() {
	// Reap the exit status of anything that was reparented to us, not that
	// we care about their exit status.
	logrus.Debugf("checking for reparented child processes")
	for range 100 {
		wpid, err := unix.Wait4(-1, nil, unix.WNOHANG, nil)
		if err != nil {
			break
		}
		if wpid == 0 {
			time.Sleep(100 * time.Millisecond)
		} else {
			logrus.Debugf("caught reparented child process %d", wpid)
		}
	}
}

func runUsingRuntimeMain() {
	var options runUsingRuntimeSubprocOptions
	// Set logging.
	if level := os.Getenv("LOGLEVEL"); level != "" {
		if ll, err := strconv.Atoi(level); err == nil {
			logrus.SetLevel(logrus.Level(ll))
		}
	}
	// Unpack our configuration.
	confPipe := os.NewFile(3, "confpipe")
	if confPipe == nil {
		fmt.Fprintf(os.Stderr, "error reading options pipe\n")
		os.Exit(1)
	}
	defer confPipe.Close()
	if err := json.NewDecoder(confPipe).Decode(&options); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding options: %v\n", err)
		os.Exit(1)
	}
	// Set ourselves up to read the container's exit status.  We're doing this in a child process
	// so that we won't mess with the setting in a caller of the library.
	if err := setChildProcess(); err != nil {
		os.Exit(1)
	}
	ospec := options.Spec
	if ospec == nil {
		fmt.Fprintf(os.Stderr, "options spec not specified\n")
		os.Exit(1)
	}

	// open the pipes used to communicate with the parent process
	var containerCreateW *os.File
	var containerStartR *os.File
	if options.ConfigureNetwork {
		containerCreateW = os.NewFile(4, "containercreatepipe")
		if containerCreateW == nil {
			fmt.Fprintf(os.Stderr, "could not open fd 4\n")
			os.Exit(1)
		}
		containerStartR = os.NewFile(5, "containerstartpipe")
		if containerStartR == nil {
			fmt.Fprintf(os.Stderr, "could not open fd 5\n")
			os.Exit(1)
		}
	}

	// Run the container, start to finish.
	status, err := runUsingRuntime(options.Options, options.ConfigureNetwork, options.MoreCreateArgs, ospec, options.BundlePath, options.ContainerName, containerCreateW, containerStartR)
	reapStrays()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running container: %v\n", err)
		os.Exit(1)
	}
	// Pass the container's exit status back to the caller by exiting with the same status.
	if status.Exited() {
		os.Exit(status.ExitStatus())
	} else if status.Signaled() {
		fmt.Fprintf(os.Stderr, "container exited on %s\n", status.Signal())
		os.Exit(1)
	}
	os.Exit(1)
}

func (b *Builder) runUsingRuntimeSubproc(isolation define.Isolation, options RunOptions, configureNetwork bool, networkString string,
	moreCreateArgs []string, spec *specs.Spec, rootPath, bundlePath, containerName, buildContainerName, hostsFile, resolvFile string,
) (err error) {
	// Lock the caller to a single OS-level thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var confwg sync.WaitGroup
	config, conferr := json.Marshal(runUsingRuntimeSubprocOptions{
		Options:          options,
		Spec:             spec,
		RootPath:         rootPath,
		BundlePath:       bundlePath,
		ConfigureNetwork: configureNetwork,
		MoreCreateArgs:   moreCreateArgs,
		ContainerName:    containerName,
		Isolation:        isolation,
	})
	if conferr != nil {
		return fmt.Errorf("encoding configuration for %q: %w", runUsingRuntimeCommand, conferr)
	}
	cmd := reexec.Command(runUsingRuntimeCommand)
	setPdeathsig(cmd)
	cmd.Dir = bundlePath
	cmd.Stdin = options.Stdin
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = options.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = options.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	cmd.Env = util.MergeEnv(os.Environ(), []string{fmt.Sprintf("LOGLEVEL=%d", logrus.GetLevel())})
	preader, pwriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("creating configuration pipe: %w", err)
	}
	confwg.Add(1)
	go func() {
		_, conferr = io.Copy(pwriter, bytes.NewReader(config))
		if conferr != nil {
			conferr = fmt.Errorf("while copying configuration down pipe to child process: %w", conferr)
		}
		confwg.Done()
	}()

	// create network configuration pipes
	var containerCreateR, containerCreateW fileCloser
	var containerStartR, containerStartW fileCloser
	if configureNetwork {
		containerCreateR.file, containerCreateW.file, err = os.Pipe()
		if err != nil {
			return fmt.Errorf("creating container create pipe: %w", err)
		}
		defer containerCreateR.Close()
		defer containerCreateW.Close()

		containerStartR.file, containerStartW.file, err = os.Pipe()
		if err != nil {
			return fmt.Errorf("creating container start pipe: %w", err)
		}
		defer containerStartR.Close()
		defer containerStartW.Close()
		cmd.ExtraFiles = []*os.File{containerCreateW.file, containerStartR.file}
	}

	cmd.ExtraFiles = append([]*os.File{preader}, cmd.ExtraFiles...)
	defer preader.Close()
	defer pwriter.Close()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("while starting runtime: %w", err)
	}

	interrupted := make(chan os.Signal, 100)
	go func() {
		for receivedSignal := range interrupted {
			if err := cmd.Process.Signal(receivedSignal); err != nil {
				logrus.Infof("%v while attempting to forward %v to child process", err, receivedSignal)
			}
		}
	}()
	signal.Notify(interrupted, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	if configureNetwork {
		// we already passed the fd to the child, now close the writer so we do not hang if the child closes it
		containerCreateW.Close()
		if err := waitForSync(containerCreateR.file); err != nil {
			// we do not want to return here since we want to capture the exit code from the child via cmd.Wait()
			// close the pipes here so that the child will not hang forever
			containerCreateR.Close()
			containerStartW.Close()
			logrus.Errorf("did not get container create message from subprocess: %v", err)
		} else {
			pidFile := filepath.Join(bundlePath, "pid")
			pidValue, err := os.ReadFile(pidFile)
			if err != nil {
				return err
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(pidValue)))
			if err != nil {
				return fmt.Errorf("parsing pid %s as a number: %w", string(pidValue), err)
			}

			teardown, netResult, err := b.runConfigureNetwork(pid, isolation, options, networkString, containerName, []string{spec.Hostname, buildContainerName})
			if teardown != nil {
				defer teardown()
			}
			if err != nil {
				return fmt.Errorf("setup network: %w", err)
			}

			// only add hosts if we manage the hosts file
			if hostsFile != "" {
				err = b.addHostsEntries(hostsFile, rootPath, netResult.entries, netResult.excludeIPs, netResult.preferredHostContainersInternalIP)
				if err != nil {
					return err
				}
			}

			if resolvFile != "" {
				err = b.addResolvConfEntries(resolvFile, netResult.dnsServers, spec, netResult.keepHostResolvers, netResult.ipv6)
				if err != nil {
					return err
				}
			}

			logrus.Debug("network namespace successfully setup, send start message to child")
			_, err = containerStartW.file.Write([]byte{1})
			if err != nil {
				return err
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("while running runtime: %w", err)
	}
	confwg.Wait()
	signal.Stop(interrupted)
	close(interrupted)
	if err == nil {
		return conferr
	}
	if conferr != nil {
		logrus.Debugf("%v", conferr)
	}
	return err
}

type runUsingRuntimeSubprocOptions struct {
	Options          RunOptions
	Spec             *specs.Spec
	RootPath         string
	BundlePath       string
	ConfigureNetwork bool
	MoreCreateArgs   []string
	ContainerName    string
	Isolation        define.Isolation
}

func init() {
	reexec.Register(runUsingRuntimeCommand, runUsingRuntimeMain)
}

// If this succeeds, after the command which uses the spec finishes running,
// the caller must call b.cleanupRunMounts() on the returned runMountArtifacts
// structure.
func (b *Builder) setupMounts(mountPoint string, spec *specs.Spec, bundlePath string, optionMounts []specs.Mount, bindFiles map[string]string, builtinVolumes []string, compatBuiltinVolumes types.OptionalBool, volumeMounts []string, runFileMounts []string, runMountInfo runMountInfo) (*runMountArtifacts, error) {
	// Start building a new list of mounts.
	var mounts []specs.Mount
	haveMount := func(destination string) bool {
		for _, mount := range mounts {
			if mount.Destination == destination {
				// Already have something to mount there.
				return true
			}
		}
		return false
	}

	specMounts, err := setupSpecialMountSpecChanges(spec, b.CommonBuildOpts.ShmSize)
	if err != nil {
		return nil, err
	}

	// Get the list of files we need to bind into the container.
	bindFileMounts := runSetupBoundFiles(bundlePath, bindFiles)

	// After this point we need to know the per-container persistent storage directory.
	cdir, err := b.store.ContainerDirectory(b.ContainerID)
	if err != nil {
		return nil, fmt.Errorf("determining work directory for container %q: %w", b.ContainerID, err)
	}

	// Figure out which UID and GID to tell the subscriptions package to use
	// for files that it creates.
	rootUID, rootGID, err := util.GetHostRootIDs(spec)
	if err != nil {
		return nil, err
	}

	// Get host UID and GID of the container process.
	uidMap := []specs.LinuxIDMapping{}
	gidMap := []specs.LinuxIDMapping{}
	if spec.Linux != nil {
		uidMap = spec.Linux.UIDMappings
		gidMap = spec.Linux.GIDMappings
	}
	processUID, processGID, err := util.GetHostIDs(uidMap, gidMap, spec.Process.User.UID, spec.Process.User.GID)
	if err != nil {
		return nil, err
	}

	// Get the list of subscriptions mounts.
	subscriptionMounts := subscriptions.MountsWithUIDGID(b.MountLabel, cdir, b.DefaultMountsFilePath, mountPoint, int(rootUID), int(rootGID), unshare.IsRootless(), false)

	idMaps := IDMaps{
		uidmap:     uidMap,
		gidmap:     gidMap,
		rootUID:    int(rootUID),
		rootGID:    int(rootGID),
		processUID: int(processUID),
		processGID: int(processGID),
	}
	// Get the list of mounts that are just for this Run() call.
	runMounts, mountArtifacts, err := b.runSetupRunMounts(bundlePath, runFileMounts, runMountInfo, idMaps)
	if err != nil {
		return nil, err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			if err := b.cleanupRunMounts(mountArtifacts); err != nil {
				b.Logger.Debugf("cleaning up run mounts: %v", err)
			}
		}
	}()
	// Add temporary copies of the contents of volume locations at the
	// volume locations, unless we already have something there.
	builtins, err := runSetupBuiltinVolumes(b.MountLabel, mountPoint, cdir, builtinVolumes, compatBuiltinVolumes, int(rootUID), int(rootGID))
	if err != nil {
		return nil, err
	}

	// Get the list of explicitly-specified volume mounts.
	mountLabel := ""
	if spec.Linux != nil {
		mountLabel = spec.Linux.MountLabel
	}
	volumes, overlayDirs, err := b.runSetupVolumeMounts(mountLabel, volumeMounts, optionMounts, idMaps)
	if err != nil {
		return nil, err
	}
	mountArtifacts.RunOverlayDirs = append(mountArtifacts.RunOverlayDirs, overlayDirs...)

	allMounts := util.SortMounts(append(append(append(append(append(volumes, builtins...), runMounts...), subscriptionMounts...), bindFileMounts...), specMounts...))

	// Add them all, in the preferred order, except where they conflict with something that was previously added.
	for _, mount := range allMounts {
		if haveMount(mount.Destination) {
			// Already mounting something there, no need to bother with this one.
			continue
		}
		// Add the mount.
		mounts = append(mounts, mount)
	}

	// Set the list in the spec.
	spec.Mounts = mounts
	succeeded = true
	return mountArtifacts, nil
}

func runSetupBuiltinVolumes(mountLabel, mountPoint, containerDir string, builtinVolumes []string, compatBuiltinVolumes types.OptionalBool, rootUID, rootGID int) ([]specs.Mount, error) {
	var mounts []specs.Mount
	hostOwner := idtools.IDPair{UID: rootUID, GID: rootGID}
	// Add temporary copies of the contents of volume locations at the
	// volume locations, unless we already have something there.
	for _, volume := range builtinVolumes {
		// Make sure the volume exists in the rootfs.
		createDirPerms := os.FileMode(0o755)
		err := copier.Mkdir(mountPoint, filepath.Join(mountPoint, volume), copier.MkdirOptions{
			ChownNew: &hostOwner,
			ChmodNew: &createDirPerms,
		})
		if err != nil {
			return nil, fmt.Errorf("ensuring volume path %q: %w", filepath.Join(mountPoint, volume), err)
		}
		// If we're not being asked to bind mount anonymous volumes
		// onto the volume paths, we're done here.
		if compatBuiltinVolumes != types.OptionalBoolTrue {
			continue
		}
		// If we need to, create the directory that we'll use to hold
		// the volume contents.  If we do need to create it, then we'll
		// need to populate it, too, so make a note of that.
		volumePath := filepath.Join(containerDir, "buildah-volumes", digest.Canonical.FromString(volume).Hex())
		initializeVolume := false
		if err := fileutils.Exists(volumePath); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
			logrus.Debugf("setting up built-in volume path at %q for %q", volumePath, volume)
			if err = os.MkdirAll(volumePath, 0o755); err != nil {
				return nil, err
			}
			if err = relabel(volumePath, mountLabel, false); err != nil {
				return nil, err
			}
			initializeVolume = true
		}
		// Read the attributes of the volume's location in the rootfs.
		srcPath, err := copier.Eval(mountPoint, filepath.Join(mountPoint, volume), copier.EvalOptions{})
		if err != nil {
			return nil, fmt.Errorf("evaluating path %q: %w", srcPath, err)
		}
		stat, err := os.Stat(srcPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		// If we need to populate the mounted volume's contents with
		// content from the rootfs, set it up now.
		if initializeVolume {
			if err = os.Chmod(volumePath, stat.Mode().Perm()); err != nil {
				return nil, err
			}
			if err = os.Chown(volumePath, int(stat.Sys().(*syscall.Stat_t).Uid), int(stat.Sys().(*syscall.Stat_t).Gid)); err != nil {
				return nil, err
			}
			logrus.Debugf("populating directory %q for volume %q using contents of %q", volumePath, volume, srcPath)
			if err = extractWithTar(mountPoint, srcPath, volumePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("populating directory %q for volume %q using contents of %q: %w", volumePath, volume, srcPath, err)
			}
		}
		// Add the bind mount.
		mounts = append(mounts, specs.Mount{
			Source:      volumePath,
			Destination: volume,
			Type:        define.TypeBind,
			Options:     define.BindOptions,
		})
	}
	return mounts, nil
}

// runSetupRunMounts sets up mounts that exist only in this RUN, not in subsequent runs
//
// If this function succeeds, the caller must free the returned
// runMountArtifacts by calling b.cleanupRunMounts() after the command being
// executed with those mounts has finished.
func (b *Builder) runSetupRunMounts(bundlePath string, mounts []string, sources runMountInfo, idMaps IDMaps) ([]specs.Mount, *runMountArtifacts, error) {
	tmpFiles := make([]string, 0, len(mounts))
	mountImages := make([]string, 0, len(mounts))
	intermediateMounts := make([]string, 0, len(mounts))
	finalMounts := make([]specs.Mount, 0, len(mounts))
	agents := make([]*sshagent.AgentServer, 0, len(mounts))
	defaultSSHSock := ""
	targetLocks := []*lockfile.LockFile{}
	var overlayDirs []string
	succeeded := false
	defer func() {
		if !succeeded {
			for _, agent := range agents {
				servePath := agent.ServePath()
				if err := agent.Shutdown(); err != nil {
					b.Logger.Errorf("shutting down SSH agent at %q: %v", servePath, err)
				}
			}
			for _, overlayDir := range overlayDirs {
				if err := overlay.RemoveTemp(overlayDir); err != nil {
					b.Logger.Error(err.Error())
				}
			}
			for _, intermediateMount := range intermediateMounts {
				if err := mount.Unmount(intermediateMount); err != nil {
					b.Logger.Errorf("unmounting %q: %v", intermediateMount, err)
				}
				if err := os.Remove(intermediateMount); err != nil {
					b.Logger.Errorf("removing should-be-empty directory %q: %v", intermediateMount, err)
				}
			}
			for _, mountImage := range mountImages {
				if _, err := b.store.UnmountImage(mountImage, false); err != nil {
					b.Logger.Error(err.Error())
				}
			}
			for _, tmpFile := range tmpFiles {
				if err := os.Remove(tmpFile); err != nil && !errors.Is(err, os.ErrNotExist) {
					b.Logger.Error(err.Error())
				}
			}
			volumes.UnlockLockArray(targetLocks)
		}
	}()
	for _, mount := range mounts {
		var mountSpec *specs.Mount
		var err error
		var envFile, image, bundleMountsDir, overlayDir, intermediateMount string
		var agent *sshagent.AgentServer
		var tl *lockfile.LockFile

		tokens := strings.Split(mount, ",")

		// If `type` is not set default to TypeBind
		mountType := define.TypeBind

		for _, field := range tokens {
			if strings.HasPrefix(field, "type=") {
				kv := strings.Split(field, "=")
				if len(kv) != 2 {
					return nil, nil, errors.New("invalid mount type")
				}
				mountType = kv[1]
			}
		}
		switch mountType {
		case "secret":
			mountSpec, envFile, err = b.getSecretMount(tokens, sources.Secrets, idMaps, sources.WorkDir)
			if err != nil {
				return nil, nil, err
			}
			if mountSpec != nil {
				finalMounts = append(finalMounts, *mountSpec)
				if envFile != "" {
					tmpFiles = append(tmpFiles, envFile)
				}
			}
		case "ssh":
			mountSpec, agent, err = b.getSSHMount(tokens, len(agents), sources.SSHSources, idMaps)
			if err != nil {
				return nil, nil, err
			}
			if mountSpec != nil {
				finalMounts = append(finalMounts, *mountSpec)
				if len(agents) == 0 {
					defaultSSHSock = mountSpec.Destination
				}
				agents = append(agents, agent)
			}
		case define.TypeBind:
			if bundleMountsDir == "" {
				if bundleMountsDir, err = os.MkdirTemp(bundlePath, "mounts"); err != nil {
					return nil, nil, err
				}
			}
			mountSpec, image, intermediateMount, overlayDir, err = b.getBindMount(tokens, sources.SystemContext, sources.ContextDir, sources.StageMountPoints, idMaps, sources.WorkDir, bundleMountsDir)
			if err != nil {
				return nil, nil, err
			}
			if image != "" {
				mountImages = append(mountImages, image)
			}
			if intermediateMount != "" {
				intermediateMounts = append(intermediateMounts, intermediateMount)
			}
			if overlayDir != "" {
				overlayDirs = append(overlayDirs, overlayDir)
			}
			finalMounts = append(finalMounts, *mountSpec)
		case "tmpfs":
			mountSpec, err = b.getTmpfsMount(tokens, idMaps, sources.WorkDir)
			if err != nil {
				return nil, nil, err
			}
			finalMounts = append(finalMounts, *mountSpec)
		case "cache":
			if bundleMountsDir == "" {
				if bundleMountsDir, err = os.MkdirTemp(bundlePath, "mounts"); err != nil {
					return nil, nil, err
				}
			}
			mountSpec, image, intermediateMount, overlayDir, tl, err = b.getCacheMount(tokens, sources.SystemContext, sources.StageMountPoints, idMaps, sources.WorkDir, bundleMountsDir)
			if err != nil {
				return nil, nil, err
			}
			if image != "" {
				mountImages = append(mountImages, image)
			}
			if intermediateMount != "" {
				intermediateMounts = append(intermediateMounts, intermediateMount)
			}
			if overlayDir != "" {
				overlayDirs = append(overlayDirs, overlayDir)
			}
			if tl != nil {
				targetLocks = append(targetLocks, tl)
			}
			finalMounts = append(finalMounts, *mountSpec)
		default:
			return nil, nil, fmt.Errorf("invalid mount type %q", mountType)
		}
	}
	succeeded = true
	artifacts := &runMountArtifacts{
		RunOverlayDirs:     overlayDirs,
		Agents:             agents,
		MountedImages:      mountImages,
		SSHAuthSock:        defaultSSHSock,
		TargetLocks:        targetLocks,
		IntermediateMounts: intermediateMounts,
	}
	return finalMounts, artifacts, nil
}

func (b *Builder) getBindMount(tokens []string, sys *types.SystemContext, contextDir string, stageMountPoints map[string]internal.StageMountDetails, idMaps IDMaps, workDir, tmpDir string) (*specs.Mount, string, string, string, error) {
	if contextDir == "" {
		return nil, "", "", "", errors.New("context directory for current run invocation is not configured")
	}
	var optionMounts []specs.Mount
	optionMount, image, intermediateMount, overlayMount, err := volumes.GetBindMount(sys, tokens, contextDir, b.store, b.MountLabel, stageMountPoints, workDir, tmpDir)
	if err != nil {
		return nil, "", "", "", err
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
			if image != "" {
				if _, err := b.store.UnmountImage(image, false); err != nil {
					b.Logger.Debugf("unmounting image %q: %v", image, err)
				}
			}
		}
	}()
	optionMounts = append(optionMounts, optionMount)
	volumes, overlayDirs, err := b.runSetupVolumeMounts(b.MountLabel, nil, optionMounts, idMaps)
	if err != nil {
		return nil, "", "", "", err
	}
	if len(overlayDirs) != 0 {
		return nil, "", "", "", errors.New("internal error: did not expect a resolved bind mount to use the O flag")
	}
	succeeded = true
	return &volumes[0], image, intermediateMount, overlayMount, nil
}

func (b *Builder) getTmpfsMount(tokens []string, idMaps IDMaps, workDir string) (*specs.Mount, error) {
	var optionMounts []specs.Mount
	mount, err := volumes.GetTmpfsMount(tokens, workDir)
	if err != nil {
		return nil, err
	}
	optionMounts = append(optionMounts, mount)
	volumes, overlayDirs, err := b.runSetupVolumeMounts(b.MountLabel, nil, optionMounts, idMaps)
	if err != nil {
		return nil, err
	}
	if len(overlayDirs) != 0 {
		return nil, errors.New("internal error: did not expect a resolved tmpfs mount to use the O flag")
	}
	return &volumes[0], nil
}

func (b *Builder) getSecretMount(tokens []string, secrets map[string]define.Secret, idMaps IDMaps, workdir string) (_ *specs.Mount, _ string, retErr error) {
	errInvalidSyntax := errors.New("secret should have syntax id=id[,target=path,required=bool,mode=uint,uid=uint,gid=uint")
	if len(tokens) == 0 {
		return nil, "", errInvalidSyntax
	}
	var err error
	var id, target string
	var required bool
	var uid, gid uint32
	var mode uint32 = 0o400
	for _, val := range tokens {
		kv := strings.SplitN(val, "=", 2)
		switch kv[0] {
		case "type":
			// This is already processed
			continue
		case "id":
			id = kv[1]
		case "target", "dst", "destination":
			target = kv[1]
			if !filepath.IsAbs(target) {
				target = filepath.Join(workdir, target)
			}
		case "required":
			required = true
			if len(kv) > 1 {
				required, err = strconv.ParseBool(kv[1])
				if err != nil {
					return nil, "", errInvalidSyntax
				}
			}
		case "mode":
			mode64, err := strconv.ParseUint(kv[1], 8, 32)
			if err != nil {
				return nil, "", errInvalidSyntax
			}
			mode = uint32(mode64)
		case "uid":
			uid64, err := strconv.ParseUint(kv[1], 10, 32)
			if err != nil {
				return nil, "", errInvalidSyntax
			}
			uid = uint32(uid64)
		case "gid":
			gid64, err := strconv.ParseUint(kv[1], 10, 32)
			if err != nil {
				return nil, "", errInvalidSyntax
			}
			gid = uint32(gid64)
		default:
			return nil, "", errInvalidSyntax
		}
	}

	if id == "" {
		return nil, "", errInvalidSyntax
	}
	// Default location for secrets is /run/secrets/id
	if target == "" {
		target = "/run/secrets/" + id
	}

	secr, ok := secrets[id]
	if !ok {
		if required {
			return nil, "", fmt.Errorf("secret required but no secret with id %q found", id)
		}
		return nil, "", nil
	}
	var data []byte
	var envFile string
	var ctrFileOnHost string

	switch secr.SourceType {
	case "env":
		data = []byte(os.Getenv(secr.Source))
		tmpFile, err := os.CreateTemp(tmpdir.GetTempDir(), "buildah*")
		if err != nil {
			return nil, "", err
		}
		defer func() {
			if retErr != nil {
				os.Remove(tmpFile.Name())
			}
		}()
		envFile = tmpFile.Name()
		ctrFileOnHost = tmpFile.Name()
	case "file":
		containerWorkingDir, err := b.store.ContainerDirectory(b.ContainerID)
		if err != nil {
			return nil, "", err
		}
		data, err = os.ReadFile(secr.Source)
		if err != nil {
			return nil, "", err
		}
		ctrFileOnHost = filepath.Join(containerWorkingDir, "secrets", digest.FromString(id).Encoded()[:16])
	default:
		return nil, "", errors.New("invalid source secret type")
	}

	// Copy secrets to container working dir (or tmp dir if it's an env), since we need to chmod,
	// chown and relabel it for the container user and we don't want to mess with the original file
	if err := os.MkdirAll(filepath.Dir(ctrFileOnHost), 0o755); err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(ctrFileOnHost, data, 0o644); err != nil {
		return nil, "", err
	}

	if err := relabel(ctrFileOnHost, b.MountLabel, false); err != nil {
		return nil, "", err
	}
	hostUID, hostGID, err := util.GetHostIDs(idMaps.uidmap, idMaps.gidmap, uid, gid)
	if err != nil {
		return nil, "", err
	}
	if err := os.Lchown(ctrFileOnHost, int(hostUID), int(hostGID)); err != nil {
		return nil, "", err
	}
	if err := os.Chmod(ctrFileOnHost, os.FileMode(mode)); err != nil {
		return nil, "", err
	}
	newMount := specs.Mount{
		Destination: target,
		Type:        define.TypeBind,
		Source:      ctrFileOnHost,
		Options:     append(define.BindOptions, "rprivate", "ro"),
	}
	return &newMount, envFile, nil
}

// getSSHMount parses the --mount type=ssh flag in the Containerfile, checks if there's an ssh source provided, and creates and starts an ssh-agent to be forwarded into the container
func (b *Builder) getSSHMount(tokens []string, count int, sshsources map[string]*sshagent.Source, idMaps IDMaps) (*specs.Mount, *sshagent.AgentServer, error) {
	errInvalidSyntax := errors.New("ssh should have syntax id=id[,target=path,required=bool,mode=uint,uid=uint,gid=uint")

	var err error
	var id, target string
	var required bool
	var uid, gid uint32
	var mode uint32 = 0o600
	for _, val := range tokens {
		kv := strings.SplitN(val, "=", 2)
		if len(kv) < 2 {
			return nil, nil, errInvalidSyntax
		}
		switch kv[0] {
		case "type":
			// This is already processed
			continue
		case "id":
			id = kv[1]
		case "target", "dst", "destination":
			target = kv[1]
		case "required":
			required, err = strconv.ParseBool(kv[1])
			if err != nil {
				return nil, nil, errInvalidSyntax
			}
		case "mode":
			mode64, err := strconv.ParseUint(kv[1], 8, 32)
			if err != nil {
				return nil, nil, errInvalidSyntax
			}
			mode = uint32(mode64)
		case "uid":
			uid64, err := strconv.ParseUint(kv[1], 10, 32)
			if err != nil {
				return nil, nil, errInvalidSyntax
			}
			uid = uint32(uid64)
		case "gid":
			gid64, err := strconv.ParseUint(kv[1], 10, 32)
			if err != nil {
				return nil, nil, errInvalidSyntax
			}
			gid = uint32(gid64)
		default:
			return nil, nil, errInvalidSyntax
		}
	}

	if id == "" {
		id = "default"
	}
	// Default location for secrets is /run/buildkit/ssh_agent.{i}
	if target == "" {
		target = fmt.Sprintf("/run/buildkit/ssh_agent.%d", count)
	}

	sshsource, ok := sshsources[id]
	if !ok {
		if required {
			return nil, nil, fmt.Errorf("ssh required but no ssh with id %s found", id)
		}
		return nil, nil, nil
	}
	// Create new agent from keys or socket
	fwdAgent, err := sshagent.NewAgentServer(sshsource)
	if err != nil {
		return nil, nil, err
	}
	// Start ssh server, and get the host sock we're mounting in the container
	hostSock, err := fwdAgent.Serve(b.ProcessLabel)
	if err != nil {
		return nil, nil, err
	}

	if err := relabel(filepath.Dir(hostSock), b.MountLabel, false); err != nil {
		if shutdownErr := fwdAgent.Shutdown(); shutdownErr != nil {
			b.Logger.Errorf("error shutting down agent: %v", shutdownErr)
		}
		return nil, nil, err
	}
	if err := relabel(hostSock, b.MountLabel, false); err != nil {
		if shutdownErr := fwdAgent.Shutdown(); shutdownErr != nil {
			b.Logger.Errorf("error shutting down agent: %v", shutdownErr)
		}
		return nil, nil, err
	}
	hostUID, hostGID, err := util.GetHostIDs(idMaps.uidmap, idMaps.gidmap, uid, gid)
	if err != nil {
		if shutdownErr := fwdAgent.Shutdown(); shutdownErr != nil {
			b.Logger.Errorf("error shutting down agent: %v", shutdownErr)
		}
		return nil, nil, err
	}
	if err := os.Lchown(hostSock, int(hostUID), int(hostGID)); err != nil {
		if shutdownErr := fwdAgent.Shutdown(); shutdownErr != nil {
			b.Logger.Errorf("error shutting down agent: %v", shutdownErr)
		}
		return nil, nil, err
	}
	if err := os.Chmod(hostSock, os.FileMode(mode)); err != nil {
		if shutdownErr := fwdAgent.Shutdown(); shutdownErr != nil {
			b.Logger.Errorf("error shutting down agent: %v", shutdownErr)
		}
		return nil, nil, err
	}
	newMount := specs.Mount{
		Destination: target,
		Type:        define.TypeBind,
		Source:      hostSock,
		Options:     append(define.BindOptions, "rprivate", "ro"),
	}
	return &newMount, fwdAgent, nil
}

// cleanupRunMounts cleans up run mounts so they only appear in this run.
func (b *Builder) cleanupRunMounts(artifacts *runMountArtifacts) error {
	for _, agent := range artifacts.Agents {
		servePath := agent.ServePath()
		if err := agent.Shutdown(); err != nil {
			return fmt.Errorf("shutting down SSH agent at %q: %v", servePath, err)
		}
	}
	// clean up any overlays we mounted
	for _, overlayDirectory := range artifacts.RunOverlayDirs {
		if err := overlay.RemoveTemp(overlayDirectory); err != nil {
			return err
		}
	}
	// unmount anything that needs unmounting
	for _, intermediateMount := range artifacts.IntermediateMounts {
		if err := mount.Unmount(intermediateMount); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("unmounting %q: %w", intermediateMount, err)
		}
		if err := os.Remove(intermediateMount); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing should-be-empty directory %q: %w", intermediateMount, err)
		}
	}
	// unmount any images we mounted for this run
	for _, image := range artifacts.MountedImages {
		if _, err := b.store.UnmountImage(image, false); err != nil {
			logrus.Debugf("umounting image %q: %v", image, err)
		}
	}
	// unlock locks we took, most likely for cache mounts
	volumes.UnlockLockArray(artifacts.TargetLocks)
	return nil
}

// setPdeathsig sets a parent-death signal for the process
// the goroutine that starts the child process should lock itself to
// a native thread using runtime.LockOSThread() until the child exits
func setPdeathsig(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

func relabel(path, mountLabel string, shared bool) error {
	if err := label.Relabel(path, mountLabel, shared); err != nil {
		if !errors.Is(err, syscall.ENOTSUP) {
			return err
		}
		logrus.Debugf("Labeling not supported on %q", path)
	}
	return nil
}

// mapContainerNameToHostname returns the passed-in string with characters that
// don't match validHostnames (defined above) stripped out.
func mapContainerNameToHostname(containerName string) string {
	match := validHostnames.FindStringIndex(containerName)
	if match == nil {
		return ""
	}
	trimmed := containerName[match[0]:]
	match[1] -= match[0]
	match[0] = 0
	for match[1] != len(trimmed) && match[1] < match[0]+maxHostnameLen {
		trimmed = trimmed[:match[1]] + trimmed[match[1]+1:]
		match = validHostnames.FindStringIndex(trimmed)
		match[1] = min(match[1], maxHostnameLen)
	}
	return trimmed[:match[1]]
}

// createMountTargets creates empty files or directories that are used as
// targets for mounts in the spec, and makes a note of what it created.
func (b *Builder) createMountTargets(spec *specs.Spec) ([]copier.ConditionalRemovePath, error) {
	// Avoid anything weird happening, just in case.
	if spec == nil || spec.Root == nil {
		return nil, nil
	}
	rootfsPath := spec.Root.Path
	then := time.Unix(0, 0)
	exemptFromTimesPreservation := map[string]struct{}{
		"dev":  {},
		"proc": {},
		"sys":  {},
	}
	exemptFromRemoval := map[string]struct{}{
		"dev":  {},
		"proc": {},
		"sys":  {},
	}
	overridePermissions := map[string]os.FileMode{
		"dev":  0o755,
		"proc": 0o755,
		"sys":  0o755,
	}
	uidmap, gidmap := convertRuntimeIDMaps(b.IDMappingOptions.UIDMap, b.IDMappingOptions.GIDMap)
	targets := copier.EnsureOptions{
		UIDMap: uidmap,
		GIDMap: gidmap,
	}
	for _, mnt := range spec.Mounts {
		typeFlag := byte(tar.TypeDir)
		// If the mount is a "bind" or "rbind" mount, then it's a bind
		// mount, which means the target _could_ be a non-directory.
		// Check the source and make a note.
		if mnt.Type == define.TypeBind || slices.Contains(mnt.Options, "bind") || slices.Contains(mnt.Options, "rbind") {
			if st, err := os.Stat(mnt.Source); err == nil {
				if !st.IsDir() {
					typeFlag = tar.TypeReg
				}
			}
		}
		// Walk the path components from the root all the way down to
		// the target mountpoint and build a list of pathnames that we
		// need to ensure exist.  If we might need to remove them, give
		// them a conspicuous mtime, so that we can detect if they were
		// unmounted and then modified, in which case we'll want to
		// preserve those changes.
		destination := mnt.Destination
		for destination != "" {
			cleanedDestination := strings.Trim(path.Clean(filepath.ToSlash(destination)), "/")
			modTime := &then
			if _, ok := exemptFromTimesPreservation[cleanedDestination]; ok {
				// don't force a timestamp for this path
				modTime = nil
			}
			var mode *os.FileMode
			if _, ok := exemptFromRemoval[cleanedDestination]; ok {
				// we're not going to filter this out later,
				// so don't make it look weird
				perms := os.FileMode(0o755)
				if typeFlag == tar.TypeReg {
					perms = 0o644
				}
				mode = &perms
				modTime = nil
			}
			if perms, ok := overridePermissions[cleanedDestination]; ok {
				// forced permissions
				mode = &perms
			}
			if mode == nil && destination != cleanedDestination {
				// parent directories default to 0o755, for
				// the sake of commands running as UID != 0
				perms := os.FileMode(0o755)
				mode = &perms
			}
			targets.Paths = append(targets.Paths, copier.EnsurePath{
				Path:     destination,
				Typeflag: typeFlag,
				ModTime:  modTime,
				Chmod:    mode,
			})
			typeFlag = tar.TypeDir
			dir, _ := filepath.Split(destination)
			if destination == dir {
				break
			}
			destination = dir
		}
	}
	if len(targets.Paths) == 0 {
		return nil, nil
	}
	created, noted, err := copier.Ensure(rootfsPath, rootfsPath, targets)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("created mount targets at %v", created)
	logrus.Debugf("parents of mount targets at %+v", noted)
	var remove []copier.ConditionalRemovePath
	for _, target := range created {
		cleanedTarget := strings.Trim(path.Clean(filepath.ToSlash(target)), "/")
		if _, ok := exemptFromRemoval[cleanedTarget]; ok {
			continue
		}
		modTime := &then
		if _, ok := exemptFromTimesPreservation[cleanedTarget]; ok {
			modTime = nil
		}
		condition := copier.ConditionalRemovePath{
			Path:    cleanedTarget,
			ModTime: modTime,
			Owner:   &idtools.IDPair{UID: 0, GID: 0},
		}
		remove = append(remove, condition)
	}
	if len(remove) == 0 {
		return nil, nil
	}
	// encode the set of paths we might need to filter out at commit-time
	// in a way that hopefully doesn't break long-running concurrent Run()
	// calls, that lets us also not have to manage any locking for them
	cdir, err := b.store.ContainerDirectory(b.Container)
	if err != nil {
		return nil, fmt.Errorf("finding working container bookkeeping directory: %w", err)
	}
	for excludesDir, exclusions := range map[string][]copier.ConditionalRemovePath{
		containerExcludesDir: remove,
		containerPulledUpDir: noted,
	} {
		if err := os.Mkdir(filepath.Join(cdir, excludesDir), 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("creating exclusions directory: %w", err)
		}
		encoded, err := json.Marshal(exclusions)
		if err != nil {
			return nil, fmt.Errorf("encoding list of items to exclude at commit-time: %w", err)
		}
		f, err := os.CreateTemp(filepath.Join(cdir, excludesDir), "filter*"+containerExcludesSubstring)
		if err != nil {
			return nil, fmt.Errorf("creating exclusions file: %w", err)
		}
		defer os.Remove(f.Name())
		defer f.Close()
		if err := ioutils.AtomicWriteFile(strings.TrimSuffix(f.Name(), containerExcludesSubstring), encoded, 0o600); err != nil {
			return nil, fmt.Errorf("writing exclusions file: %w", err)
		}
	}
	// return the set of to-remove-now paths directly, in case the caller would prefer
	// to clear them out itself now instead of waiting until commit-time
	return remove, nil
}
