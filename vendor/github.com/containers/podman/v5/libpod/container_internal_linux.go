//go:build !remote

package libpod

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/shutdown"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/cyphar/filepath-securejoin/pathrs-lite"
	"github.com/moby/sys/capability"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/opencontainers/selinux/go-selinux/label"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/slirp4netns"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/cgroups"
	"go.podman.io/common/pkg/config"
	"golang.org/x/sys/unix"
)

var (
	bindOptions = []string{define.TypeBind, "rprivate"}
)

func (c *Container) mountSHM(shmOptions string) error {
	contextType := "context"
	if c.config.LabelNested {
		contextType = "rootcontext"
	}

	if err := unix.Mount("shm", c.config.ShmDir, define.TypeTmpfs, unix.MS_NOEXEC|unix.MS_NOSUID|unix.MS_NODEV,
		label.FormatMountLabelByType(shmOptions, c.config.MountLabel, contextType)); err != nil {
		return fmt.Errorf("failed to mount shm tmpfs %q: %w", c.config.ShmDir, err)
	}
	return nil
}

func (c *Container) unmountSHM(mount string) error {
	if err := unix.Unmount(mount, 0); err != nil {
		if err != syscall.EINVAL && err != syscall.ENOENT {
			return fmt.Errorf("unmounting container %s SHM mount %s: %w", c.ID(), mount, err)
		}
		// If it's just an EINVAL or ENOENT, debug logs only
		logrus.Debugf("Container %s failed to unmount %s : %v", c.ID(), mount, err)
	}
	return nil
}

// prepare mounts the container and sets up other required resources like net
// namespaces
func (c *Container) prepare() error {
	var (
		wg                              sync.WaitGroup
		netNS                           string
		networkStatus                   map[string]types.StatusBlock
		createNetNSErr, mountStorageErr error
		mountPoint                      string
		tmpStateLock                    sync.Mutex
	)

	shutdown.Inhibit()
	defer shutdown.Uninhibit()

	wg.Add(2)

	go func() {
		defer wg.Done()
		if c.state.State == define.ContainerStateStopped {
			// networking should not be reused after a stop
			if err := c.cleanupNetwork(); err != nil {
				createNetNSErr = err
				return
			}
		}

		// Set up network namespace if not already set up
		noNetNS := c.state.NetNS == ""
		if c.config.CreateNetNS && noNetNS && !c.config.PostConfigureNetNS {
			c.reservedPorts, createNetNSErr = c.bindPorts()
			if createNetNSErr != nil {
				return
			}

			netNS, networkStatus, createNetNSErr = c.runtime.createNetNS(c)
			if createNetNSErr != nil {
				return
			}

			tmpStateLock.Lock()
			defer tmpStateLock.Unlock()

			// Assign NetNS attributes to container
			c.state.NetNS = netNS
			c.state.NetworkStatus = networkStatus
		}
	}()
	// Mount storage if not mounted
	go func() {
		defer wg.Done()
		mountPoint, mountStorageErr = c.mountStorage()

		if mountStorageErr != nil {
			return
		}

		tmpStateLock.Lock()
		defer tmpStateLock.Unlock()

		// Finish up mountStorage
		c.state.Mounted = true
		c.state.Mountpoint = mountPoint

		logrus.Debugf("Created root filesystem for container %s at %s", c.ID(), c.state.Mountpoint)
	}()

	wg.Wait()

	var createErr error
	if createNetNSErr != nil {
		createErr = createNetNSErr
	}
	if mountStorageErr != nil {
		if createErr != nil {
			logrus.Errorf("Preparing container %s: %v", c.ID(), createErr)
		}
		createErr = mountStorageErr
	}

	// Only trigger storage cleanup if mountStorage was successful.
	// Otherwise, we may mess up mount counters.
	if createNetNSErr != nil && mountStorageErr == nil {
		if err := c.cleanupStorage(); err != nil {
			// createErr is guaranteed non-nil, so print
			// unconditionally
			logrus.Errorf("Preparing container %s: %v", c.ID(), createErr)
			createErr = fmt.Errorf("unmounting storage for container %s after network create failure: %w", c.ID(), err)
		}
	}

	// It's OK to unconditionally trigger network cleanup. If the network
	// isn't ready it will do nothing.
	if createErr != nil {
		if err := c.cleanupNetwork(); err != nil {
			logrus.Errorf("Preparing container %s: %v", c.ID(), createErr)
			createErr = fmt.Errorf("cleaning up container %s network after setup failure: %w", c.ID(), err)
		}
	}

	if createErr != nil {
		for _, f := range c.reservedPorts {
			// make sure to close all ports again on errors
			f.Close()
		}
		c.reservedPorts = nil
		return createErr
	}

	// Save changes to container state
	if err := c.save(); err != nil {
		return err
	}

	return nil
}

