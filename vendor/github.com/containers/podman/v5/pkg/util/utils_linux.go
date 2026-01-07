package util

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/psgo"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

var (
	errNotADevice = errors.New("not a device node")
)

// GetContainerPidInformationDescriptors returns a string slice of all supported
// format descriptors of GetContainerPidInformation.
func GetContainerPidInformationDescriptors() ([]string, error) {
	return psgo.ListDescriptors(), nil
}

// FindDeviceNodes parses /dev/ into a set of major:minor -> path, where
// [major:minor] is the device's major and minor numbers formatted as, for
// example, 2:0 and path is the path to the device node.
// Symlinks to nodes are ignored.
// If onlyBlockDevices is specified, character devices are ignored.
func FindDeviceNodes(onlyBlockDevices bool) (map[string]string, error) {
	nodes := make(map[string]string)
	err := filepath.WalkDir("/dev", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				logrus.Warnf("Error descending into path %s: %v", path, err)
			}
			return filepath.SkipDir
		}

		// If we aren't a device node, do nothing.
		if d.Type()&os.ModeDevice == 0 {
			return nil
		}

		// Ignore character devices, because it is not possible to set limits on them.
		// os.ModeCharDevice is usable only when os.ModeDevice is set.
		if onlyBlockDevices && d.Type()&os.ModeCharDevice != 0 {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			// Info() can return ErrNotExist if the file was deleted between the readdir and stat call.
			// This race can happen and is no reason to log an ugly error. If this is a container device
			// that is used the code later will print a proper error in such case.
			// There also seem to be cases were ErrNotExist is always returned likely due a weird device
			// state, e.g. removing a device forcefully. This can happen with iSCSI devices.
			if !errors.Is(err, fs.ErrNotExist) {
				logrus.Errorf("Failed to get device information for %s: %v", path, err)
			}
			// return nil here as we want to continue looking for more device and not stop the WalkDir()
			return nil
		}
		// We are a device node. Get major/minor.
		sysstat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New("could not convert stat output for use")
		}
		// We must typeconvert sysstat.Rdev from uint64->int to avoid constant overflow
		rdev := int(sysstat.Rdev)
		major := ((rdev >> 8) & 0xfff) | ((rdev >> 32) & ^0xfff)
		minor := (rdev & 0xff) | ((rdev >> 12) & ^0xff)

		nodes[fmt.Sprintf("%d:%d", major, minor)] = path

		return nil
	})
	if err != nil {
		return nil, err
	}

	return nodes, nil
}

// isVirtualConsoleDevice returns true if path is a virtual console device
// (/dev/tty\d+).
// The passed path must be clean (filepath.Clean).
func isVirtualConsoleDevice(path string) bool {
	/*
		Virtual consoles are of the form `/dev/tty\d+`, any other device such as
		/dev/tty, ttyUSB0, or ttyACM0 should not be matched.
		See `man 4 console` for more information.
	*/
	suffix := strings.TrimPrefix(path, "/dev/tty")
	if suffix == path || suffix == "" {
		return false
	}

	// 16bit because, max. supported TTY devices is 512 in Linux 6.1.5.
	_, err := strconv.ParseUint(suffix, 10, 16)
	return err == nil
}

