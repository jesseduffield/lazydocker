//go:build windows

package rawfilelock

import (
	"golang.org/x/sys/windows"
)

const (
	reserved = 0
	allBytes = ^uint32(0)
)

type fileHandle windows.Handle

func openHandle(path string, mode int) (fileHandle, error) {
	mode |= windows.O_CLOEXEC
	fd, err := windows.Open(path, mode, windows.S_IWRITE)
	return fileHandle(fd), err
}

func lockHandle(fd fileHandle, lType LockType, nonblocking bool) error {
	flags := 0
	if lType != ReadLock {
		flags = windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	if nonblocking {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(fd), uint32(flags), reserved, allBytes, allBytes, ol); err != nil {
		if nonblocking {
			return err
		}
		panic(err)
	}
	return nil
}

func unlockAndCloseHandle(fd fileHandle) {
	ol := new(windows.Overlapped)
	windows.UnlockFileEx(windows.Handle(fd), reserved, allBytes, allBytes, ol)
	closeHandle(fd)
}

func closeHandle(fd fileHandle) {
	windows.Close(windows.Handle(fd))
}
