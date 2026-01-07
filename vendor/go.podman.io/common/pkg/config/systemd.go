//go:build systemd && cgo

package config

import (
	"os"
	"path/filepath"
	"sync"

	"go.podman.io/common/pkg/cgroupv2"
	"go.podman.io/common/pkg/systemd"
	"go.podman.io/storage/pkg/unshare"
)

var (
	journaldOnce sync.Once
	usesJournald bool
)

const (
	// DefaultLogDriver is the default type of log files.
	DefaultLogDriver = "journald"
)

func defaultCgroupManager() string {
	if !useSystemd() {
		return CgroupfsCgroupsManager
	}
	enabled, err := cgroupv2.Enabled()
	if err == nil && !enabled && unshare.IsRootless() {
		return CgroupfsCgroupsManager
	}

	return SystemdCgroupsManager
}

func defaultEventsLogger() string {
	if useJournald() {
		return "journald"
	}
	return "file"
}

func defaultLogDriver() string {
	if useJournald() {
		return "journald"
	}
	return "k8s-file"
}

func useSystemd() bool {
	return systemd.RunsOnSystemd()
}

func useJournald() bool {
	journaldOnce.Do(func() {
		if !useSystemd() {
			return
		}
		for _, root := range []string{"/run/log/journal", "/var/log/journal"} {
			dirs, err := os.ReadDir(root)
			if err != nil {
				continue
			}
			for _, d := range dirs {
				if d.IsDir() {
					if _, err := os.ReadDir(filepath.Join(root, d.Name())); err == nil {
						usesJournald = true
						return
					}
				}
			}
		}
	})
	return usesJournald
}
