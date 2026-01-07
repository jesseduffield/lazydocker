package vfs

import "go.podman.io/storage/drivers/copy"

func dirCopy(srcDir, dstDir string) error {
	return copy.DirCopy(srcDir, dstDir, copy.Content, true)
}
