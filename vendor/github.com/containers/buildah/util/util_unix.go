//go:build linux || darwin || freebsd || netbsd

package util

import (
	"os"
	"syscall"
)

func UID(st os.FileInfo) int {
	return int(st.Sys().(*syscall.Stat_t).Uid)
}

func GID(st os.FileInfo) int {
	return int(st.Sys().(*syscall.Stat_t).Gid)
}
