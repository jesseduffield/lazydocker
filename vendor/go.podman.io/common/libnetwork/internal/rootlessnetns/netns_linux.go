package rootlessnetns

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/hashicorp/go-multierror"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/pasta"
	"go.podman.io/common/libnetwork/resolvconf"
	"go.podman.io/common/libnetwork/slirp4netns"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/netns"
	"go.podman.io/common/pkg/systemd"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/homedir"
	"go.podman.io/storage/pkg/lockfile"
	"golang.org/x/sys/unix"
)

const (
	// rootlessNetnsDir is the directory name.
	rootlessNetnsDir = "rootless-netns"
	// refCountFile file name for the ref count file.
	refCountFile = "ref-count"

	// infoCacheFile file name for the cache file used to store the rootless netns info.
	infoCacheFile = "info.json"

	// rootlessNetNsConnPidFile is the name of the rootless netns slirp4netns/pasta pid file.
	rootlessNetNsConnPidFile = "rootless-netns-conn.pid"

	// persistentCNIDir is the directory where the CNI files are stored.
	persistentCNIDir = "/var/lib/cni"

	tmpfs          = "tmpfs"
	none           = "none"
	resolvConfName = "resolv.conf"
)

type Netns struct {
	// dir used for the rootless netns
	dir string
	// backend used for the network setup/teardown
	backend NetworkBackend

	// config contains containers.conf options.
	config *config.Config

	// info contain information about ip addresses used in the netns.
	// A caller can get this info via Info().
	info *types.RootlessNetnsInfo
}

type rootlessNetnsError struct {
	msg string
	err error
}

func (e *rootlessNetnsError) Error() string {
	msg := e.msg + ": "
	return fmt.Sprintf("rootless netns: %s%v", msg, e.err)
}

func (e *rootlessNetnsError) Unwrap() error {
	return e.err
}

// wrapError wraps the error with extra context
// It will always include "rootless netns:" so the msg should not mention it again,
// msg can be empty to just include the rootless netns part.
// err must be non nil.
func wrapError(msg string, err error) *rootlessNetnsError {
	return &rootlessNetnsError{
		msg: msg,
		err: err,
	}
}

func New(dir string, backend NetworkBackend, conf *config.Config) (*Netns, error) {
	netnsDir := filepath.Join(dir, rootlessNetnsDir)
	if err := os.MkdirAll(netnsDir, 0o700); err != nil {
		return nil, wrapError("", err)
	}
	return &Netns{
		dir:     netnsDir,
		backend: backend,
		config:  conf,
	}, nil
}

// getPath is a small wrapper around filepath.Join() to have a bit less code.
func (n *Netns) getPath(path string) string {
	return filepath.Join(n.dir, path)
}

