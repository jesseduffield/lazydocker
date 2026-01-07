//go:build !remote

package libpod

func checkCgroups2UnifiedMode(_ *Runtime) {
}

func (r *Runtime) checkBootID(_ string) error {
	return nil
}
