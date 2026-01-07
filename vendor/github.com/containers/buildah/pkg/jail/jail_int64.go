//go:build !(386 || arm) && freebsd
// +build !386,!arm,freebsd

package jail

import (
	"syscall"
)

func stringToIovec(val string) (syscall.Iovec, error) {
	bs, err := syscall.ByteSliceFromString(val)
	if err != nil {
		return syscall.Iovec{}, err
	}
	var res syscall.Iovec
	res.Base = &bs[0]
	res.Len = uint64(len(bs))
	return res, nil
}
