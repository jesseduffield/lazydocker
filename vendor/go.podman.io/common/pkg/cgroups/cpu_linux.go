//go:build linux

package cgroups

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/opencontainers/cgroups"
	"github.com/opencontainers/cgroups/fs"
	"github.com/opencontainers/cgroups/fs2"
)

type linuxCPUHandler struct {
	CPU fs.CpuGroup
}

func getCPUHandler() *linuxCPUHandler {
	return &linuxCPUHandler{}
}

// Apply set the specified constraints.
func (c *linuxCPUHandler) Apply(ctr *CgroupControl, res *cgroups.Resources) error {
	if ctr.cgroup2 {
		man, err := fs2.NewManager(ctr.config, filepath.Join(cgroupRoot, ctr.config.Path))
		if err != nil {
			return err
		}
		return man.Set(res)
	}
	path := filepath.Join(cgroupRoot, CPU, ctr.config.Path)
	return c.CPU.Set(path, res)
}

// Create the cgroup.
func (c *linuxCPUHandler) Create(ctr *CgroupControl) (bool, error) {
	if ctr.cgroup2 {
		return false, nil
	}
	return ctr.createCgroupDirectory(CPU)
}

// Destroy the cgroup.
func (c *linuxCPUHandler) Destroy(ctr *CgroupControl) error {
	return rmDirRecursively(ctr.getCgroupv1Path(CPU))
}

// Stat fills a metrics structure with usage stats for the controller.
func (c *linuxCPUHandler) Stat(ctr *CgroupControl, m *cgroups.Stats) error {
	var err error
	cpu := cgroups.CpuStats{}
	if ctr.cgroup2 {
		values, err := readCgroup2MapFile(ctr, "cpu.stat")
		if err != nil {
			return err
		}
		if val, found := values["usage_usec"]; found {
			cpu.CpuUsage.TotalUsage, err = strconv.ParseUint(cleanString(val[0]), 10, 64)
			if err != nil {
				return err
			}
			cpu.CpuUsage.UsageInKernelmode *= 1000
		}
		if val, found := values["system_usec"]; found {
			cpu.CpuUsage.UsageInKernelmode, err = strconv.ParseUint(cleanString(val[0]), 10, 64)
			if err != nil {
				return err
			}
			cpu.CpuUsage.TotalUsage *= 1000
		}
	} else {
		cpu.CpuUsage.TotalUsage, err = readAcct(ctr, "cpuacct.usage")
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			cpu.CpuUsage.TotalUsage = 0
		}
		cpu.CpuUsage.UsageInKernelmode, err = readAcct(ctr, "cpuacct.usage_sys")
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			cpu.CpuUsage.UsageInKernelmode = 0
		}
		cpu.CpuUsage.PercpuUsage, err = readAcctList(ctr, "cpuacct.usage_percpu")
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			cpu.CpuUsage.PercpuUsage = nil
		}
	}
	m.CpuStats = cpu
	return nil
}