// cleanupNetwork unmounts and cleans up the container's network
func (c *Container) cleanupNetwork() error {
	if c.config.NetNsCtr != "" {
		return nil
	}
	netDisabled, err := c.NetworkDisabled()
	if err != nil {
		return err
	}
	if netDisabled {
		return nil
	}
	if c.state.NetNS == "" {
		logrus.Debugf("Network is already cleaned up, skipping...")
		return nil
	}

	// Stop the container's network namespace (if it has one)
	neterr := c.runtime.teardownNetNS(c)
	c.state.NetNS = ""
	c.state.NetworkStatus = nil

	// always save even when there was an error
	err = c.save()
	if err != nil {
		if neterr != nil {
			logrus.Errorf("Unable to clean up network for container %s: %q", c.ID(), neterr)
		}
		return err
	}

	return neterr
}

// reloadNetwork reloads the network for the given container, recreating
// firewall rules.
func (c *Container) reloadNetwork() error {
	result, err := c.runtime.reloadContainerNetwork(c)
	if err != nil {
		return err
	}

	c.state.NetworkStatus = result

	return c.save()
}

// systemd expects to have /run, /run/lock and /tmp on tmpfs
// It also expects to be able to write to /sys/fs/cgroup/systemd and /var/log/journal
func (c *Container) setupSystemd(mounts []spec.Mount, g generate.Generator) error {
	var containerUUIDSet bool
	for _, s := range c.config.Spec.Process.Env {
		if strings.HasPrefix(s, "container_uuid=") {
			containerUUIDSet = true
			break
		}
	}
	if !containerUUIDSet {
		g.AddProcessEnv("container_uuid", c.ID()[:32])
	}
	// limit systemd-specific tmpfs mounts if specified
	// while creating a pod or ctr, if not, default back to 50%
	var shmSizeSystemdMntOpt string
	if c.config.ShmSizeSystemd != 0 {
		shmSizeSystemdMntOpt = fmt.Sprintf("size=%d", c.config.ShmSizeSystemd)
	}
	options := []string{"rw", "rprivate", "nosuid", "nodev"}
	for _, dest := range []string{"/run", "/run/lock"} {
		if MountExists(mounts, dest) {
			continue
		}
		tmpfsMnt := spec.Mount{
			Destination: dest,
			Type:        define.TypeTmpfs,
			Source:      define.TypeTmpfs,
			Options:     append(options, "tmpcopyup", shmSizeSystemdMntOpt),
		}
		g.AddMount(tmpfsMnt)
	}
	for _, dest := range []string{"/tmp", "/var/log/journal"} {
		if MountExists(mounts, dest) {
			continue
		}
		tmpfsMnt := spec.Mount{
			Destination: dest,
			Type:        define.TypeTmpfs,
			Source:      define.TypeTmpfs,
			Options:     append(options, "tmpcopyup", shmSizeSystemdMntOpt),
		}
		g.AddMount(tmpfsMnt)
	}

	unified, err := cgroups.IsCgroup2UnifiedMode()
	if err != nil {
		return err
	}

	hasCgroupNs := false
	for _, ns := range c.config.Spec.Linux.Namespaces {
		if ns.Type == spec.CgroupNamespace {
			hasCgroupNs = true
			break
		}
	}

	if unified {
		g.RemoveMount("/sys/fs/cgroup")

		var systemdMnt spec.Mount
		if hasCgroupNs {
			systemdMnt = spec.Mount{
				Destination: "/sys/fs/cgroup",
				Type:        "cgroup",
				Source:      "cgroup",
				Options:     []string{"private", "rw"},
			}
		} else {
			systemdMnt = spec.Mount{
				Destination: "/sys/fs/cgroup",
				Type:        define.TypeBind,
				Source:      "/sys/fs/cgroup",
				Options:     []string{define.TypeBind, "private", "rw"},
			}
		}
		g.AddMount(systemdMnt)
	} else {
		hasSystemdMount := MountExists(mounts, "/sys/fs/cgroup/systemd")
		if hasCgroupNs && !hasSystemdMount {
			return errors.New("cgroup namespace is not supported with cgroup v1 and systemd mode")
		}
		mountOptions := []string{define.TypeBind, "rprivate"}

		if !hasSystemdMount {
			skipMount := hasSystemdMount
			var statfs unix.Statfs_t
			if err := unix.Statfs("/sys/fs/cgroup/systemd", &statfs); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					// If the mount is missing on the host, we cannot bind mount it so
					// just skip it.
					skipMount = true
				}
				mountOptions = append(mountOptions, "nodev", "noexec", "nosuid")
			} else {
				if statfs.Flags&unix.MS_NODEV == unix.MS_NODEV {
					mountOptions = append(mountOptions, "nodev")
				}
				if statfs.Flags&unix.MS_NOEXEC == unix.MS_NOEXEC {
					mountOptions = append(mountOptions, "noexec")
				}
				if statfs.Flags&unix.MS_NOSUID == unix.MS_NOSUID {
					mountOptions = append(mountOptions, "nosuid")
				}
				if statfs.Flags&unix.MS_RDONLY == unix.MS_RDONLY {
					mountOptions = append(mountOptions, "ro")
				}
			}
			if !skipMount {
				systemdMnt := spec.Mount{
					Destination: "/sys/fs/cgroup/systemd",
					Type:        define.TypeBind,
					Source:      "/sys/fs/cgroup/systemd",
					Options:     mountOptions,
				}
				g.AddMount(systemdMnt)
				g.AddLinuxMaskedPaths("/sys/fs/cgroup/systemd/release_agent")
			}
		}
	}

	return nil
}

