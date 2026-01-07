package util

import (
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func clockGettime(clockid int32, time *unix.Timespec) (err error) {
	_, _, e1 := unix.Syscall(unix.SYS_CLOCK_GETTIME, uintptr(clockid), uintptr(unsafe.Pointer(time)), 0)
	if e1 != 0 {
		return e1
	}
	return nil
}

func ReadUptime() (time.Duration, error) {
	tv, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return 0, err
	}
	sec, nsec := tv.Unix()
	return time.Now().Sub(time.Unix(sec, nsec)), nil
}
