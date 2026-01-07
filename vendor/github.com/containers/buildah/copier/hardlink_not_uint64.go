//go:build darwin || (linux && mips) || (linux && mipsle) || (linux && mips64) || (linux && mips64le)

package copier

import (
	"syscall"
)

func makeHardlinkDeviceAndInode(st *syscall.Stat_t) hardlinkDeviceAndInode {
	return hardlinkDeviceAndInode{
		device: uint64(st.Dev),
		inode:  uint64(st.Ino),
	}
}
