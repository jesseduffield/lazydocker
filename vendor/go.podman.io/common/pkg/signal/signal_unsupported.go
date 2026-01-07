//go:build !linux

// Signal handling for Linux only.
package signal

import (
	"os"
	"syscall"
)

const (
	sigrtmin = 34
	sigrtmax = 64

	SIGWINCH = syscall.Signal(0xff)
)

// signalMap is a map of Linux signals.
// These constants are sourced from the Linux version of golang.org/x/sys/unix
// (I don't see much risk of this changing).
// This should work as long as Podman only runs containers on Linux, which seems
// a safe assumption for now.
var signalMap = map[string]syscall.Signal{
	"ABRT":     syscall.Signal(0x6),
	"ALRM":     syscall.Signal(0xe),
	"BUS":      syscall.Signal(0x7),
	"CHLD":     syscall.Signal(0x11),
	"CLD":      syscall.Signal(0x11),
	"CONT":     syscall.Signal(0x12),
	"FPE":      syscall.Signal(0x8),
	"HUP":      syscall.Signal(0x1),
	"ILL":      syscall.Signal(0x4),
	"INT":      syscall.Signal(0x2),
	"IO":       syscall.Signal(0x1d),
	"IOT":      syscall.Signal(0x6),
	"KILL":     syscall.Signal(0x9),
	"PIPE":     syscall.Signal(0xd),
	"POLL":     syscall.Signal(0x1d),
	"PROF":     syscall.Signal(0x1b),
	"PWR":      syscall.Signal(0x1e),
	"QUIT":     syscall.Signal(0x3),
	"SEGV":     syscall.Signal(0xb),
	"STKFLT":   syscall.Signal(0x10),
	"STOP":     syscall.Signal(0x13),
	"SYS":      syscall.Signal(0x1f),
	"TERM":     syscall.Signal(0xf),
	"TRAP":     syscall.Signal(0x5),
	"TSTP":     syscall.Signal(0x14),
	"TTIN":     syscall.Signal(0x15),
	"TTOU":     syscall.Signal(0x16),
	"URG":      syscall.Signal(0x17),
	"USR1":     syscall.Signal(0xa),
	"USR2":     syscall.Signal(0xc),
	"VTALRM":   syscall.Signal(0x1a),
	"WINCH":    syscall.Signal(0x1c),
	"XCPU":     syscall.Signal(0x18),
	"XFSZ":     syscall.Signal(0x19),
	"RTMIN":    sigrtmin,
	"RTMIN+1":  sigrtmin + 1,
	"RTMIN+2":  sigrtmin + 2,
	"RTMIN+3":  sigrtmin + 3,
	"RTMIN+4":  sigrtmin + 4,
	"RTMIN+5":  sigrtmin + 5,
	"RTMIN+6":  sigrtmin + 6,
	"RTMIN+7":  sigrtmin + 7,
	"RTMIN+8":  sigrtmin + 8,
	"RTMIN+9":  sigrtmin + 9,
	"RTMIN+10": sigrtmin + 10,
	"RTMIN+11": sigrtmin + 11,
	"RTMIN+12": sigrtmin + 12,
	"RTMIN+13": sigrtmin + 13,
	"RTMIN+14": sigrtmin + 14,
	"RTMIN+15": sigrtmin + 15,
	"RTMAX-14": sigrtmax - 14,
	"RTMAX-13": sigrtmax - 13,
	"RTMAX-12": sigrtmax - 12,
	"RTMAX-11": sigrtmax - 11,
	"RTMAX-10": sigrtmax - 10,
	"RTMAX-9":  sigrtmax - 9,
	"RTMAX-8":  sigrtmax - 8,
	"RTMAX-7":  sigrtmax - 7,
	"RTMAX-6":  sigrtmax - 6,
	"RTMAX-5":  sigrtmax - 5,
	"RTMAX-4":  sigrtmax - 4,
	"RTMAX-3":  sigrtmax - 3,
	"RTMAX-2":  sigrtmax - 2,
	"RTMAX-1":  sigrtmax - 1,
	"RTMAX":    sigrtmax,
}

// CatchAll catches all signals and relays them to the specified channel.
func CatchAll(sigc chan os.Signal) {
	panic("Unsupported on non-linux platforms")
}

// StopCatch stops catching the signals and closes the specified channel.
func StopCatch(sigc chan os.Signal) {
	panic("Unsupported on non-linux platforms")
}
