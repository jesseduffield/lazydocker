package ioutils

import (
	"os"

	"golang.org/x/sys/unix"
)

func dataOrFullSync(f *os.File) error {
	return unix.Fdatasync(int(f.Fd()))
}

func (w *atomicFileWriter) postDataWrittenSync() error {
	if w.noSync {
		return nil
	}
	return unix.Fdatasync(int(w.f.Fd()))
}

func (w *atomicFileWriter) preRenameSync() error {
	// On Linux data can be reliably flushed to media without metadata, so defer
	return nil
}