// getOrCreateNetns returns the rootless netns, if it created a new one the
// returned bool is set to true.
func (n *Netns) getOrCreateNetns() (ns.NetNS, bool, error) {
	nsPath := n.getPath(rootlessNetnsDir)
	nsRef, err := ns.GetNS(nsPath)
	if err == nil {
		pidPath := n.getPath(rootlessNetNsConnPidFile)
		pid, err := readPidFile(pidPath)
		if err == nil {
			// quick check if pasta/slirp4netns are still running
			err := unix.Kill(pid, 0)
			if err == nil {
				if err := n.deserializeInfo(); err != nil {
					return nil, false, wrapError("deserialize info", err)
				}
				// All good, return the netns.
				return nsRef, false, nil
			}
			// Print warnings in case things went wrong, we might be able to recover
			// but maybe not so make sure to leave some hints so we can figure out what went wrong.
			if errors.Is(err, unix.ESRCH) {
				logrus.Warn("rootless netns program no longer running, trying to start it again")
			} else {
				logrus.Warnf("failed to check if rootless netns program is running: %v, trying to start it again", err)
			}
		} else {
			logrus.Warnf("failed to read rootless netns program pid: %v", err)
		}
		// In case of errors continue and setup the network cmd again.
	} else {
		// Special case, the file might exist already but is not a valid netns.
		// One reason could be that a previous setup was killed between creating
		// the file and mounting it. Or if the file is not on tmpfs (deleted on boot)
		// you might run into it as well: https://github.com/containers/podman/issues/25144
		// We have to do this because NewNSAtPath fails with EEXIST otherwise
		if errors.As(err, &ns.NSPathNotNSErr{}) {
			// We don't care if this fails, NewNSAtPath() should return the real error.
			_ = os.Remove(nsPath)
		}
		logrus.Debugf("Creating rootless network namespace at %q", nsPath)
		// We have to create the netns dir again here because it is possible
		// that cleanup() removed it.
		if err := os.MkdirAll(n.dir, 0o700); err != nil {
			return nil, false, wrapError("", err)
		}
		nsRef, err = netns.NewNSAtPath(nsPath)
		if err != nil {
			return nil, false, wrapError("create netns", err)
		}
	}
	switch strings.ToLower(n.config.Network.DefaultRootlessNetworkCmd) {
	case "", slirp4netns.BinaryName:
		err = n.setupSlirp4netns(nsPath)
	case pasta.BinaryName:
		err = n.setupPasta(nsPath)
	default:
		err = fmt.Errorf("invalid rootless network command %q", n.config.Network.DefaultRootlessNetworkCmd)
	}
	// If pasta or slirp4netns fail here we need to get rid of the netns again to not leak it,
	// otherwise the next command thinks the netns was successfully setup.
	if err != nil {
		if nerr := netns.UnmountNS(nsPath); nerr != nil {
			logrus.Error(nerr)
		}
		_ = nsRef.Close()
		return nil, false, err
	}

	return nsRef, true, nil
}

func (n *Netns) cleanup() error {
	if err := fileutils.Exists(n.dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// dir does not exists no need for cleanup
			return nil
		}
		return err
	}

	logrus.Debug("Cleaning up rootless network namespace")

	nsPath := n.getPath(rootlessNetnsDir)
	var multiErr *multierror.Error
	if err := netns.UnmountNS(nsPath); err != nil {
		multiErr = multierror.Append(multiErr, err)
	}
	if err := n.cleanupRootlessNetns(); err != nil {
		multiErr = multierror.Append(multiErr, wrapError("kill network process", err))
	}
	if err := os.RemoveAll(n.dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		multiErr = multierror.Append(multiErr, wrapError("remove rootless netns dir", err))
	}

	return multiErr.ErrorOrNil()
}

func (n *Netns) setupPasta(nsPath string) error {
	pidPath := n.getPath(rootlessNetNsConnPidFile)

	pastaOpts := pasta.SetupOptions{
		Config:       n.config,
		Netns:        nsPath,
		ExtraOptions: []string{"--pid", pidPath},
	}
	res, err := pasta.Setup(&pastaOpts)
	if err != nil {
		return fmt.Errorf("setting up Pasta: %w", err)
	}

	if systemd.RunsOnSystemd() {
		// Treat these as fatal - if pasta failed to write a PID file something is probably wrong.
		pid, err := readPidFile(pidPath)
		if err != nil {
			return fmt.Errorf("unable to decode pasta PID: %w", err)
		}

		if err := systemd.MoveRootlessNetnsSlirpProcessToUserSlice(pid); err != nil {
			// only log this, it is not fatal but can lead to issues when running podman inside systemd units
			logrus.Errorf("failed to move the rootless netns pasta process to the systemd user.slice: %v", err)
		}
	}

	if err := resolvconf.New(&resolvconf.Params{
		Path: n.getPath(resolvConfName),
		// fake the netns since we want to filter localhost
		Namespaces: []specs.LinuxNamespace{
			{Type: specs.NetworkNamespace},
		},
		IPv6Enabled:     res.IPv6,
		KeepHostServers: true,
		Nameservers:     res.DNSForwardIPs,
	}); err != nil {
		return wrapError("create resolv.conf", err)
	}

	n.info = &types.RootlessNetnsInfo{
		IPAddresses:   res.IPAddresses,
		DnsForwardIps: res.DNSForwardIPs,
		MapGuestIps:   res.MapGuestAddrIPs,
	}
	if err := n.serializeInfo(); err != nil {
		return wrapError("serialize info", err)
	}

	return nil
}

