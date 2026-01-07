//go:build linux && !cgo

package unshare

import (
	"os"
)

func getenv(name string) string {
	return os.Getenv(name)
}
