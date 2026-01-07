//go:build !linux || !cgo

package copy //nolint: predeclared

import (
	"io"
	"os"

	"go.podman.io/storage/pkg/chrootarchive"
)

// Mode indicates whether to use hardlink or copy content
type Mode int

const (
	// Content creates a new file, and copies the content of the file
	Content Mode = iota
)

// DirCopy copies or hardlinks the contents of one directory to another,
// properly handling soft links
func DirCopy(srcDir, dstDir string, _ Mode, _ bool) error {
	return chrootarchive.NewArchiver(nil).CopyWithTar(srcDir, dstDir)
}

// CopyRegularToFile copies the content of a file to another
func CopyRegularToFile(srcPath string, dstFile *os.File, fileinfo os.FileInfo, copyWithFileRange, copyWithFileClone *bool) error { //nolint: revive // "func name will be used as copy.CopyRegularToFile by other packages, and that stutters"
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(dstFile, f)
	return err
}

// CopyRegular copies the content of a file to another
func CopyRegular(srcPath, dstPath string, fileinfo os.FileInfo, copyWithFileRange, copyWithFileClone *bool) error { //nolint:revive // "func name will be used as copy.CopyRegular by other packages, and that stutters"
	return chrootarchive.NewArchiver(nil).CopyWithTar(srcPath, dstPath)
}