func (n *Netns) setupSlirp4netns(nsPath string) error {
	res, err := slirp4netns.Setup(&slirp4netns.SetupOptions{
		Config:      n.config,
		ContainerID: "rootless-netns",
		Netns:       nsPath,
	})
	if err != nil {
		return wrapError("start slirp4netns", err)
	}
	// create pid file for the slirp4netns process
	// this is need to kill the process in the cleanup
	pid := strconv.Itoa(res.Pid)
	err = os.WriteFile(n.getPath(rootlessNetNsConnPidFile), []byte(pid), 0o600)
	if err != nil {
		return wrapError("write slirp4netns pid file", err)
	}

	if systemd.RunsOnSystemd() {
		// move to systemd scope to prevent systemd from killing it
		err = systemd.MoveRootlessNetnsSlirpProcessToUserSlice(res.Pid)
		if err != nil {
			// only log this, it is not fatal but can lead to issues when running podman inside systemd units
			logrus.Errorf("failed to move the rootless netns slirp4netns process to the systemd user.slice: %v", err)
		}
	}

	// build a new resolv.conf file which uses the slirp4netns dns server address
	resolveIP, err := slirp4netns.GetDNS(res.Subnet)
	if err != nil {
		return wrapError("determine default slirp4netns DNS address", err)
	}
	nameservers := []string{resolveIP.String()}

	netnsIP, err := slirp4netns.GetIP(res.Subnet)
	if err != nil {
		return wrapError("determine default slirp4netns ip address", err)
	}

	if err := resolvconf.New(&resolvconf.Params{
		Path: n.getPath(resolvConfName),
		// fake the netns since we want to filter localhost
		Namespaces: []specs.LinuxNamespace{
			{Type: specs.NetworkNamespace},
		},
		IPv6Enabled:     res.IPv6,
		KeepHostServers: true,
		Nameservers:     nameservers,
	}); err != nil {
		return wrapError("create resolv.conf", err)
	}

	n.info = &types.RootlessNetnsInfo{
		IPAddresses:   []net.IP{*netnsIP},
		DnsForwardIps: nameservers,
	}
	if err := n.serializeInfo(); err != nil {
		return wrapError("serialize info", err)
	}

	return nil
}

func (n *Netns) cleanupRootlessNetns() error {
	pidFile := n.getPath(rootlessNetNsConnPidFile)
	pid, err := readPidFile(pidFile)
	// do not hard error if the file dos not exists, cleanup should be idempotent
	if errors.Is(err, fs.ErrNotExist) {
		logrus.Debugf("Rootless netns conn pid file does not exists %s", pidFile)
		return nil
	}
	if err == nil {
		// kill the slirp/pasta process so we do not leak it
		err = unix.Kill(pid, unix.SIGTERM)
		if err == unix.ESRCH {
			err = nil
		}
	}
	return err
}

// mountAndMkdirDest convenience wrapper for mount and mkdir.
func mountAndMkdirDest(source string, target string, fstype string, flags uintptr) error {
	if err := os.MkdirAll(target, 0o700); err != nil {
		return wrapError("create mount point", err)
	}
	if err := unix.Mount(source, target, fstype, flags, ""); err != nil {
		return wrapError(fmt.Sprintf("mount %q to %q", source, target), err)
	}
	return nil
}

