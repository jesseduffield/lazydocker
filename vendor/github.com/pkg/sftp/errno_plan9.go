package sftp

import (
	"os"
	"syscall"
)

var EBADF = syscall.NewError("fd out of range or not open")

func wrapPathError(filepath string, err error) error {
	if errno, ok := err.(syscall.ErrorString); ok {
		return &os.PathError{Path: filepath, Err: errno}
	}
	return err
}

// translateErrno translates a syscall error number to a SFTP error code.
func translateErrno(errno syscall.ErrorString) uint32 {
	switch errno {
	case "":
		return sshFxOk
	case syscall.ENOENT:
		return sshFxNoSuchFile
	case syscall.EPERM:
		return sshFxPermissionDenied
	}

	return sshFxFailure
}

func translateSyscallError(err error) (uint32, bool) {
	switch e := err.(type) {
	case syscall.ErrorString:
		return translateErrno(e), true
	case *os.PathError:
		debug("statusFromError,pathError: error is %T %#v", e.Err, e.Err)
		if errno, ok := e.Err.(syscall.ErrorString); ok {
			return translateErrno(errno), true
		}
	}
	return 0, false
}
