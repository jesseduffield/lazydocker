//go:build !linux

package vfs // import "go.podman.io/storage/drivers/vfs"

import "go.podman.io/storage/pkg/chrootarchive"

func dirCopy(srcDir, dstDir string) error {
	return chrootarchive.NewArchiver(nil).CopyWithTar(srcDir, dstDir)
}