func (n *Netns) setupMounts() error {
	// Before we can run the given function,
	// we have to set up all mounts correctly.

	// The order of the mounts is IMPORTANT.
	// The idea of the extra mount ns is to make /run and /var/lib/cni writeable
	// for the cni plugins but not affecting the podman user namespace.
	// Because the plugins also need access to XDG_RUNTIME_DIR/netns some special setup is needed.

	// The following bind mounts are needed
	// 1. XDG_RUNTIME_DIR -> XDG_RUNTIME_DIR/rootless-netns/XDG_RUNTIME_DIR
	// 2. /run/systemd -> XDG_RUNTIME_DIR/rootless-netns/run/systemd (only if it exists)
	// 3. XDG_RUNTIME_DIR/rootless-netns/resolv.conf -> /etc/resolv.conf or XDG_RUNTIME_DIR/rootless-netns/run/symlink/target
	// 4. XDG_RUNTIME_DIR/rootless-netns/var/lib/cni -> /var/lib/cni (if /var/lib/cni does not exist, use the parent dir)
	// 5. XDG_RUNTIME_DIR/rootless-netns/run -> /run

	// Create a new mount namespace,
	// this must happen inside the netns thread.
	err := unix.Unshare(unix.CLONE_NEWNS)
	if err != nil {
		return wrapError("create new mount namespace", err)
	}

	// Ensure we mount private in our mountns to prevent accidentally
	// overwriting the host mounts in case the default propagation is shared.
	// However using private propagation is not what we want. New mounts/umounts
	// would not be propagated into our namespace. This is a problem because we
	// may hold mount points open that were unmounted on the host confusing users
	// why the underlying device is still busy as they no longer see the mount:
	// https://github.com/containers/podman/issues/25994
	err = unix.Mount("", "/", "", unix.MS_SLAVE|unix.MS_REC, "")
	if err != nil {
		return wrapError("set mount propagation to slave in new mount namespace", err)
	}

	xdgRuntimeDir, err := homedir.GetRuntimeDir()
	if err != nil {
		return fmt.Errorf("could not get runtime directory: %w", err)
	}
	newXDGRuntimeDir := n.getPath(xdgRuntimeDir)
	// 1. Mount the netns into the new run to keep them accessible.
	// Otherwise cni setup will fail because it cannot access the netns files.
	err = mountAndMkdirDest(xdgRuntimeDir, newXDGRuntimeDir, none, unix.MS_BIND|unix.MS_REC)
	if err != nil {
		return err
	}

	// 2. Also keep /run/systemd if it exists.
	// Many files are symlinked into this dir, for example /dev/log.
	runSystemd := "/run/systemd"
	err = fileutils.Exists(runSystemd)
	if err == nil {
		newRunSystemd := n.getPath(runSystemd)
		err = mountAndMkdirDest(runSystemd, newRunSystemd, none, unix.MS_BIND|unix.MS_REC)
		if err != nil {
			return err
		}
	}

	// 3. On some distros /etc/resolv.conf is symlinked to somewhere under /run.
	// Because the kernel will follow the symlink before mounting, it is not
	// possible to mount a file at /etc/resolv.conf. We have to ensure that
	// the link target will be available in the mount ns.
	// see: https://github.com/containers/podman/issues/10855
	resolvePath := resolvconf.DefaultResolvConf
	linkCount := 0
	for i := 1; i < len(resolvePath); i++ {
		// Do not use filepath.EvalSymlinks, we only want the first symlink under /run.
		// If /etc/resolv.conf has more than one symlink under /run, e.g.
		// -> /run/systemd/resolve/stub-resolv.conf -> /run/systemd/resolve/resolv.conf
		// we would put the netns resolv.conf file to the last path. However this will
		// break dns because the second link does not exist in the mount ns.
		// see https://github.com/containers/podman/issues/11222
		//
		// We also need to resolve all path components not just the last file.
		// see https://github.com/containers/podman/issues/12461

		if resolvePath[i] != '/' {
			// if we are at the last char we need to inc i by one because there is no final slash
			if i == len(resolvePath)-1 {
				i++
			} else {
				// not the end of path, keep going
				continue
			}
		}
		path := resolvePath[:i]

		fi, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("failed to stat resolv.conf path: %w", err)
		}

		// no link, just continue
		if fi.Mode()&os.ModeSymlink == 0 {
			continue
		}

		link, err := os.Readlink(path)
		if err != nil {
			return fmt.Errorf("failed to read resolv.conf symlink: %w", err)
		}
		linkCount++
		if filepath.IsAbs(link) {
			// link is as an absolute path
			resolvePath = filepath.Join(link, resolvePath[i:])
		} else {
			// link is as a relative, join it with the previous path
			base := filepath.Dir(path)
			resolvePath = filepath.Join(base, link, resolvePath[i:])
		}
		// set i back to zero since we now have a new base path
		i = 0

		// we have to stop at the first path under /run because we will have an empty /run and will create the path anyway
		// if we would continue we would need to recreate all links under /run
		if strings.HasPrefix(resolvePath, "/run/") {
			break
		}
		// make sure wo do not loop forever
		if linkCount == 255 {
			return errors.New("too many symlinks while resolving /etc/resolv.conf")
		}
	}
	logrus.Debugf("The path of /etc/resolv.conf in the mount ns is %q", resolvePath)
	// When /etc/resolv.conf on the host is a symlink to /run/systemd/resolve/stub-resolv.conf,
	// we have to mount an empty filesystem on /run/systemd/resolve in the child namespace,
	// so as to isolate the directory from the host mount namespace.
	//
	// Otherwise our bind-mount for /run/systemd/resolve/stub-resolv.conf is unmounted
	// when systemd-resolved unlinks and recreates /run/systemd/resolve/stub-resolv.conf on the host.
	// see: https://github.com/containers/podman/issues/10929
	if strings.HasPrefix(resolvePath, "/run/systemd/resolve/") {
		rsr := n.getPath("/run/systemd/resolve")
		err = mountAndMkdirDest("", rsr, tmpfs, unix.MS_NOEXEC|unix.MS_NOSUID|unix.MS_NODEV)
		if err != nil {
			return err
		}
	}
	if strings.HasPrefix(resolvePath, "/run/") {
		resolvePath = n.getPath(resolvePath)
		err = os.MkdirAll(filepath.Dir(resolvePath), 0o700)
		if err != nil {
			return wrapError("create resolv.conf directory", err)
		}
		// we want to bind mount on this file so we have to create the file first
		_, err = os.OpenFile(resolvePath, os.O_CREATE|os.O_RDONLY, 0o600)
		if err != nil {
			return wrapError("create resolv.conf file: %w", err)
		}
	}
	// mount resolv.conf to make use of the host dns
	err = unix.Mount(n.getPath(resolvConfName), resolvePath, none, unix.MS_BIND, "")
	if err != nil {
		return wrapError(fmt.Sprintf("mount resolv.conf to %q", resolvePath), err)
	}

	// 4. CNI plugins need access to /var/lib/cni
	if n.backend == CNI {
		if err := n.mountCNIVarDir(); err != nil {
			return err
		}
	}

	// 5. Mount the new prepared run dir to /run, it has to be recursive to keep the other bind mounts.
	runDir := n.getPath("run")
	err = os.MkdirAll(runDir, 0o700)
	if err != nil {
		return wrapError("create run directory", err)
	}
	// relabel the new run directory to the iptables /run label
	// this is important, otherwise the iptables command will fail
	err = label.Relabel(runDir, "system_u:object_r:iptables_var_run_t:s0", false)
	if err != nil {
		if !errors.Is(err, unix.ENOTSUP) {
			return wrapError("relabel iptables_var_run_t", err)
		}
		logrus.Debugf("Labeling not supported on %q", runDir)
	}
	err = mountAndMkdirDest(runDir, "/run", none, unix.MS_BIND|unix.MS_REC)
	if err != nil {
		return err
	}
	return nil
}

