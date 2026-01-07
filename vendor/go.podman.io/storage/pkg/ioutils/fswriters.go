package ioutils

import (
	"io"
	"os"
	"path/filepath"
	"time"
)

// AtomicFileWriterOptions specifies options for creating the atomic file writer.
type AtomicFileWriterOptions struct {
	// NoSync specifies whether the sync call must be skipped for the file.
	// If NoSync is not specified, the file is synced to the
	// storage after it has been written and before it is moved to
	// the specified path.
	NoSync bool
	// On successful return from Close() this is set to the mtime of the
	// newly written file.
	ModTime time.Time
	// Specifies whether Commit() must be explicitly called to write state
	// to the destination. This allows an application to preserve the original
	// file when an error occurs during processing (and not just during write)
	// The default is false, which will auto-commit on Close
	ExplicitCommit bool
}

type CommittableWriter interface {
	io.WriteCloser

	// Commit closes the temporary file associated with this writer, and
	// provided no errors (during commit or previously during write operations),
	// will publish the completed file under the intended destination.
	Commit() error
}

var defaultWriterOptions = AtomicFileWriterOptions{}

// SetDefaultOptions overrides the default options used when creating an
// atomic file writer.
func SetDefaultOptions(opts AtomicFileWriterOptions) {
	defaultWriterOptions = opts
}

// NewAtomicFileWriterWithOpts returns a CommittableWriter so that writing to it
// writes to a temporary file, which can later be committed to a destination path,
// either by Closing in the case of auto-commit, or manually calling commit if the
// ExplicitCommit option is enabled. Writing and closing concurrently is not
// allowed.
func NewAtomicFileWriterWithOpts(filename string, perm os.FileMode, opts *AtomicFileWriterOptions) (CommittableWriter, error) {
	return newAtomicFileWriter(filename, perm, opts)
}

// newAtomicFileWriter returns a CommittableWriter so that writing to it writes to
// a temporary file, which can later be committed to a destination path, either by
// Closing in the case of auto-commit, or manually calling commit if the
// ExplicitCommit option is enabled. Writing and closing concurrently is not allowed.
func newAtomicFileWriter(filename string, perm os.FileMode, opts *AtomicFileWriterOptions) (*atomicFileWriter, error) {
	f, err := os.CreateTemp(filepath.Dir(filename), ".tmp-"+filepath.Base(filename))
	if err != nil {
		return nil, err
	}
	if opts == nil {
		opts = &defaultWriterOptions
	}
	abspath, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	return &atomicFileWriter{
		f:              f,
		fn:             abspath,
		perm:           perm,
		noSync:         opts.NoSync,
		explicitCommit: opts.ExplicitCommit,
	}, nil
}

// NewAtomicFileWriterWithOpts returns a CommittableWriter, with auto-commit enabled.
// Writing to it writes to a temporary file and closing it atomically changes the
// temporary file to destination path. Writing and closing concurrently is not allowed.
func NewAtomicFileWriter(filename string, perm os.FileMode) (CommittableWriter, error) {
	return NewAtomicFileWriterWithOpts(filename, perm, nil)
}

// AtomicWriteFile atomically writes data to a file named by filename.
func AtomicWriteFileWithOpts(filename string, data []byte, perm os.FileMode, opts *AtomicFileWriterOptions) error {
	f, err := newAtomicFileWriter(filename, perm, opts)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err == nil && n < len(data) {
		err = io.ErrShortWrite
		f.writeErr = err
	}
	if err1 := f.Close(); err == nil {
		err = err1
	}

	if opts != nil {
		opts.ModTime = f.modTime
	}

	return err
}

func AtomicWriteFile(filename string, data []byte, perm os.FileMode) error {
	return AtomicWriteFileWithOpts(filename, data, perm, nil)
}

type atomicFileWriter struct {
	f              *os.File
	fn             string
	writeErr       error
	perm           os.FileMode
	noSync         bool
	modTime        time.Time
	closed         bool
	explicitCommit bool
}

