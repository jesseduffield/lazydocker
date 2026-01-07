//go:build !remote

package libpod

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/containers/podman/v5/pkg/errorhandling"
	"github.com/containers/podman/v5/pkg/rootless"
	runcconfig "github.com/opencontainers/cgroups"
	devices "github.com/opencontainers/cgroups/devices/config"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/selinux/go-selinux"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/cgroups"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/systemd"
	pmount "go.podman.io/storage/pkg/mount"
	"golang.org/x/sys/unix"
)

func (r *ConmonOCIRuntime) createRootlessContainer(ctr *Container, restoreOptions *ContainerCheckpointOptions, hideFiles bool) (int64, error) {
	type result struct {
		restoreDuration int64
		err             error
	}
	ch := make(chan result)
	go func() {
		runtime.LockOSThread()
		restoreDuration, err := func() (int64, error) {
			fd, err := os.Open(fmt.Sprintf("/proc/%d/task/%d/ns/mnt", os.Getpid(), unix.Gettid()))
			if err != nil {
				return 0, err
			}
			defer errorhandling.CloseQuiet(fd)

			rootPath, err := ctr.getRootPathForOCI()
			if err != nil {
				return 0, err
			}

			// create a new mountns on the current thread
			if err = unix.Unshare(unix.CLONE_NEWNS); err != nil {
				return 0, err
			}
			defer func() {
				err := unix.Setns(int(fd.Fd()), unix.CLONE_NEWNS)
				if err == nil {
					// If we are able to reset the previous mount namespace, unlock the thread and reuse it
					runtime.UnlockOSThread()
				} else {
					// otherwise, leave the thread locked and the Go runtime will terminate it
					logrus.Errorf("Unable to reset the previous mount namespace: %q", err)
				}
			}()

			getMounts := sync.OnceValues(pmount.GetMounts)

			// bind mount the containers' mount path to the path where the OCI runtime expects it to be
			// if the container is already mounted at the expected path, do not cover the mountpoint.
			if rootPath != "" && filepath.Clean(ctr.state.Mountpoint) != filepath.Clean(rootPath) {
				mounts, err := getMounts()
				if err != nil {
					return 0, err
				}

				byMountpoint := make(map[string]*pmount.Info)
				for _, m := range mounts {
					byMountpoint[m.Mountpoint] = m
				}
				isShared := false
				var parentMount string
				for dir := filepath.Dir(rootPath); ; dir = filepath.Dir(dir) {
					if m, found := byMountpoint[dir]; found {
						//nolint:staticcheck // false positive?! it claims the value of parentMount is not used but we use it below for the mount
						parentMount = dir
						for o := range strings.SplitSeq(m.Optional, ",") {
							opt := strings.Split(o, ":")
							if opt[0] == "shared" {
								isShared = true
								break
							}
						}
						break
					}
					if dir == "/" {
						return 0, fmt.Errorf("cannot find mountpoint for the root path")
					}
				}

				// do not propagate the bind mount on the parent mount namespace
				if err := unix.Mount("", parentMount, "", unix.MS_SLAVE, ""); err != nil {
					return 0, fmt.Errorf("failed to make %s slave: %w", parentMount, err)
				}
				if err := unix.Mount(ctr.state.Mountpoint, rootPath, "", unix.MS_BIND, ""); err != nil {
					return 0, fmt.Errorf("failed to bind mount %s to %s: %w", ctr.state.Mountpoint, rootPath, err)
				}
				if isShared {
					// we need to restore the shared propagation of the parent mount so that we don't break -v $SRC:$DST:shared in the container
					// if $SRC is on the same mount as the root path
					if err := unix.Mount("", parentMount, "", unix.MS_SHARED, ""); err != nil {
						return 0, fmt.Errorf("failed to restore MS_SHARED propagation for %s: %w", parentMount, err)
					}
				}
			}

			if hideFiles {
				// don't spread our mounts around.  We are setting only /sys to be slave
				// so that the cleanup process is still able to umount the storage and the
				// changes are propagated to the host.
				err = unix.Mount("/sys", "/sys", "none", unix.MS_REC|unix.MS_SLAVE, "")
				if err != nil {
					return 0, fmt.Errorf("cannot make /sys slave: %w", err)
				}
				mounts, err := getMounts()
				if err != nil {
					return 0, err
				}
				for _, m := range mounts {
					if !strings.HasPrefix(m.Mountpoint, "/sys/kernel") {
						continue
					}
					err = unix.Unmount(m.Mountpoint, 0)
					if err != nil && !errors.Is(err, fs.ErrNotExist) {
						return 0, fmt.Errorf("cannot unmount %s: %w", m.Mountpoint, err)
					}
				}
			}
			return r.createOCIContainer(ctr, restoreOptions)
		}()
		ch <- result{
			restoreDuration: restoreDuration,
			err:             err,
		}
	}()
	res := <-ch
	return res.restoreDuration, res.err
}