func (n *Netns) mountCNIVarDir() error {
	varDir := ""
	varTarget := persistentCNIDir
	// we can only mount to a target dir which exists, check /var/lib/cni recursively
	// while we could always use /var there are cases where a user might store the cni
	// configs under /var/custom and this would break
	for {
		if err := fileutils.Exists(varTarget); err == nil {
			varDir = n.getPath(varTarget)
			break
		}
		varTarget = filepath.Dir(varTarget)
		if varTarget == "/" {
			break
		}
	}
	if varDir == "" {
		return errors.New("failed to stat /var directory")
	}
	if err := os.MkdirAll(varDir, 0o700); err != nil {
		return wrapError("create var dir", err)
	}
	// make sure to mount var first
	err := unix.Mount(varDir, varTarget, none, unix.MS_BIND, "")
	if err != nil {
		return wrapError(fmt.Sprintf("mount %q to %q", varDir, varTarget), err)
	}
	return nil
}

func (n *Netns) runInner(toRun func() error, cleanup bool) (err error) {
	nsRef, newNs, err := n.getOrCreateNetns()
	if err != nil {
		return err
	}
	defer nsRef.Close()
	// If a new netns was created make sure to clean it up again on an error to not leak it if requested.
	if newNs && cleanup {
		defer func() {
			if err != nil {
				if err := n.cleanup(); err != nil {
					logrus.Errorf("Rootless netns cleanup error after failed setup: %v", err)
				}
			}
		}()
	}

	return nsRef.Do(func(_ ns.NetNS) error {
		if err := n.setupMounts(); err != nil {
			return err
		}
		if err := toRun(); err != nil {
			return err
		}
		return nil
	})
}

