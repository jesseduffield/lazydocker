//go:build !linux

package binfmt

import "syscall"

// MaybeRegister() returns no error.
func MaybeRegister(configurationSearchDirectories []string) error {
	return nil
}

// Register() returns an error.
func Register(configurationSearchDirectories []string) error {
	return syscall.ENOSYS
}
