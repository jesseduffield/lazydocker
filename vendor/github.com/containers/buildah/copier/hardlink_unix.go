//go:build !windows

package copier

import (
	"os"
	"sync"
	"syscall"
)

type hardlinkDeviceAndInode struct {
	device, inode uint64
}

type hardlinkChecker struct {
	hardlinks sync.Map
}

func (h *hardlinkChecker) Check(fi os.FileInfo) string {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && fi.Mode().IsRegular() && st.Nlink > 1 {
		if name, ok := h.hardlinks.Load(makeHardlinkDeviceAndInode(st)); ok && name.(string) != "" {
			return name.(string)
		}
	}
	return ""
}

func (h *hardlinkChecker) Add(fi os.FileInfo, name string) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && fi.Mode().IsRegular() && st.Nlink > 1 {
		h.hardlinks.Store(makeHardlinkDeviceAndInode(st), name)
	}
}