// Run the closure with the container's socket label set
func (r *ConmonOCIRuntime) withContainerSocketLabel(ctr *Container, closure func() error) error {
	runtime.LockOSThread()
	if err := selinux.SetSocketLabel(ctr.ProcessLabel()); err != nil {
		return err
	}
	err := closure()
	// Ignore error returned from SetSocketLabel("") call,
	// can't recover.
	if labelErr := selinux.SetSocketLabel(""); labelErr == nil {
		// Unlock the thread only if the process label could be restored
		// successfully.  Otherwise leave the thread locked and the Go runtime
		// will terminate it once it returns to the threads pool.
		runtime.UnlockOSThread()
	} else {
		logrus.Errorf("Unable to reset socket label: %q", labelErr)
	}
	return err
}

// Create systemd unit name for cgroup scopes.
func createUnitName(prefix string, name string) string {
	return fmt.Sprintf("%s-%s.scope", prefix, name)
}

// moveConmonToCgroupAndSignal gets a container's cgroupParent and moves the conmon process to that cgroup
// it then signals for conmon to start by sending nonce data down the start fd
func (r *ConmonOCIRuntime) moveConmonToCgroupAndSignal(ctr *Container, cmd *exec.Cmd, startFd *os.File) error {
	mustCreateCgroup := !ctr.config.NoCgroups

	// If cgroup creation is disabled - just signal.
	switch ctr.config.CgroupsMode {
	case "disabled", "no-conmon", cgroupSplit:
		mustCreateCgroup = false
	}

	// $INVOCATION_ID is set by systemd when running as a service.
	if ctr.runtime.RemoteURI() == "" && os.Getenv("INVOCATION_ID") != "" {
		mustCreateCgroup = false
	}

	if mustCreateCgroup {
		// Usually rootless users are not allowed to configure cgroupfs.
		// There are cases though, where it is allowed, e.g. if the cgroup
		// is manually configured and chowned).  Avoid detecting all
		// such cases and simply use a lower log level.
		logLevel := logrus.WarnLevel
		if rootless.IsRootless() {
			logLevel = logrus.InfoLevel
		}
		// TODO: This should be a switch - we are not guaranteed that
		// there are only 2 valid cgroup managers
		cgroupParent := ctr.CgroupParent()
		cgroupPath := filepath.Join(ctr.config.CgroupParent, "conmon")
		cgroupResources, err := GetLimits(ctr.LinuxResources())
		if err != nil {
			logrus.StandardLogger().Log(logLevel, "Could not get ctr resources")
		}
		if ctr.CgroupManager() == config.SystemdCgroupsManager {
			unitName := createUnitName("libpod-conmon", ctr.ID())
			realCgroupParent := cgroupParent
			splitParent := strings.Split(cgroupParent, "/")
			if strings.HasSuffix(cgroupParent, ".slice") && len(splitParent) > 1 {
				realCgroupParent = splitParent[len(splitParent)-1]
			}

			logrus.Infof("Running conmon under slice %s and unitName %s", realCgroupParent, unitName)
			if err := systemd.RunUnderSystemdScope(cmd.Process.Pid, realCgroupParent, unitName); err != nil {
				logrus.StandardLogger().Logf(logLevel, "Failed to add conmon to systemd sandbox cgroup: %v", err)
			}
		} else {
			control, err := cgroups.New(cgroupPath, &cgroupResources)
			if err != nil {
				logrus.StandardLogger().Logf(logLevel, "Failed to add conmon to cgroupfs sandbox cgroup: %v", err)
			} else if err := control.AddPid(cmd.Process.Pid); err != nil {
				// we need to remove this defer and delete the cgroup once conmon exits
				// maybe need a conmon monitor?
				logrus.StandardLogger().Logf(logLevel, "Failed to add conmon to cgroupfs sandbox cgroup: %v", err)
			}
		}
	}

	/* We set the cgroup, now the child can start creating children */
	return writeConmonPipeData(startFd)
}