func (n *Netns) Setup(nets int, toRun func() error) error {
	err := n.runInner(toRun, true)
	if err != nil {
		return err
	}
	_, err = refCount(n.dir, nets)
	return err
}

func (n *Netns) Teardown(nets int, toRun func() error) error {
	err := n.runInner(toRun, true)
	if err != nil {
		return err
	}
	// decrement only if teardown didn't fail, podman will call us again on errors so we should not double decrement
	count, err := refCount(n.dir, -nets)
	if err != nil {
		return err
	}

	// cleanup when ref count is 0
	if count == 0 {
		return n.cleanup()
	}

	return nil
}

// Run any long running function in the userns.
// We need to ensure that during setup/cleanup we are locked to avoid races.
// However because the given function could be running a long time we must
// unlock in between, i.e. this is used by podman unshare --rootless-nets
// and we do not want to keep it locked for the lifetime of the given command.
func (n *Netns) Run(lock *lockfile.LockFile, toRun func() error) error {
	lock.Lock()
	defer lock.Unlock()
	_, err := refCount(n.dir, 1)
	if err != nil {
		return err
	}
	inner := func() error {
		lock.Unlock()
		err = toRun()
		lock.Lock()
		return err
	}

	inErr := n.runInner(inner, false)
	// make sure to always reset the ref counter afterwards
	count, err := refCount(n.dir, -1)
	if err != nil {
		if inErr == nil {
			return err
		}
		logrus.Errorf("Failed to decrement ref count: %v", err)
		return inErr
	}

	if count == 0 {
		err = n.cleanup()
		if err != nil {
			return wrapError("cleanup", err)
		}
	}

	return inErr
}

// Info returns the currently used ip addresses for the rootless-netns.
// These should be used to configure the containers resolv.conf and
// host.containers.internal entries.
func (n *Netns) Info() *types.RootlessNetnsInfo {
	return n.info
}

func refCount(dir string, inc int) (int, error) {
	file := filepath.Join(dir, refCountFile)
	content, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return -1, wrapError("read ref counter", err)
	}

	currentCount := 0
	if len(content) > 0 {
		currentCount, err = strconv.Atoi(string(content))
		if err != nil {
			return -1, wrapError("parse ref counter", err)
		}
	}

	currentCount += inc
	if currentCount < 0 {
		logrus.Errorf("rootless netns ref counter out of sync, counter is at %d, resetting it back to 0", currentCount)
		currentCount = 0
	}

	newNum := strconv.Itoa(currentCount)
	if err = os.WriteFile(file, []byte(newNum), 0o600); err != nil {
		return -1, wrapError("write ref counter", err)
	}

	return currentCount, nil
}

func readPidFile(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

func (n *Netns) serializeInfo() error {
	f, err := os.Create(filepath.Join(n.dir, infoCacheFile))
	if err != nil {
		return err
	}
	return json.NewEncoder(f).Encode(n.info)
}

func (n *Netns) deserializeInfo() error {
	f, err := os.Open(filepath.Join(n.dir, infoCacheFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()
	if n.info == nil {
		n.info = new(types.RootlessNetnsInfo)
	}
	return json.NewDecoder(f).Decode(n.info)
}