func AddPrivilegedDevices(g *generate.Generator, systemdMode bool) error {
	hostDevices, err := getDevices("/dev")
	if err != nil {
		return err
	}

	if rootless.IsRootless() {
		mounts := make(map[string]any)
		for _, m := range g.Mounts() {
			mounts[m.Destination] = true
		}
		newMounts := []spec.Mount{}
		for _, d := range hostDevices {
			devMnt := spec.Mount{
				Destination: d.Path,
				Type:        define.TypeBind,
				Source:      d.Path,
				Options:     []string{"slave", "nosuid", "noexec", "rw", "rbind"},
			}

			/* The following devices should not be mounted in rootless containers:
			 *
			 *   /dev/ptmx: The host-provided /dev/ptmx should not be shared to
			 *              the rootless containers for security reasons, and
			 *              the container runtime will create it for us
			 *              anyway (ln -s /dev/pts/ptmx /dev/ptmx);
			 *   /dev/tty and
			 *   /dev/tty[0-9]+: Prevent the container from taking over the host's
			 *                   virtual consoles, even when not in systemd mode
			 *                   for backwards compatibility.
			 */
			if d.Path == "/dev/ptmx" || d.Path == "/dev/tty" || isVirtualConsoleDevice(d.Path) {
				continue
			}
			if _, found := mounts[d.Path]; found {
				continue
			}
			newMounts = append(newMounts, devMnt)
		}
		g.Config.Mounts = append(newMounts, g.Config.Mounts...)
		if g.Config.Linux.Resources != nil {
			g.Config.Linux.Resources.Devices = nil
		}
	} else {
		for _, d := range hostDevices {
			/* Restrict access to the virtual consoles *only* when running
			 * in systemd mode to improve backwards compatibility. See
			 * https://github.com/containers/podman/issues/15878.
			 *
			 * NOTE: May need revisiting in the future to drop the systemd
			 * condition if more use cases end up breaking the virtual terminals
			 * of people who specifically disable the systemd mode. It would
			 * also provide a more consistent behaviour between rootless and
			 * rootfull containers.
			 */
			if systemdMode && isVirtualConsoleDevice(d.Path) {
				continue
			}
			g.AddDevice(d)
		}
		// Add resources device - need to clear the existing one first.
		if g.Config.Linux.Resources != nil {
			g.Config.Linux.Resources.Devices = nil
		}
		g.AddLinuxResourcesDevice(true, "", nil, nil, "rwm")
	}

	return nil
}

// based on getDevices from runc (libcontainer/devices/devices.go)
func getDevices(path string) ([]spec.LinuxDevice, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		if rootless.IsRootless() && os.IsPermission(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []spec.LinuxDevice{}
	for _, f := range files {
		switch {
		case f.IsDir():
			switch f.Name() {
			// ".lxc" & ".lxd-mounts" added to address https://github.com/lxc/lxd/issues/2825
			case "pts", "shm", "fd", "mqueue", ".lxc", ".lxd-mounts":
				continue
			default:
				sub, err := getDevices(filepath.Join(path, f.Name()))
				if err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						continue
					}
					return nil, err
				}
				if sub != nil {
					out = append(out, sub...)
				}
				continue
			}
		case f.Name() == "console":
			continue
		case f.Type()&os.ModeSymlink != 0:
			continue
		}

		device, err := DeviceFromPath(filepath.Join(path, f.Name()))
		if err != nil {
			if err == errNotADevice {
				continue
			}
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		out = append(out, *device)
	}
	return out, nil
}

// Copied from github.com/opencontainers/runc/libcontainer/devices
// Given the path to a device look up the information about a linux device
func DeviceFromPath(path string) (*spec.LinuxDevice, error) {
	var stat unix.Stat_t
	err := unix.Lstat(path, &stat)
	if err != nil {
		return nil, err
	}
	var (
		devType   string
		mode      = stat.Mode
		devNumber = uint64(stat.Rdev) //nolint: unconvert
		m         = os.FileMode(mode)
	)

	switch {
	case mode&unix.S_IFBLK == unix.S_IFBLK:
		devType = "b"
	case mode&unix.S_IFCHR == unix.S_IFCHR:
		devType = "c"
	case mode&unix.S_IFIFO == unix.S_IFIFO:
		devType = "p"
	default:
		return nil, errNotADevice
	}

	return &spec.LinuxDevice{
		Type:     devType,
		Path:     path,
		FileMode: &m,
		UID:      &stat.Uid,
		GID:      &stat.Gid,
		Major:    int64(unix.Major(devNumber)),
		Minor:    int64(unix.Minor(devNumber)),
	}, nil
}
