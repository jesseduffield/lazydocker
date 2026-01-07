//go:build !linux

package overlay

func SupportsNativeOverlay(graphroot, rundir string) (bool, error) {
	return false, nil
}
