//go:build !remote

package libpod

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/containers/podman/v5/pkg/rootless"
	securejoin "github.com/cyphar/filepath-securejoin"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/types"
	"golang.org/x/sys/unix"
)

var (
	bindOptions = []string{}
)

func (c *Container) mountSHM(_ string) error {
	return nil
}

func (c *Container) unmountSHM(_ string) error {
	return nil
}

// prepare mounts the container and sets up other required resources like net
// namespaces
func (c *Container) prepare() error {
	var (
		wg                              sync.WaitGroup
		ctrNS                           string
		networkStatus                   map[string]types.StatusBlock
		createNetNSErr, mountStorageErr error
		mountPoint                      string
		tmpStateLock                    sync.Mutex
	)

	wg.Add(2)

	go func() {
		defer wg.Done()
		// Set up network namespace if not already set up
		noNetNS := c.state.NetNS == ""
		if c.config.CreateNetNS && noNetNS && !c.config.PostConfigureNetNS {
			c.reservedPorts, createNetNSErr = c.bindPorts()
			if createNetNSErr != nil {
				return
			}
			ctrNS, networkStatus, createNetNSErr = c.runtime.createNetNS(c)
			if createNetNSErr != nil {
				return
			}

			tmpStateLock.Lock()
			defer tmpStateLock.Unlock()

			// Assign NetNS attributes to container
			c.state.NetNS = ctrNS
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
	if createErr != nil {
		if mountStorageErr == nil {
			if err := c.cleanupStorage(); err != nil {
				// createErr is guaranteed non-nil, so print
				// unconditionally
				logrus.Errorf("Preparing container %s: %v", c.ID(), createErr)
				createErr = fmt.Errorf("unmounting storage for container %s after network create failure: %w", c.ID(), err)
			}
		}
		// It's OK to unconditionally trigger network cleanup. If the network
		// isn't ready it will do nothing.
		if err := c.cleanupNetwork(); err != nil {
			logrus.Errorf("Preparing container %s: %v", c.ID(), createErr)
			createErr = fmt.Errorf("cleaning up container %s network after setup failure: %w", c.ID(), err)
		}
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

	// Stop the container's network namespace (if it has one)
	neterr := c.runtime.teardownNetNS(c)

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

// Add an existing container's network jail
func (c *Container) addNetworkContainer(g *generate.Generator, ctr string) error {
	nsCtr, err := c.runtime.state.Container(ctr)
	if err != nil {
		return fmt.Errorf("retrieving dependency %s of container %s from state: %w", ctr, c.ID(), err)
	}
	if err := c.runtime.state.UpdateContainer(nsCtr); err != nil {
		return err
	}
	if nsCtr.state.NetNS != "" {
		g.AddAnnotation("org.freebsd.parentJail", nsCtr.state.NetNS)
	}
	return nil
}

func isRootlessCgroupSet(_ string) bool {
	return false
}

func (c *Container) expectPodCgroup() (bool, error) {
	return false, nil
}

func (c *Container) getOCICgroupPath() (string, error) {
	return "", nil
}

func openDirectory(path string) (fd int, err error) {
	const O_PATH = 0x00400000
	return unix.Open(path, unix.O_RDONLY|O_PATH|unix.O_CLOEXEC, 0)
}

func (c *Container) addNetworkNamespace(g *generate.Generator) error {
	if c.config.CreateNetNS {
		// If PostConfigureNetNS is set (which is true on FreeBSD 13.3
		// and later), we can manage a container's network settings
		// without an extra parent jail to own the vnew.
		//
		// In this case, the OCI runtime creates a new vnet for the
		// container jail, otherwise it creates the container jail as a
		// child of the jail owning the vnet.
		if c.config.PostConfigureNetNS {
			g.AddAnnotation("org.freebsd.jail.vnet", "new")
		} else {
			g.AddAnnotation("org.freebsd.parentJail", c.state.NetNS)
		}
	}
	return nil
}

func (c *Container) addSystemdMounts(_ *generate.Generator) error {
	return nil
}

func (c *Container) addSharedNamespaces(g *generate.Generator) error {
	if c.config.NetNsCtr != "" {
		if err := c.addNetworkContainer(g, c.config.NetNsCtr); err != nil {
			return err
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

	// TODO: make this optional, needs progress on adding FreeBSD section to the spec
	foundUTS := true
	g.SetHostname(hostname)

	if !foundUTS {
		tmpHostname, err := os.Hostname()
		if err != nil {
			return err
		}
		hostname = tmpHostname
	}
	needEnv := true
	for _, checkEnv := range g.Config.Process.Env {
		if strings.HasPrefix(checkEnv, "HOSTNAME=") {
			needEnv = false
			break
		}
	}
	if needEnv {
		g.AddProcessEnv("HOSTNAME", hostname)
	}
	return nil
}

func (c *Container) addRootPropagation(_ *generate.Generator, _ []spec.Mount) error {
	return nil
}

func (c *Container) setProcessLabel(_ *generate.Generator) {
}

func (c *Container) setMountLabel(_ *generate.Generator) {
}

func (c *Container) setCgroupsPath(_ *generate.Generator) error {
	return nil
}

func (c *Container) addSpecialDNS(nameservers []string) []string {
	return nameservers
}

func (c *Container) isSlirp4netnsIPv6() bool {
	return false
}

// check for net=none
func (c *Container) hasNetNone() bool {
	return c.state.NetNS == ""
}

func setVolumeAtime(mountPoint string, st os.FileInfo) error {
	stat := st.Sys().(*syscall.Stat_t)
	atime := time.Unix(int64(stat.Atimespec.Sec), int64(stat.Atimespec.Nsec)) //nolint: unconvert
	if err := os.Chtimes(mountPoint, atime, st.ModTime()); err != nil {
		return err
	}
	return nil
}

func (c *Container) makeHostnameBindMount() error {
	return nil
}

func (c *Container) getConmonPidFd() int {
	// Note: kqueue(2) could be used here but that would require
	// factoring out the call to unix.PollFd from WaitForExit so
	// keeping things simple for now.
	return -1
}

func (c *Container) jailName() (string, error) {
	// If this container is in a pod, get the vnet name from the
	// corresponding infra container
	var ic *Container
	if c.config.Pod != "" && c.config.Pod != c.ID() {
		// Get the pod from state
		pod, err := c.runtime.state.Pod(c.config.Pod)
		if err != nil {
			return "", fmt.Errorf("cannot find infra container for pod %s: %w", c.config.Pod, err)
		}
		ic, err = pod.InfraContainer()
		if err != nil {
			return "", fmt.Errorf("getting infra container for pod %s: %w", pod.ID(), err)
		}
		if ic.ID() != c.ID() {
			ic.lock.Lock()
			defer ic.lock.Unlock()
			if err := ic.syncContainer(); err != nil {
				return "", err
			}
		}
	} else {
		ic = c
	}

	if ic.state.NetNS != "" && ic != c {
		return ic.state.NetNS + "." + c.ID(), nil
	} else {
		return c.ID(), nil
	}
}

type safeMountInfo struct {
	// mountPoint is the mount point.
	mountPoint string
}

// Close releases the resources allocated with the safe mount info.
func (s *safeMountInfo) Close() {
}

// safeMountSubPath securely mounts a subpath inside a volume to a new temporary location.
// The function checks that the subpath is a valid subpath within the volume and that it
// does not escape the boundaries of the mount point (volume).
//
// The caller is responsible for closing the file descriptor and unmounting the subpath
// when it's no longer needed.
func (c *Container) safeMountSubPath(mountPoint, subpath string) (s *safeMountInfo, err error) {
	return &safeMountInfo{mountPoint: filepath.Join(mountPoint, subpath)}, nil
}

func (c *Container) makePlatformMtabLink(_, _, _ int) error {
	// /etc/mtab does not exist on FreeBSD
	return nil
}

func (c *Container) getPlatformRunPath() (string, error) {
	// If we have a linux image, use "/run", otherwise use "/var/run" for
	// consistency with FreeBSD path conventions.
	runPath := "/var/run"
	if c.config.RootfsImageID != "" {
		image, _, err := c.runtime.libimageRuntime.LookupImage(c.config.RootfsImageID, nil)
		if err != nil {
			return "", err
		}
		inspectData, err := image.Inspect(context.TODO(), nil)
		if err != nil {
			return "", err
		}
		if inspectData.Os == "linux" {
			runPath = "/run"
		}
	}
	return runPath, nil
}

func (c *Container) addMaskedPaths(_ *generate.Generator) {
	// There are currently no FreeBSD-specific masked paths
}

func (c *Container) hasPrivateUTS() bool {
	// Currently we always use a private UTS namespace on FreeBSD. This
	// should be optional but needs a FreeBSD section in the OCI runtime
	// specification.
	return true
}

// hasCapSysResource returns whether the current process has CAP_SYS_RESOURCE.
func hasCapSysResource() (bool, error) {
	return true, nil
}

// containerPathIsFile returns true if the given containerPath is a file
func containerPathIsFile(unsafeRoot string, containerPath string) (bool, error) {
	// Note freebsd does not have support for OpenInRoot() so us the less safe way
	// with the old SecureJoin(), but given this is only called before the container
	// is started it is not subject to race conditions with the container process.
	path, err := securejoin.SecureJoin(unsafeRoot, containerPath)
	if err != nil {
		return false, err
	}

	st, err := os.Lstat(path)
	if err == nil && !st.IsDir() {
		return true, nil
	}
	return false, err
}