// Add an existing container's namespace to the spec
func (c *Container) addNamespaceContainer(g *generate.Generator, ns LinuxNS, ctr string, specNS spec.LinuxNamespaceType) error {
	nsCtr, err := c.runtime.state.Container(ctr)
	if err != nil {
		return fmt.Errorf("retrieving dependency %s of container %s from state: %w", ctr, c.ID(), err)
	}

	if specNS == spec.UTSNamespace {
		hostname := nsCtr.Hostname()
		// Joining an existing namespace, cannot set the hostname
		g.SetHostname("")
		g.AddProcessEnv("HOSTNAME", hostname)
	}

	nsPath, err := nsCtr.NamespacePath(ns)
	if err != nil {
		return err
	}

	if err := g.AddOrReplaceLinuxNamespace(string(specNS), nsPath); err != nil {
		return err
	}

	return nil
}

func isRootlessCgroupSet(cgroup string) bool {
	// old versions of podman were setting the CgroupParent to CgroupfsDefaultCgroupParent
	// by default.  Avoid breaking these versions and check whether the cgroup parent is
	// set to the default and in this case enable the old behavior.  It should not be a real
	// problem because the default CgroupParent is usually owned by root so rootless users
	// cannot access it.
	// This check might be lifted in a future version of Podman.
	// Check both that the cgroup or its parent is set to the default value (used by pods).
	return cgroup != CgroupfsDefaultCgroupParent && filepath.Dir(cgroup) != CgroupfsDefaultCgroupParent
}

