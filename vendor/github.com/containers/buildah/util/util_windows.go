//go:build !linux && !darwin

package util

import (
	"os"
)

func UID(st os.FileInfo) int {
	return 0
}

func GID(st os.FileInfo) int {
	return 0
}
