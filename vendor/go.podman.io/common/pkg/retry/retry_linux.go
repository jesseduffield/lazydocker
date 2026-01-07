package retry

import (
	"syscall"
)

func isErrnoERESTART(e error) bool {
	return e == syscall.ERESTART
}