func (c *Container) expectPodCgroup() (bool, error) {
	unified, err := cgroups.IsCgroup2UnifiedMode()
	if err != nil {
		return false, err
	}
	cgroupManager := c.CgroupManager()
	switch {
	case c.config.NoCgroups:
		return false, nil
	case cgroupManager == config.SystemdCgroupsManager:
		return !rootless.IsRootless() || unified, nil
	case cgroupManager == config.CgroupfsCgroupsManager:
		return !rootless.IsRootless(), nil
	default:
		return false, fmt.Errorf("invalid cgroup mode %s requested for pods: %w", cgroupManager, define.ErrInvalidArg)
	}
}

// Get cgroup path in a format suitable for the OCI spec
func (c *Container) getOCICgroupPath() (string, error) {
	unified, err := cgroups.IsCgroup2UnifiedMode()
	if err != nil {
		return "", err
	}
	cgroupManager := c.CgroupManager()
	switch {
	case c.config.NoCgroups:
		return "", nil
	case c.config.CgroupsMode == cgroupSplit:
		selfCgroup, err := cgroups.GetOwnCgroupDisallowRoot()
		if err != nil {
			return "", err
		}
		return filepath.Join(selfCgroup, fmt.Sprintf("libpod-payload-%s", c.ID())), nil
	case cgroupManager == config.SystemdCgroupsManager:
		// When the OCI runtime is set to use Systemd as a cgroup manager, it
		// expects cgroups to be passed as follows:
		// slice:prefix:name
		systemdCgroups := fmt.Sprintf("%s:libpod:%s", path.Base(c.config.CgroupParent), c.ID())
		logrus.Debugf("Setting Cgroups for container %s to %s", c.ID(), systemdCgroups)
		return systemdCgroups, nil
	case (rootless.IsRootless() && (cgroupManager == config.CgroupfsCgroupsManager || !unified)):
		if c.config.CgroupParent == "" || !isRootlessCgroupSet(c.config.CgroupParent) {
			return "", nil
		}
		fallthrough
	case cgroupManager == config.CgroupfsCgroupsManager:
		cgroupPath := filepath.Join(c.config.CgroupParent, fmt.Sprintf("libpod-%s", c.ID()))
		logrus.Debugf("Setting Cgroup path for container %s to %s", c.ID(), cgroupPath)
		return cgroupPath, nil
	default:
		return "", fmt.Errorf("invalid cgroup manager %s requested: %w", cgroupManager, define.ErrInvalidArg)
	}
}

func openDirectory(path string) (fd int, err error) {
	return unix.Open(path, unix.O_RDONLY|unix.O_PATH|unix.O_CLOEXEC, 0)
}

