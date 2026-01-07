//go:build solaris

package cwriter

import "golang.org/x/sys/unix"

const ioctlReadTermios = unix.TCGETA