func (w *atomicFileWriter) Write(dt []byte) (int, error) {
	n, err := w.f.Write(dt)
	if err != nil {
		w.writeErr = err
	}
	return n, err
}

func (w *atomicFileWriter) closeTempFile() error {
	if w.closed {
		return nil
	}

	w.closed = true
	return w.f.Close()
}

func (w *atomicFileWriter) Close() error {
	return w.complete(!w.explicitCommit)
}

func (w *atomicFileWriter) Commit() error {
	return w.complete(true)
}

func (w *atomicFileWriter) complete(commit bool) (retErr error) {
	if w == nil || w.closed {
		return nil
	}

	defer func() {
		err := w.closeTempFile()
		if retErr != nil || w.writeErr != nil {
			os.Remove(w.f.Name())
		}
		if retErr == nil {
			retErr = err
		}
	}()

	if commit {
		return w.commitState()
	}

	return nil
}

func (w *atomicFileWriter) commitState() error {
	// Perform a data only sync (fdatasync()) if supported
	if err := w.postDataWrittenSync(); err != nil {
		return err
	}

	// Capture fstat before closing the fd
	info, err := w.f.Stat()
	if err != nil {
		return err
	}
	w.modTime = info.ModTime()

	if err := w.f.Chmod(w.perm); err != nil {
		return err
	}

	// Perform full sync on platforms that need it
	if err := w.preRenameSync(); err != nil {
		return err
	}

	// Some platforms require closing before rename (Windows)
	if err := w.closeTempFile(); err != nil {
		return err
	}

	if w.writeErr == nil {
		return os.Rename(w.f.Name(), w.fn)
	}

	return nil
}

// AtomicWriteSet is used to atomically write a set
// of files and ensure they are visible at the same time.
// Must be committed to a new directory.
type AtomicWriteSet struct {
	root string
}

// NewAtomicWriteSet creates a new atomic write set to
// atomically create a set of files. The given directory
// is used as the base directory for storing files before
// commit. If no temporary directory is given the system
// default is used.
func NewAtomicWriteSet(tmpDir string) (*AtomicWriteSet, error) {
	td, err := os.MkdirTemp(tmpDir, "write-set-")
	if err != nil {
		return nil, err
	}

	return &AtomicWriteSet{
		root: td,
	}, nil
}

// WriteFile writes a file to the set, guaranteeing the file
// has been synced.
func (ws *AtomicWriteSet) WriteFile(filename string, data []byte, perm os.FileMode) error {
	f, err := ws.FileWriter(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err == nil && n < len(data) {
		err = io.ErrShortWrite
	}
	if err1 := f.Close(); err == nil {
		err = err1
	}
	return err
}

type syncFileCloser struct {
	*os.File
}

func (w syncFileCloser) Close() error {
	if !defaultWriterOptions.NoSync {
		return w.File.Close()
	}
	err := dataOrFullSync(w.File)
	if err1 := w.File.Close(); err == nil {
		err = err1
	}
	return err
}

// FileWriter opens a file writer inside the set. The file
// should be synced and closed before calling commit.
func (ws *AtomicWriteSet) FileWriter(name string, flag int, perm os.FileMode) (io.WriteCloser, error) {
	f, err := os.OpenFile(filepath.Join(ws.root, name), flag, perm)
	if err != nil {
		return nil, err
	}
	return syncFileCloser{f}, nil
}

// Cancel cancels the set and removes all temporary data
// created in the set.
func (ws *AtomicWriteSet) Cancel() error {
	return os.RemoveAll(ws.root)
}

// Commit moves all created files to the target directory. The
// target directory must not exist and the parent of the target
// directory must exist.
func (ws *AtomicWriteSet) Commit(target string) error {
	return os.Rename(ws.root, target)
}

// String returns the location the set is writing to.
func (ws *AtomicWriteSet) String() string {
	return ws.root
}
