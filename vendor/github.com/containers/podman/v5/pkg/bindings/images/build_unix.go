//go:build !windows

package images

import (
	"os"
	"syscall"
)

func checkHardLink(fi os.FileInfo) (devino, bool) {
	st := fi.Sys().(*syscall.Stat_t)
	return devino{
		Dev: uint64(st.Dev), //nolint:unconvert,nolintlint
		Ino: st.Ino,
	}, st.Nlink > 1
}