func (c *Container) addNetworkNamespace(g *generate.Generator) error {
	if c.config.CreateNetNS {
		if c.config.PostConfigureNetNS {
			if err := g.AddOrReplaceLinuxNamespace(string(spec.NetworkNamespace), ""); err != nil {
				return err
			}
		} else {
			if err := g.AddOrReplaceLinuxNamespace(string(spec.NetworkNamespace), c.state.NetNS); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Container) addSystemdMounts(g *generate.Generator) error {
	if c.Systemd() {
		if err := c.setupSystemd(g.Mounts(), *g); err != nil {
			return err
		}
	}
	return nil
}

func (c *Container) addSharedNamespaces(g *generate.Generator) error {
	if c.config.IPCNsCtr != "" {
		if err := c.addNamespaceContainer(g, IPCNS, c.config.IPCNsCtr, spec.IPCNamespace); err != nil {
			return err
		}
	}
	if c.config.MountNsCtr != "" {
		if err := c.addNamespaceContainer(g, MountNS, c.config.MountNsCtr, spec.MountNamespace); err != nil {
			return err
		}
	}
	if c.config.NetNsCtr != "" {
		if err := c.addNamespaceContainer(g, NetNS, c.config.NetNsCtr, spec.NetworkNamespace); err != nil {
			return err
		}
	}
	if c.config.PIDNsCtr != "" {
		if err := c.addNamespaceContainer(g, PIDNS, c.config.PIDNsCtr, spec.PIDNamespace); err != nil {
			return err
		}
	}
	if c.config.UserNsCtr != "" {
		if err := c.addNamespaceContainer(g, UserNS, c.config.UserNsCtr, spec.UserNamespace); err != nil {
			return err
		}
		if len(g.Config.Linux.UIDMappings) == 0 {
			// runc complains if no mapping is specified, even if we join another ns.  So provide a dummy mapping
			g.AddLinuxUIDMapping(uint32(0), uint32(0), uint32(1))
			g.AddLinuxGIDMapping(uint32(0), uint32(0), uint32(1))
		}
	}

	availableUIDs, availableGIDs, err := rootless.GetAvailableIDMaps()
	if err != nil {
		if os.IsNotExist(err) {
			// The kernel-provided files only exist if user namespaces are supported
			logrus.Debugf("User or group ID mappings not available: %s", err)
		} else {
			return err
		}
	} else {
		g.Config.Linux.UIDMappings = rootless.MaybeSplitMappings(g.Config.Linux.UIDMappings, availableUIDs)
		g.Config.Linux.GIDMappings = rootless.MaybeSplitMappings(g.Config.Linux.GIDMappings, availableGIDs)
	}

	// Hostname handling:
	// If we have a UTS namespace, set Hostname in the OCI spec.
	// Set the HOSTNAME environment variable unless explicitly overridden by
	// the user (already present in OCI spec). If we don't have a UTS ns,
	// set it to the host's hostname instead.
	hostname := c.Hostname()
	foundUTS := false

	for _, i := range c.config.Spec.Linux.Namespaces {
		if i.Type == spec.UTSNamespace && i.Path == "" {
			foundUTS = true
			g.SetHostname(hostname)
			break
		}
	}
	if !foundUTS {
		tmpHostname, err := os.Hostname()
		if err != nil {
			return err
		}
		hostname = tmpHostname
	}
	needEnv := true
	for _, checkEnv := range g.Config.Process.Env {
		if strings.SplitN(checkEnv, "=", 2)[0] == "HOSTNAME" {
			needEnv = false
			break
		}
	}
	if needEnv {
		g.AddProcessEnv("HOSTNAME", hostname)
	}

	if c.config.UTSNsCtr != "" {
		if err := c.addNamespaceContainer(g, UTSNS, c.config.UTSNsCtr, spec.UTSNamespace); err != nil {
			return err
		}
	}
	if c.config.CgroupNsCtr != "" {
		if err := c.addNamespaceContainer(g, CgroupNS, c.config.CgroupNsCtr, spec.CgroupNamespace); err != nil {
			return err
		}
	}

	if c.config.UserNsCtr == "" && c.config.IDMappings.AutoUserNs {
		if err := g.AddOrReplaceLinuxNamespace(string(spec.UserNamespace), ""); err != nil {
			return err
		}
		g.ClearLinuxUIDMappings()
		for _, uidmap := range c.config.IDMappings.UIDMap {
			g.AddLinuxUIDMapping(uint32(uidmap.HostID), uint32(uidmap.ContainerID), uint32(uidmap.Size))
		}
		g.ClearLinuxGIDMappings()
		for _, gidmap := range c.config.IDMappings.GIDMap {
			g.AddLinuxGIDMapping(uint32(gidmap.HostID), uint32(gidmap.ContainerID), uint32(gidmap.Size))
		}
	}
	return nil
}

func (c *Container) addRootPropagation(g *generate.Generator, mounts []spec.Mount) error {
	// Determine property of RootPropagation based on volume properties. If
	// a volume is shared, then keep root propagation shared. This should
	// work for slave and private volumes too.
	//
	// For slave volumes, it can be either [r]shared/[r]slave.
	//
	// For private volumes any root propagation value should work.
	rootPropagation := ""
	for _, m := range mounts {
		for _, opt := range m.Options {
			switch opt {
			case MountShared, MountRShared:
				if rootPropagation != MountShared && rootPropagation != MountRShared {
					rootPropagation = MountShared
				}
			case MountSlave, MountRSlave:
				if rootPropagation != MountShared && rootPropagation != MountRShared && rootPropagation != MountSlave && rootPropagation != MountRSlave {
					rootPropagation = MountRSlave
				}
			}
		}
	}
	if rootPropagation != "" {
		logrus.Debugf("Set root propagation to %q", rootPropagation)
		if err := g.SetLinuxRootPropagation(rootPropagation); err != nil {
			return err
		}
	}
	return nil
}

func (c *Container) setProcessLabel(g *generate.Generator) {
	g.SetProcessSelinuxLabel(c.ProcessLabel())
}

func (c *Container) setMountLabel(g *generate.Generator) {
	g.SetLinuxMountLabel(c.MountLabel())
}

func (c *Container) setCgroupsPath(g *generate.Generator) error {
	cgroupPath, err := c.getOCICgroupPath()
	if err != nil {
		return err
	}
	g.SetLinuxCgroupsPath(cgroupPath)
	return nil
}

// addSpecialDNS adds special dns servers for slirp4netns and pasta
func (c *Container) addSpecialDNS(nameservers []string) []string {
	switch {
	case c.config.NetMode.IsBridge():
		info, err := c.runtime.network.RootlessNetnsInfo()
		if err == nil && info != nil {
			nameservers = append(nameservers, info.DnsForwardIps...)
		}
	case c.pastaResult != nil:
		nameservers = append(nameservers, c.pastaResult.DNSForwardIPs...)
	case c.config.NetMode.IsSlirp4netns():
		// slirp4netns has a built in DNS forwarder.
		slirp4netnsDNS, err := slirp4netns.GetDNS(c.slirp4netnsSubnet)
		if err != nil {
			logrus.Warn("Failed to determine Slirp4netns DNS: ", err.Error())
		} else {
			nameservers = append(nameservers, slirp4netnsDNS.String())
		}
	}
	return nameservers
}

func (c *Container) isSlirp4netnsIPv6() bool {
	if c.config.NetMode.IsSlirp4netns() {
		extraOptions := c.config.NetworkOptions[slirp4netns.BinaryName]
		options := make([]string, 0, len(c.runtime.config.Engine.NetworkCmdOptions.Get())+len(extraOptions))
		options = append(options, c.runtime.config.Engine.NetworkCmdOptions.Get()...)
		options = append(options, extraOptions...)

		// loop backwards as the last argument wins and we can exit early
		// This should be kept in sync with c/common/libnetwork/slirp4netns.
		for i := len(options) - 1; i >= 0; i-- {
			switch options[i] {
			case "enable_ipv6=true":
				return true
			case "enable_ipv6=false":
				return false
			}
		}
		// default is true
		return true
	}

	return false
}

// check for net=none
func (c *Container) hasNetNone() bool {
	if !c.config.CreateNetNS {
		for _, ns := range c.config.Spec.Linux.Namespaces {
			if ns.Type == spec.NetworkNamespace {
				if ns.Path == "" {
					return true
				}
			}
		}
	}
	return false
}

func setVolumeAtime(mountPoint string, st os.FileInfo) error {
	stat := st.Sys().(*syscall.Stat_t)
	atime := time.Unix(int64(stat.Atim.Sec), int64(stat.Atim.Nsec)) //nolint: unconvert
	if err := os.Chtimes(mountPoint, atime, st.ModTime()); err != nil {
		return err
	}
	return nil
}

func (c *Container) makeHostnameBindMount() error {
	if c.config.UseImageHostname {
		return nil
	}

	// Make /etc/hostname
	// This should never change, so no need to recreate if it exists
	if _, ok := c.state.BindMounts["/etc/hostname"]; !ok {
		hostnamePath, err := c.writeStringToRundir("hostname", c.Hostname()+"\n")
		if err != nil {
			return fmt.Errorf("creating hostname file for container %s: %w", c.ID(), err)
		}
		c.state.BindMounts["/etc/hostname"] = hostnamePath
	}
	return nil
}

func (c *Container) getConmonPidFd() int {
	// Track lifetime of conmon precisely using pidfd_open + poll.
	// There are many cases for this to fail, for instance conmon is dead
	// or pidfd_open is not supported (pre linux 5.3), so fall back to the
	// traditional loop with poll + sleep
	if fd, err := unix.PidfdOpen(c.state.ConmonPID, 0); err == nil {
		return fd
	} else if err != unix.ENOSYS && err != unix.ESRCH {
		logrus.Debugf("PidfdOpen(%d) failed: %v", c.state.ConmonPID, err)
	}
	return -1
}

type safeMountInfo struct {
	// file is the open File.
	file *os.File

	// mountPoint is the mount point.
	mountPoint string
}

// Close releases the resources allocated with the safe mount info.
func (s *safeMountInfo) Close() {
	_ = unix.Unmount(s.mountPoint, unix.MNT_DETACH)
	_ = s.file.Close()
}

// safeMountSubPath securely mounts a subpath inside a volume to a new temporary location.
// The function checks that the subpath is a valid subpath within the volume and that it
// does not escape the boundaries of the mount point (volume).
//
// The caller is responsible for closing the file descriptor and unmounting the subpath
// when it's no longer needed.
func (c *Container) safeMountSubPath(mountPoint, subpath string) (s *safeMountInfo, err error) {
	file, err := pathrs.OpenInRoot(mountPoint, subpath)
	if err != nil {
		return nil, err
	}

	// we need to always reference the file by its fd, that points inside the mountpoint.
	fname := fmt.Sprintf("/proc/self/fd/%d", int(file.Fd()))

	fi, err := os.Stat(fname)
	if err != nil {
		return nil, err
	}
	var npath string
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		return nil, fmt.Errorf("file %q is a symlink", filepath.Join(mountPoint, subpath))
	case fi.IsDir():
		npath, err = os.MkdirTemp(c.state.RunDir, "subpath")
		if err != nil {
			return nil, err
		}
	default:
		tmp, err := os.CreateTemp(c.state.RunDir, "subpath")
		if err != nil {
			return nil, err
		}
		tmp.Close()
		npath = tmp.Name()
	}

	if err := unix.Mount(fname, npath, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return nil, err
	}
	return &safeMountInfo{
		file:       file,
		mountPoint: npath,
	}, nil
}

func (c *Container) makePlatformMtabLink(etcInTheContainerFd, rootUID, rootGID int) error {
	// If /etc/mtab does not exist in container image, then we need to
	// create it, so that mount command within the container will work.
	err := unix.Symlinkat("/proc/mounts", etcInTheContainerFd, "mtab")
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("creating /etc/mtab symlink: %w", err)
	}
	// If the symlink was created, then also chown it to root in the container
	if err == nil && (rootUID != 0 || rootGID != 0) {
		err = unix.Fchownat(etcInTheContainerFd, "mtab", rootUID, rootGID, unix.AT_SYMLINK_NOFOLLOW)
		if err != nil {
			return fmt.Errorf("chown /etc/mtab: %w", err)
		}
	}
	return nil
}

