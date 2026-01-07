//go:build plan9
// +build plan9

package sftp

import (
	"syscall"
)

func fakeFileInfoSys() interface{} {
	return &syscall.Dir{}
}

func testOsSys(sys interface{}) error {
	return nil
}
