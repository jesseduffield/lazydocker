//go:build freebsd

package util

import (
	"github.com/opencontainers/runtime-tools/generate"
)

func GetContainerPidInformationDescriptors() ([]string, error) {
	// These are chosen to match the set of AIX format descriptors
	// supported in Linux - FreeBSD ps does support (many) others.
	return []string{
		"args",
		"comm",
		"etime",
		"group",
		"nice",
		"pcpu",
		"pgid",
		"pid",
		"ppid",
		"rgroup",
		"ruser",
		"time",
		"tty",
		"user",
		"vsz",
	}, nil
}

func AddPrivilegedDevices(_ *generate.Generator, _ bool) error {
	return nil
}
