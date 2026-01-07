//go:build (linux && !mips && !mipsle && !mips64 && !mips64le) || freebsd || netbsd

package copier

import (
	"syscall"
)

func makeHardlinkDeviceAndInode(st *syscall.Stat_t) hardlinkDeviceAndInode {
	return hardlinkDeviceAndInode{
		device: st.Dev,
		inode:  st.Ino,
	}
}
