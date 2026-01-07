//go:build linux

package cgroups

import (
	"path/filepath"

	"github.com/opencontainers/cgroups"
	"github.com/opencontainers/cgroups/fs"
	"github.com/opencontainers/cgroups/fs2"
)

type linuxPidHandler struct {
	Pid fs.PidsGroup
}

func getPidsHandler() *linuxPidHandler {
	return &linuxPidHandler{}
}

// Apply set the specified constraints.
func (c *linuxPidHandler) Apply(ctr *CgroupControl, res *cgroups.Resources) error {
	if ctr.cgroup2 {
		man, err := fs2.NewManager(ctr.config, filepath.Join(cgroupRoot, ctr.config.Path))
		if err != nil {
			return err
		}
		return man.Set(res)
	}

	path := filepath.Join(cgroupRoot, Pids, ctr.config.Path)
	return c.Pid.Set(path, res)
}

// Create the cgroup.
func (c *linuxPidHandler) Create(ctr *CgroupControl) (bool, error) {
	if ctr.cgroup2 {
		return false, nil
	}
	return ctr.createCgroupDirectory(Pids)
}

// Destroy the cgroup.
func (c *linuxPidHandler) Destroy(ctr *CgroupControl) error {
	return rmDirRecursively(ctr.getCgroupv1Path(Pids))
}

// Stat fills a metrics structure with usage stats for the controller.
func (c *linuxPidHandler) Stat(ctr *CgroupControl, m *cgroups.Stats) error {
	if ctr.config.Path == "" {
		// nothing we can do to retrieve the pids.current path
		return nil
	}

	var PIDRoot string
	if ctr.cgroup2 {
		PIDRoot = filepath.Join(cgroupRoot, ctr.config.Path)
	} else {
		PIDRoot = ctr.getCgroupv1Path(Pids)
	}

	current, err := readFileAsUint64(filepath.Join(PIDRoot, "pids.current"))
	if err != nil {
		return err
	}

	m.PidsStats.Current = current
	return nil
}