func (c *Container) getPlatformRunPath() (string, error) {
	return "/run", nil
}

func (c *Container) addMaskedPaths(g *generate.Generator) {
	if !c.config.Privileged && g.Config != nil && g.Config.Linux != nil && len(g.Config.Linux.MaskedPaths) > 0 {
		g.AddLinuxMaskedPaths("/sys/devices/virtual/powercap")
	}
}

func (c *Container) hasPrivateUTS() bool {
	privateUTS := false
	if c.config.Spec.Linux != nil {
		for _, ns := range c.config.Spec.Linux.Namespaces {
			if ns.Type == spec.UTSNamespace {
				privateUTS = true
				break
			}
		}
	}
	return privateUTS
}

// hasCapSysResource returns whether the current process has CAP_SYS_RESOURCE.
var hasCapSysResource = sync.OnceValues(func() (bool, error) {
	currentCaps, err := capability.NewPid2(0)
	if err != nil {
		return false, err
	}
	if err = currentCaps.Load(); err != nil {
		return false, err
	}
	return currentCaps.Get(capability.EFFECTIVE, capability.CAP_SYS_RESOURCE), nil
})

// containerPathIsFile returns true if the given containerPath is a file
func containerPathIsFile(unsafeRoot string, containerPath string) (bool, error) {
	f, err := pathrs.OpenInRoot(unsafeRoot, containerPath)
	if err != nil {
		return false, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err == nil && !st.IsDir() {
		return true, nil
	}
	return false, err
}
