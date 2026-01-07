package systemd

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strconv"
	"sync"

	systemdDbus "github.com/coreos/go-systemd/v22/dbus"
	"github.com/godbus/dbus/v5"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/cgroups"
	"go.podman.io/storage/pkg/unshare"
)

var (
	runsOnSystemdOnce sync.Once
	runsOnSystemd     bool
)

// RunsOnSystemd returns whether the system is using systemd.
func RunsOnSystemd() bool {
	runsOnSystemdOnce.Do(func() {
		// per sd_booted(3), check for this dir
		fd, err := os.Stat("/run/systemd/system")
		runsOnSystemd = err == nil && fd.IsDir()
	})
	return runsOnSystemd
}

func moveProcessPIDFileToScope(pidPath, slice, scope string) error {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		// do not raise an error if the file doesn't exist
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("cannot read pid file: %w", err)
	}
	pid, err := strconv.ParseUint(string(data), 10, 0)
	if err != nil {
		return fmt.Errorf("cannot parse pid file %s: %w", pidPath, err)
	}

	return moveProcessToScope(int(pid), slice, scope)
}

func moveProcessToScope(pid int, slice, scope string) error {
	err := RunUnderSystemdScope(pid, slice, scope)
	// If the PID is not valid anymore, do not return an error.
	if dbusErr, ok := err.(dbus.Error); ok {
		if dbusErr.Name == "org.freedesktop.DBus.Error.UnixProcessIdUnknown" {
			return nil
		}
	}
	return err
}

// MoveRootlessNetnsSlirpProcessToUserSlice moves the slirp4netns process for the rootless netns
// into a different scope so that systemd does not kill it with a container.
func MoveRootlessNetnsSlirpProcessToUserSlice(pid int) error {
	randBytes := make([]byte, 4)
	_, err := rand.Read(randBytes)
	if err != nil {
		return err
	}
	return moveProcessToScope(pid, "user.slice", fmt.Sprintf("rootless-netns-%x.scope", randBytes))
}

// MovePauseProcessToScope moves the pause process used for rootless mode to keep the namespaces alive to
// a separate scope.
func MovePauseProcessToScope(pausePidPath string) {
	var err error

	for range 10 {
		randBytes := make([]byte, 4)
		_, err = rand.Read(randBytes)
		if err != nil {
			logrus.Errorf("failed to read random bytes: %v", err)
			continue
		}
		err = moveProcessPIDFileToScope(pausePidPath, "user.slice", fmt.Sprintf("podman-pause-%x.scope", randBytes))
		if err == nil {
			return
		}
	}

	if err != nil {
		unified, err2 := cgroups.IsCgroup2UnifiedMode()
		if err2 != nil {
			logrus.Warnf("Failed to detect if running with cgroup unified: %v", err)
		}
		if RunsOnSystemd() && unified {
			logrus.Warnf("Failed to add pause process to systemd sandbox cgroup: %v", err)
		} else {
			logrus.Debugf("Failed to add pause process to systemd sandbox cgroup: %v", err)
		}
	}
}

// RunUnderSystemdScope adds the specified pid to a systemd scope.
func RunUnderSystemdScope(pid int, slice string, unitName string) error {
	var properties []systemdDbus.Property
	var conn *systemdDbus.Conn
	var err error

	if unshare.GetRootlessUID() != 0 {
		conn, err = cgroups.UserConnection(unshare.GetRootlessUID())
		if err != nil {
			return err
		}
	} else {
		conn, err = systemdDbus.NewWithContext(context.Background())
		if err != nil {
			return err
		}
	}
	defer conn.Close()
	properties = append(properties, systemdDbus.PropSlice(slice))
	properties = append(properties, newProp("PIDs", []uint32{uint32(pid)}))
	properties = append(properties, newProp("Delegate", true))
	properties = append(properties, newProp("DefaultDependencies", false))
	ch := make(chan string)
	_, err = conn.StartTransientUnitContext(context.Background(), unitName, "replace", properties, ch)
	if err != nil {
		// On errors check if the cgroup already exists, if it does move the process there
		if props, err := conn.GetUnitTypePropertiesContext(context.Background(), unitName, "Scope"); err == nil {
			if cgroup, ok := props["ControlGroup"].(string); ok && cgroup != "" {
				if err := cgroups.MoveUnderCgroup(cgroup, "", []uint32{uint32(pid)}); err == nil {
					return nil
				}
				// On errors return the original error message we got from StartTransientUnit.
			}
		}
		return err
	}

	// Block until job is started
	<-ch

	return nil
}

func newProp(name string, units any) systemdDbus.Property {
	return systemdDbus.Property{
		Name:  name,
		Value: dbus.MakeVariant(units),
	}
}
