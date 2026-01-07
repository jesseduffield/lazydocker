//go:build linux

package cgroups

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opencontainers/cgroups"
	"github.com/opencontainers/cgroups/fs"
	"github.com/opencontainers/cgroups/fs2"
)

type linuxBlkioHandler struct {
	Blkio fs.BlkioGroup
}

func getBlkioHandler() *linuxBlkioHandler {
	return &linuxBlkioHandler{}
}

// Apply set the specified constraints.
func (c *linuxBlkioHandler) Apply(ctr *CgroupControl, res *cgroups.Resources) error {
	if ctr.cgroup2 {
		man, err := fs2.NewManager(ctr.config, filepath.Join(cgroupRoot, ctr.config.Path))
		if err != nil {
			return err
		}
		return man.Set(res)
	}
	path := filepath.Join(cgroupRoot, Blkio, ctr.config.Path)
	return c.Blkio.Set(path, res)
}

// Create the cgroup.
func (c *linuxBlkioHandler) Create(ctr *CgroupControl) (bool, error) {
	if ctr.cgroup2 {
		return false, nil
	}
	return ctr.createCgroupDirectory(Blkio)
}

// Destroy the cgroup.
func (c *linuxBlkioHandler) Destroy(ctr *CgroupControl) error {
	return rmDirRecursively(ctr.getCgroupv1Path(Blkio))
}

// Stat fills a metrics structure with usage stats for the controller.
func (c *linuxBlkioHandler) Stat(ctr *CgroupControl, m *cgroups.Stats) error {
	var ioServiceBytesRecursive []cgroups.BlkioStatEntry

	if ctr.cgroup2 {
		// more details on the io.stat file format:X https://facebookmicrosites.github.io/cgroup2/docs/io-controller.html
		values, err := readCgroup2MapFile(ctr, "io.stat")
		if err != nil {
			return err
		}
		for k, v := range values {
			d := strings.Split(k, ":")
			if len(d) != 2 {
				continue
			}
			minor, err := strconv.ParseUint(d[0], 10, 0)
			if err != nil {
				return err
			}
			major, err := strconv.ParseUint(d[1], 10, 0)
			if err != nil {
				return err
			}

			for _, item := range v {
				d := strings.Split(item, "=")
				if len(d) != 2 {
					continue
				}
				op := d[0]

				// Accommodate the cgroup v1 naming
				switch op {
				case "rbytes":
					op = "read"
				case "wbytes":
					op = "write"
				}

				value, err := strconv.ParseUint(d[1], 10, 0)
				if err != nil {
					return err
				}

				entry := cgroups.BlkioStatEntry{
					Op:    op,
					Major: major,
					Minor: minor,
					Value: value,
				}
				ioServiceBytesRecursive = append(ioServiceBytesRecursive, entry)
			}
		}
	} else {
		BlkioRoot := ctr.getCgroupv1Path(Blkio)

		p := filepath.Join(BlkioRoot, "blkio.throttle.io_service_bytes_recursive")
		f, err := os.Open(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("open %s: %w", p, err)
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.Fields(line)
			if len(parts) < 3 {
				continue
			}
			d := strings.Split(parts[0], ":")
			if len(d) != 2 {
				continue
			}
			minor, err := strconv.ParseUint(d[0], 10, 0)
			if err != nil {
				return err
			}
			major, err := strconv.ParseUint(d[1], 10, 0)
			if err != nil {
				return err
			}

			op := parts[1]

			value, err := strconv.ParseUint(parts[2], 10, 0)
			if err != nil {
				return err
			}
			entry := cgroups.BlkioStatEntry{
				Op:    op,
				Major: major,
				Minor: minor,
				Value: value,
			}
			ioServiceBytesRecursive = append(ioServiceBytesRecursive, entry)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
	}
	m.BlkioStats.IoServiceBytesRecursive = ioServiceBytesRecursive
	return nil
}
