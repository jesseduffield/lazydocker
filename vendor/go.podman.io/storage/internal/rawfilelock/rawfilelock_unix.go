//go:build !windows

package rawfilelock

import (
	"time"

	"golang.org/x/sys/unix"
)

type fileHandle uintptr

func openHandle(path string, mode int) (fileHandle, error) {
	mode |= unix.O_CLOEXEC
	fd, err := unix.Open(path, mode, 0o644)
	return fileHandle(fd), err
}

func lockHandle(fd fileHandle, lType LockType, nonblocking bool) error {
	fType := unix.F_RDLCK
	if lType != ReadLock {
		fType = unix.F_WRLCK
	}
	lk := unix.Flock_t{
		Type:   int16(fType),
		Whence: int16(unix.SEEK_SET),
		Start:  0,
		Len:    0,
	}
	cmd := unix.F_SETLKW
	if nonblocking {
		cmd = unix.F_SETLK
	}
	for {
		err := unix.FcntlFlock(uintptr(fd), cmd, &lk)
		if err == nil || nonblocking {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func unlockAndCloseHandle(fd fileHandle) {
	unix.Close(int(fd))
}

func closeHandle(fd fileHandle) {
	unix.Close(int(fd))
}
