//go:build windows

package graphdriver

import (
	"os"
	"syscall"

	"go.podman.io/storage/pkg/idtools"
)

type platformChowner struct{}

func newLChowner() *platformChowner {
	return &platformChowner{}
}

func (c *platformChowner) LChown(path string, info os.FileInfo, toHost, toContainer *idtools.IDMappings) error {
	return &os.PathError{"lchown", path, syscall.EWINDOWS}
}
