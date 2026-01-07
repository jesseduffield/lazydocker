//go:build (linux && cgo && !gccgo) || (freebsd && cgo)

package unshare

// #cgo CFLAGS: -Wall
// extern void _containers_unshare(void);
// static void __attribute__((constructor)) init(void) {
//   _containers_unshare();
// }
import "C"
