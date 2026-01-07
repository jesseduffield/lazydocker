package util

import (
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// For some reason, unix.ClockGettime isn't implemented by x/sys/unix on FreeBSD
func clockGettime(clockid int32, time *unix.Timespec) (err error) {
	_, _, e1 := unix.Syscall(unix.SYS_CLOCK_GETTIME, uintptr(clockid), uintptr(unsafe.Pointer(time)), 0)
	if e1 != 0 {
		return e1
	}
	return nil
}

func ReadUptime() (time.Duration, error) {
	var uptime unix.Timespec
	if err := clockGettime(unix.CLOCK_UPTIME, &uptime); err != nil {
		return 0, err
	}
	return time.Duration(unix.TimespecToNsec(uptime)), nil
}
