package graphdriver

import (
	"golang.org/x/sys/unix"

	"go.podman.io/storage/pkg/mount"
)

const (
	// FsMagicZfs filesystem id for Zfs
	FsMagicZfs = FsMagic(0x2fc12fc1)
)

var (
	// Slice of drivers that should be used in an order
	Priority = []string{
		"zfs",
		"vfs",
	}

	// FsNames maps filesystem id to name of the filesystem.
	FsNames = map[FsMagic]string{
		FsMagicZfs: "zfs",
	}
)

// NewDefaultChecker returns a check that parses /proc/mountinfo to check
// if the specified path is mounted.
// No-op on FreeBSD.
func NewDefaultChecker() Checker {
	return &defaultChecker{}
}

type defaultChecker struct{}

func (c *defaultChecker) IsMounted(path string) bool {
	m, _ := mount.Mounted(path)
	return m
}

// Mounted checks if the given path is mounted as the fs type
func Mounted(fsType FsMagic, mountPath string) (bool, error) {
	var buf unix.Statfs_t
	if err := unix.Statfs(mountPath, &buf); err != nil {
		return false, err
	}
	return FsMagic(buf.Type) == fsType, nil
}
