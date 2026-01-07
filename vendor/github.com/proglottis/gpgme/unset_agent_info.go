//go:build !windows
// +build !windows

package gpgme

// #include <stdlib.h>
import "C"
import (
	"unsafe"
)

// This is somewhat of a horrible hack. We need to unset GPG_AGENT_INFO so that gpgme does not pass --use-agent to GPG.
// os.Unsetenv should be enough, but that only calls the underlying C library (which gpgme uses) if cgo is involved
// - and cgo can't be used in tests. So, provide this helper for test initialization.
func unsetenvGPGAgentInfo() {
	v := C.CString("GPG_AGENT_INFO")
	defer C.free(unsafe.Pointer(v))
	C.unsetenv(v)
}