// GetLimits converts spec resource limits to cgroup consumable limits
func GetLimits(resource *spec.LinuxResources) (runcconfig.Resources, error) {
	if resource == nil {
		resource = &spec.LinuxResources{}
	}
	final := &runcconfig.Resources{}
	devs := []*devices.Rule{}

	// Devices
	for _, entry := range resource.Devices {
		if entry.Major == nil || entry.Minor == nil {
			continue
		}
		runeType := 'a'
		switch entry.Type {
		case "b":
			runeType = 'b'
		case "c":
			runeType = 'c'
		}

		devs = append(devs, &devices.Rule{
			Type:        devices.Type(runeType),
			Major:       *entry.Major,
			Minor:       *entry.Minor,
			Permissions: devices.Permissions(entry.Access),
			Allow:       entry.Allow,
		})
	}
	final.Devices = devs

	// HugepageLimits
	pageLimits := []*runcconfig.HugepageLimit{}
	for _, entry := range resource.HugepageLimits {
		pageLimits = append(pageLimits, &runcconfig.HugepageLimit{
			Pagesize: entry.Pagesize,
			Limit:    entry.Limit,
		})
	}
	final.HugetlbLimit = pageLimits

	// Networking
	netPriorities := []*runcconfig.IfPrioMap{}
	if resource.Network != nil {
		for _, entry := range resource.Network.Priorities {
			netPriorities = append(netPriorities, &runcconfig.IfPrioMap{
				Interface: entry.Name,
				Priority:  int64(entry.Priority),
			})
		}
	}
	final.NetPrioIfpriomap = netPriorities
	rdma := make(map[string]runcconfig.LinuxRdma)
	for name, entry := range resource.Rdma {
		rdma[name] = runcconfig.LinuxRdma{HcaHandles: entry.HcaHandles, HcaObjects: entry.HcaObjects}
	}
	final.Rdma = rdma

	// Memory
	if resource.Memory != nil {
		if resource.Memory.Limit != nil {
			final.Memory = *resource.Memory.Limit
		}
		if resource.Memory.Reservation != nil {
			final.MemoryReservation = *resource.Memory.Reservation
		}
		if resource.Memory.Swap != nil {
			final.MemorySwap = *resource.Memory.Swap
		}
		if resource.Memory.Swappiness != nil {
			final.MemorySwappiness = resource.Memory.Swappiness
		}
	}

	// CPU
	if resource.CPU != nil {
		if resource.CPU.Period != nil {
			final.CpuPeriod = *resource.CPU.Period
		}
		if resource.CPU.Quota != nil {
			final.CpuQuota = *resource.CPU.Quota
		}
		if resource.CPU.RealtimePeriod != nil {
			final.CpuRtPeriod = *resource.CPU.RealtimePeriod
		}
		if resource.CPU.RealtimeRuntime != nil {
			final.CpuRtRuntime = *resource.CPU.RealtimeRuntime
		}
		if resource.CPU.Shares != nil {
			final.CpuShares = *resource.CPU.Shares
		}
		final.CpusetCpus = resource.CPU.Cpus
		final.CpusetMems = resource.CPU.Mems
	}

	// BlkIO
	if resource.BlockIO != nil {
		if len(resource.BlockIO.ThrottleReadBpsDevice) > 0 {
			for _, entry := range resource.BlockIO.ThrottleReadBpsDevice {
				throttle := runcconfig.NewThrottleDevice(entry.Major, entry.Minor, entry.Rate)
				final.BlkioThrottleReadBpsDevice = append(final.BlkioThrottleReadBpsDevice, throttle)
			}
		}
		if len(resource.BlockIO.ThrottleWriteBpsDevice) > 0 {
			for _, entry := range resource.BlockIO.ThrottleWriteBpsDevice {
				throttle := runcconfig.NewThrottleDevice(entry.Major, entry.Minor, entry.Rate)
				final.BlkioThrottleWriteBpsDevice = append(final.BlkioThrottleWriteBpsDevice, throttle)
			}
		}
		if len(resource.BlockIO.ThrottleReadIOPSDevice) > 0 {
			for _, entry := range resource.BlockIO.ThrottleReadIOPSDevice {
				throttle := runcconfig.NewThrottleDevice(entry.Major, entry.Minor, entry.Rate)
				final.BlkioThrottleReadIOPSDevice = append(final.BlkioThrottleReadIOPSDevice, throttle)
			}
		}
		if len(resource.BlockIO.ThrottleWriteIOPSDevice) > 0 {
			for _, entry := range resource.BlockIO.ThrottleWriteIOPSDevice {
				throttle := runcconfig.NewThrottleDevice(entry.Major, entry.Minor, entry.Rate)
				final.BlkioThrottleWriteIOPSDevice = append(final.BlkioThrottleWriteIOPSDevice, throttle)
			}
		}
		if resource.BlockIO.LeafWeight != nil {
			final.BlkioLeafWeight = *resource.BlockIO.LeafWeight
		}
		if resource.BlockIO.Weight != nil {
			final.BlkioWeight = *resource.BlockIO.Weight
		}
		if len(resource.BlockIO.WeightDevice) > 0 {
			for _, entry := range resource.BlockIO.WeightDevice {
				var w, lw uint16
				if entry.Weight != nil {
					w = *entry.Weight
				}
				if entry.LeafWeight != nil {
					lw = *entry.LeafWeight
				}
				weight := runcconfig.NewWeightDevice(entry.Major, entry.Minor, w, lw)
				final.BlkioWeightDevice = append(final.BlkioWeightDevice, weight)
			}
		}
	}

	// Pids
	if resource.Pids != nil {
		final.PidsLimit = resource.Pids.Limit
	}

	// Networking
	if resource.Network != nil {
		if resource.Network.ClassID != nil {
			final.NetClsClassid = *resource.Network.ClassID
		}
	}

	// Unified state
	final.Unified = resource.Unified
	return *final, nil
}

func moveToRuntimeCgroup() error {
	return cgroups.MoveUnderCgroupSubtree("runtime")
}
