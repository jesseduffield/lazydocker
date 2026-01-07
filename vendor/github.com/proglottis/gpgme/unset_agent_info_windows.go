package gpgme

// #include <stdlib.h>
import "C"
import (
	"unsafe"
)

// unsetenv is not available in mingw
func unsetenvGPGAgentInfo() {
	v := C.CString("GPG_AGENT_INFO=")
	defer C.free(unsafe.Pointer(v))
	C.putenv(v)
}
