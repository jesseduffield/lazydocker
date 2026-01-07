//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package cwriter

import "golang.org/x/sys/unix"

const ioctlReadTermios = unix.TIOCGETA
