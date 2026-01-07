//go:build !remote

package libpod

func (r *Runtime) stopPauseProcess() error {
	return nil
}
