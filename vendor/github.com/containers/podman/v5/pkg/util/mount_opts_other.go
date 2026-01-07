//go:build !linux

package util

func getDefaultMountOptions(_ string) (opts defaultMountOptions, err error) {
	return
}
