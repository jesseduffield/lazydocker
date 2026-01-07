//go:build linux && seccomp

package seccomp

import (
	"sync"

	"golang.org/x/sys/unix"
)

var (
	supported bool
	supOnce   sync.Once
)

// IsSupported returns true if the system has been configured to support
// seccomp (including the check for CONFIG_SECCOMP_FILTER kernel option).
func IsSupported() bool {
	// Excerpts from prctl(2), section ERRORS:
	//
	// EACCES
	//	option is PR_SET_SECCOMP and arg2 is SECCOMP_MODE_FILTER, but
	//	the process does not have the CAP_SYS_ADMIN capability or has
	//	not set the no_new_privs attribute <...>.
	// <...>
	// EFAULT
	//	option is PR_SET_SECCOMP, arg2 is SECCOMP_MODE_FILTER, the
	//	system was built with CONFIG_SECCOMP_FILTER, and arg3 is an
	//	invalid address.
	// <...>
	// EINVAL
	//	option is PR_SET_SECCOMP or PR_GET_SECCOMP, and the kernel
	//	was not configured with CONFIG_SECCOMP.
	//
	// EINVAL
	//	option is PR_SET_SECCOMP, arg2 is SECCOMP_MODE_FILTER,
	//	and the kernel was not configured with CONFIG_SECCOMP_FILTER.
	// <end of quote>
	//
	// Meaning, in case these kernel options are set (this is what we check
	// for here), we will get some other error (most probably EACCES or
	// EFAULT). IOW, EINVAL means "seccomp not supported", any other error
	// means it is supported.

	supOnce.Do(func() {
		supported = unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, 0, 0, 0) != unix.EINVAL
	})
	return supported
}
