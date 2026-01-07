//go:build !linux && !windows && !solaris && !(freebsd && cgo)

package system

// ReadMemInfo is not supported on platforms other than linux and windows.
func ReadMemInfo() (*MemInfo, error) {
	return nil, ErrNotSupportedPlatform
}
