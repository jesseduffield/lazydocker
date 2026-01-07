//go:build linux

package ctime

import (
	"os"
	"syscall"
	"time"
)

func created(fi os.FileInfo) time.Time {
	st := fi.Sys().(*syscall.Stat_t)
	//nolint:unconvert // need to type cast on some cpu architectures
	return time.Unix(int64(st.Ctim.Sec), int64(st.Ctim.Nsec))
}
