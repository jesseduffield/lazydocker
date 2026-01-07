package fsverity

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// verityDigest struct represents the digest used for verifying the integrity of a file.
type verityDigest struct {
	Fsv unix.FsverityDigest
	Buf [64]byte
}

// EnableVerity enables the verity feature on a file represented by the file descriptor 'fd'.  The file must be opened
// in read-only mode.
// The 'description' parameter is a human-readable description of the file.
func EnableVerity(description string, fd int) error {
	enableArg := unix.FsverityEnableArg{
		Version:        1,
		Hash_algorithm: unix.FS_VERITY_HASH_ALG_SHA256,
		Block_size:     4096,
	}

	_, _, e1 := syscall.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.FS_IOC_ENABLE_VERITY), uintptr(unsafe.Pointer(&enableArg)))
	if e1 != 0 && !errors.Is(e1, unix.EEXIST) {
		return fmt.Errorf("failed to enable verity for %q: %w", description, e1)
	}
	return nil
}

// MeasureVerity measures and returns the verity digest for the file represented by 'fd'.
// The 'description' parameter is a human-readable description of the file.
func MeasureVerity(description string, fd int) (string, error) {
	var digest verityDigest
	digest.Fsv.Size = 64
	_, _, e1 := syscall.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.FS_IOC_MEASURE_VERITY), uintptr(unsafe.Pointer(&digest)))
	if e1 != 0 {
		return "", fmt.Errorf("failed to measure verity for %q: %w", description, e1)
	}
	return fmt.Sprintf("%x", digest.Buf[:digest.Fsv.Size]), nil
}
