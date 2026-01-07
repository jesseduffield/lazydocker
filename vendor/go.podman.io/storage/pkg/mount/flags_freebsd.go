package mount

import (
	"golang.org/x/sys/unix"
)

const (
	// RDONLY will mount the file system read-only.
	RDONLY = unix.MNT_RDONLY

	// NOSUID will not allow set-user-identifier or set-group-identifier bits to
	// take effect.
	NOSUID = unix.MNT_NOSUID

	// NOEXEC will not allow execution of any binaries on the mounted file system.
	NOEXEC = unix.MNT_NOEXEC

	// SYNCHRONOUS will allow I/O to the file system to be done synchronously.
	SYNCHRONOUS = unix.MNT_SYNCHRONOUS

	// REMOUNT will attempt to remount an already-mounted file system. This is
	// commonly used to change the mount flags for a file system, especially to
	// make a readonly file system writeable. It does not change device or mount
	// point.
	REMOUNT = unix.MNT_UPDATE

	// NOATIME will not update the file access time when reading from a file.
	NOATIME = unix.MNT_NOATIME

	mntDetach = unix.MNT_FORCE

	NODIRATIME  = 0
	NODEV       = 0
	DIRSYNC     = 0
	MANDLOCK    = 0
	BIND        = 0
	RBIND       = 0
	UNBINDABLE  = 0
	RUNBINDABLE = 0
	PRIVATE     = 0
	RPRIVATE    = 0
	SLAVE       = 0
	RSLAVE      = 0
	SHARED      = 0
	RSHARED     = 0
	RELATIME    = 0
	STRICTATIME = 0
)
