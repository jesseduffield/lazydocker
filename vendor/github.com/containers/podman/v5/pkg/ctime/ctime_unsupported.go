//go:build !linux

package ctime

import (
	"os"
	"time"
)

func created(fi os.FileInfo) time.Time {
	return fi.ModTime()
}
