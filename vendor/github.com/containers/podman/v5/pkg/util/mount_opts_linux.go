package util

import (
	"os"

	"golang.org/x/sys/unix"
)

func getDefaultMountOptions(path string) (defaultMountOptions, error) {
	opts := defaultMountOptions{false, true, true}
	if path == "" {
		return opts, nil
	}
	var statfs unix.Statfs_t
	if e := unix.Statfs(path, &statfs); e != nil {
		return opts, &os.PathError{Op: "statfs", Path: path, Err: e}
	}
	opts.nodev = (statfs.Flags&unix.MS_NODEV == unix.MS_NODEV)
	opts.noexec = (statfs.Flags&unix.MS_NOEXEC == unix.MS_NOEXEC)
	opts.nosuid = (statfs.Flags&unix.MS_NOSUID == unix.MS_NOSUID)

	return opts, nil
}
