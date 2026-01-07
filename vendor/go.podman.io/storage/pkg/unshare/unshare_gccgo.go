//go:build linux && cgo && gccgo

package unshare

// #cgo CFLAGS: -Wall -Wextra
// extern void _containers_unshare(void);
// static void __attribute__((constructor)) init(void) {
//   _containers_unshare();
// }
import "C"

// This next bit is straight out of libcontainer.

// AlwaysFalse is here to stay false
// (and be exported so the compiler doesn't optimize out its reference)
var AlwaysFalse bool

func init() {
	if AlwaysFalse {
		// by referencing this C init() in a noop test, it will ensure the compiler
		// links in the C function.
		// https://gcc.gnu.org/bugzilla/show_bug.cgi?id=65134
		C.init()
	}
}
