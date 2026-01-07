//go:build linux && cgo

package unshare

import (
	"unsafe"
)

/*
#cgo remoteclient CFLAGS: -Wall -Werror
#include <stdlib.h>
*/
import "C"

func getenv(name string) string {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	value := C.GoString(C.getenv(cName))

	return value
}
