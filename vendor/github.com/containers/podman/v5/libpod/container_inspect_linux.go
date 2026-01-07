//go:build !remote

package libpod

import (
	"fmt"
	"sort"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/util"
	"github.com/moby/sys/capability"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/config"
	"go.podman.io/storage/types"
)

func (c *Container) platformInspectContainerHostConfig(ctrSpec *spec.Spec, hostConfig *define.InspectContainerHostConfig) error {
	// This is very expensive to initialize.
	// So we don't want to initialize it unless we absolutely have to - IE,
	// there are things that require a major:minor to path translation.
	var deviceNodes map[string]string

	if ctrSpec.Linux != nil {
		if ctrSpec.Linux.IntelRdt != nil {
			if ctrSpec.Linux.IntelRdt.ClosID != "" {
				// container is assigned to a ClosID
				hostConfig.IntelRdtClosID = ctrSpec.Linux.IntelRdt.ClosID
			}
		}
		// Resource limits
		if ctrSpec.Linux.Resources != nil {
			if ctrSpec.Linux.Resources.CPU != nil {
				if ctrSpec.Linux.Resources.CPU.Shares != nil {
					hostConfig.CpuShares = *ctrSpec.Linux.Resources.CPU.Shares
				}
				if ctrSpec.Linux.Resources.CPU.Period != nil {
					hostConfig.CpuPeriod = *ctrSpec.Linux.Resources.CPU.Period
				}
				if ctrSpec.Linux.Resources.CPU.Quota != nil {
					hostConfig.CpuQuota = *ctrSpec.Linux.Resources.CPU.Quota
				}
				if ctrSpec.Linux.Resources.CPU.RealtimePeriod != nil {
					hostConfig.CpuRealtimePeriod = *ctrSpec.Linux.Resources.CPU.RealtimePeriod
				}
				if ctrSpec.Linux.Resources.CPU.RealtimeRuntime != nil {
					hostConfig.CpuRealtimeRuntime = *ctrSpec.Linux.Resources.CPU.RealtimeRuntime
				}
				hostConfig.CpusetCpus = ctrSpec.Linux.Resources.CPU.Cpus
				hostConfig.CpusetMems = ctrSpec.Linux.Resources.CPU.Mems
			}
			if ctrSpec.Linux.Resources.Memory != nil {
				if ctrSpec.Linux.Resources.Memory.Limit != nil {
					hostConfig.Memory = *ctrSpec.Linux.Resources.Memory.Limit
				}
				if ctrSpec.Linux.Resources.Memory.Reservation != nil {
					hostConfig.MemoryReservation = *ctrSpec.Linux.Resources.Memory.Reservation
				}
				if ctrSpec.Linux.Resources.Memory.Swap != nil {
					hostConfig.MemorySwap = *ctrSpec.Linux.Resources.Memory.Swap
				}
				if ctrSpec.Linux.Resources.Memory.Swappiness != nil {
					hostConfig.MemorySwappiness = int64(*ctrSpec.Linux.Resources.Memory.Swappiness)
				} else {
					// Swappiness has a default of -1
					hostConfig.MemorySwappiness = -1
				}
				if ctrSpec.Linux.Resources.Memory.DisableOOMKiller != nil {
					hostConfig.OomKillDisable = *ctrSpec.Linux.Resources.Memory.DisableOOMKiller
				}
			}
			if ctrSpec.Linux.Resources.Pids != nil {
				hostConfig.PidsLimit = ctrSpec.Linux.Resources.Pids.Limit
			}
			hostConfig.CgroupConf = ctrSpec.Linux.Resources.Unified
			if ctrSpec.Linux.Resources.BlockIO != nil {
				if ctrSpec.Linux.Resources.BlockIO.Weight != nil {
					hostConfig.BlkioWeight = *ctrSpec.Linux.Resources.BlockIO.Weight
				}
				hostConfig.BlkioWeightDevice = []define.InspectBlkioWeightDevice{}
				for _, dev := range ctrSpec.Linux.Resources.BlockIO.WeightDevice {
					key := fmt.Sprintf("%d:%d", dev.Major, dev.Minor)
					// TODO: how do we handle LeafWeight vs
					// Weight? For now, ignore anything
					// without Weight set.
					if dev.Weight == nil {
						logrus.Infof("Ignoring weight device %s as it lacks a weight", key)
						continue
					}
					if deviceNodes == nil {
						nodes, err := util.FindDeviceNodes(true)
						if err != nil {
							return err
						}
						deviceNodes = nodes
					}
					path, ok := deviceNodes[key]
					if !ok {
						logrus.Infof("Could not locate weight device %s in system devices", key)
						continue
					}
					weightDev := define.InspectBlkioWeightDevice{}
					weightDev.Path = path
					weightDev.Weight = *dev.Weight
					hostConfig.BlkioWeightDevice = append(hostConfig.BlkioWeightDevice, weightDev)
				}

				readBps, err := blkioDeviceThrottle(deviceNodes, ctrSpec.Linux.Resources.BlockIO.ThrottleReadBpsDevice)
				if err != nil {
					return err
				}
				hostConfig.BlkioDeviceReadBps = readBps

				writeBps, err := blkioDeviceThrottle(deviceNodes, ctrSpec.Linux.Resources.BlockIO.ThrottleWriteBpsDevice)
				if err != nil {
					return err
				}
				hostConfig.BlkioDeviceWriteBps = writeBps

				readIops, err := blkioDeviceThrottle(deviceNodes, ctrSpec.Linux.Resources.BlockIO.ThrottleReadIOPSDevice)
				if err != nil {
					return err
				}
				hostConfig.BlkioDeviceReadIOps = readIops

				writeIops, err := blkioDeviceThrottle(deviceNodes, ctrSpec.Linux.Resources.BlockIO.ThrottleWriteIOPSDevice)
				if err != nil {
					return err
				}
				hostConfig.BlkioDeviceWriteIOps = writeIops
			}
		}
	}

	// Cap add and cap drop.
	// We need a default set of capabilities to compare against.
	// The OCI generate package has one, and is commonly used, so we'll
	// use it.
	// Problem: there are 5 sets of capabilities.
	// Use the bounding set for this computation, it's the most encompassing
	// (but still not perfect).
	capAdd := []string{}
	capDrop := []string{}
	// No point in continuing if we got a spec without a Process block...
	if ctrSpec.Process != nil {
		// Max an O(1) lookup table for default bounding caps.
		boundingCaps := make(map[string]bool)
		if !hostConfig.Privileged {
			for _, cap := range c.runtime.config.Containers.DefaultCapabilities.Get() {
				boundingCaps[cap] = true
			}
		} else {
			// If we are privileged, use all caps.
			for _, cap := range capability.ListKnown() {
				boundingCaps[fmt.Sprintf("CAP_%s", strings.ToUpper(cap.String()))] = true
			}
		}
		// Iterate through default caps.
		// If it's not in default bounding caps, it was added.
		// If it is, delete from the default set. Whatever remains after
		// we finish are the dropped caps.
		for _, cap := range ctrSpec.Process.Capabilities.Bounding {
			if _, ok := boundingCaps[cap]; ok {
				delete(boundingCaps, cap)
			} else {
				capAdd = append(capAdd, cap)
			}
		}
		for cap := range boundingCaps {
			capDrop = append(capDrop, cap)
		}
		// Sort CapDrop so it displays in consistent order (GH #9490)
		sort.Strings(capDrop)
	}
	hostConfig.CapAdd = capAdd
	hostConfig.CapDrop = capDrop
	switch {
	case c.config.IPCNsCtr != "":
		hostConfig.IpcMode = fmt.Sprintf("container:%s", c.config.IPCNsCtr)
	case ctrSpec.Linux != nil:
		// Locate the spec's IPC namespace.
		// If there is none, it's ipc=host.
		// If there is one and it has a path, it's "ns:".
		// If no path, it's default - the empty string.
		hostConfig.IpcMode = "host"
		for _, ns := range ctrSpec.Linux.Namespaces {
			if ns.Type == spec.IPCNamespace {
				if ns.Path != "" {
					hostConfig.IpcMode = fmt.Sprintf("ns:%s", ns.Path)
				} else {
					switch {
					case c.config.NoShm:
						hostConfig.IpcMode = "none"
					case c.config.NoShmShare:
						hostConfig.IpcMode = "private"
					default:
						hostConfig.IpcMode = "shareable"
					}
				}
				break
			}
		}
	case c.config.NoShm:
		hostConfig.IpcMode = "none"
	case c.config.NoShmShare:
		hostConfig.IpcMode = "private"
	}

	// Cgroup namespace mode
	cgroupMode := ""
	if c.config.CgroupNsCtr != "" {
		cgroupMode = fmt.Sprintf("container:%s", c.config.CgroupNsCtr)
	} else if ctrSpec.Linux != nil {
		// Locate the spec's cgroup namespace
		// If there is none, it's cgroup=host.
		// If there is one and it has a path, it's "ns:".
		// If there is no path, it's private.
		for _, ns := range ctrSpec.Linux.Namespaces {
			if ns.Type == spec.CgroupNamespace {
				if ns.Path != "" {
					cgroupMode = fmt.Sprintf("ns:%s", ns.Path)
				} else {
					cgroupMode = "private"
				}
			}
		}
		if cgroupMode == "" {
			cgroupMode = "host"
		}
	}
	hostConfig.CgroupMode = cgroupMode

	// Cgroup parent
	// Need to check if it's the default, and not print if so.
	defaultCgroupParent := ""
	switch c.CgroupManager() {
	case config.CgroupfsCgroupsManager:
		defaultCgroupParent = CgroupfsDefaultCgroupParent
	case config.SystemdCgroupsManager:
		defaultCgroupParent = SystemdDefaultCgroupParent
	}
	if c.config.CgroupParent != defaultCgroupParent {
		hostConfig.CgroupParent = c.config.CgroupParent
	}
	hostConfig.CgroupManager = c.CgroupManager()

	// PID namespace mode
	pidMode := ""
	if c.config.PIDNsCtr != "" {
		pidMode = fmt.Sprintf("container:%s", c.config.PIDNsCtr)
	} else if ctrSpec.Linux != nil {
		// Locate the spec's PID namespace.
		// If there is none, it's pid=host.
		// If there is one and it has a path, it's "ns:".
		// If there is no path, it's default - the empty string.
		for _, ns := range ctrSpec.Linux.Namespaces {
			if ns.Type == spec.PIDNamespace {
				if ns.Path != "" {
					pidMode = fmt.Sprintf("ns:%s", ns.Path)
				} else {
					pidMode = "private"
				}
				break
			}
		}
		if pidMode == "" {
			pidMode = "host"
		}
	}
	hostConfig.PidMode = pidMode

	// UTS namespace mode
	utsMode := c.NamespaceMode(spec.UTSNamespace, ctrSpec)

	hostConfig.UTSMode = utsMode

	// User namespace mode
	usernsMode := ""
	if c.config.UserNsCtr != "" {
		usernsMode = fmt.Sprintf("container:%s", c.config.UserNsCtr)
	} else if ctrSpec.Linux != nil {
		// Locate the spec's user namespace.
		// If there is none, it's default - the empty string.
		// If there is one, it's "private" if no path, or "ns:" if
		// there's a path.

		for _, ns := range ctrSpec.Linux.Namespaces {
			if ns.Type == spec.UserNamespace {
				if ns.Path != "" {
					usernsMode = fmt.Sprintf("ns:%s", ns.Path)
				} else {
					usernsMode = "private"
				}
			}
		}
	}
	hostConfig.UsernsMode = usernsMode
	if c.config.IDMappings.UIDMap != nil && c.config.IDMappings.GIDMap != nil {
		hostConfig.IDMappings = generateIDMappings(c.config.IDMappings)
	}
	// Devices
	// Do not include if privileged - assumed that all devices will be
	// included.
	var err error
	hostConfig.Devices, err = c.GetDevices(hostConfig.Privileged, *ctrSpec, deviceNodes)
	if err != nil {
		return err
	}

	return nil
}

