//go:build !linux && !darwin

package copier

import (
	"os"
)

type hardlinkChecker struct{}

func (h *hardlinkChecker) Check(fi os.FileInfo) string {
	return ""
}

func (h *hardlinkChecker) Add(fi os.FileInfo, name string) {
}
