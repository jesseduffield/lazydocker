//go:build cgo && !(linux || freebsd)

package unshare

// Go refuses to compile a subpackage with CGO_ENABLED=1 if there is a *.c file but no 'import "C"'.
// OTOH if we did have an 'import "C"', the Linux-only code would fail to compile.
// So, satisfy the Go compiler by using import "C" but #ifdef-ing out all of the code.

// #cgo CPPFLAGS: -DUNSHARE_NO_CODE_AT_ALL
import "C"