func generateIDMappings(idMappings types.IDMappingOptions) *define.InspectIDMappings {
	var inspectMappings define.InspectIDMappings
	for _, uid := range idMappings.UIDMap {
		inspectMappings.UIDMap = append(inspectMappings.UIDMap, fmt.Sprintf("%d:%d:%d", uid.ContainerID, uid.HostID, uid.Size))
	}
	for _, gid := range idMappings.GIDMap {
		inspectMappings.GIDMap = append(inspectMappings.GIDMap, fmt.Sprintf("%d:%d:%d", gid.ContainerID, gid.HostID, gid.Size))
	}
	return &inspectMappings
}

// Return true if the container is running in the host's PID NS.
func (c *Container) inHostPidNS() (bool, error) {
	if c.config.PIDNsCtr != "" {
		return false, nil
	}
	ctrSpec, err := c.specFromState()
	if err != nil {
		return false, err
	}
	if ctrSpec.Linux != nil {
		// Locate the spec's PID namespace.
		// If there is none, it's pid=host.
		// If there is one and it has a path, it's "ns:".
		// If there is no path, it's default - the empty string.
		for _, ns := range ctrSpec.Linux.Namespaces {
			if ns.Type == spec.PIDNamespace {
				return false, nil
			}
		}
	}
	return true, nil
}
