//go:build !remote && !linux && !freebsd

package libpod

import (
	"errors"
)

func (r *Runtime) stopPauseProcess() error {
	return errors.New("not implemented (*Runtime) stopPauseProcess")
}
