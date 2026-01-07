//go:build !linux

package ioutils

import (
	"os"
)

func dataOrFullSync(f *os.File) error {
	return f.Sync()
}

func (w *atomicFileWriter) postDataWrittenSync() error {
	// many platforms (Mac, Windows) require a full sync to reliably flush to media
	return nil
}

func (w *atomicFileWriter) preRenameSync() error {
	if w.noSync {
		return nil
	}

	// fsync() on Non-linux Unix, FlushFileBuffers (Windows), F_FULLFSYNC (Mac)
	return w.f.Sync()
}
