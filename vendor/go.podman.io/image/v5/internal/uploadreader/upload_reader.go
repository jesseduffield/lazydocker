package uploadreader

import (
	"io"
	"sync"
)

// UploadReader is a pass-through reader for use in sending non-trivial data using the net/http
// package (http.NewRequest, http.Post and the like).
//
// The net/http package uses a separate goroutine to upload data to a HTTP connection,
// and it is possible for the server to return a response (typically an error) before consuming
// the full body of the request. In that case http.Client.Do can return with an error while
// the body is still being read — regardless of the cancellation, if any, of http.Request.Context().
//
// As a result, any data used/updated by the io.Reader() provided as the request body may be
// used/updated even after http.Client.Do returns, causing races.
//
// To fix this, UploadReader provides a synchronized Terminate() method, which can block for
// a not-completely-negligible time (for a duration of the underlying Read()), but guarantees that
// after Terminate() returns, the underlying reader is never used any more (unlike calling
// the cancellation callback of context.WithCancel, which returns before any recipients may have
// reacted to the cancellation).
type UploadReader struct {
	mutex sync.Mutex
	// The following members can only be used with mutex held
	reader           io.Reader
	terminationError error // nil if not terminated yet
}

// NewUploadReader returns an UploadReader for an "underlying" reader.
func NewUploadReader(underlying io.Reader) *UploadReader {
	return &UploadReader{
		reader:           underlying,
		terminationError: nil,
	}
}

// Read returns the error set by Terminate, if any, or calls the underlying reader.
// It is safe to call this from a different goroutine than Terminate.
func (ur *UploadReader) Read(p []byte) (int, error) {
	ur.mutex.Lock()
	defer ur.mutex.Unlock()

	if ur.terminationError != nil {
		return 0, ur.terminationError
	}
	return ur.reader.Read(p)
}

// Terminate waits for in-progress Read calls, if any, to finish, and ensures that after
// this function returns, any Read calls will fail with the provided error, and the underlying
// reader will never be used any more.
//
// It is safe to call this from a different goroutine than Read.
func (ur *UploadReader) Terminate(err error) {
	ur.mutex.Lock() // May block for some time if ur.reader.Read() is in progress
	defer ur.mutex.Unlock()

	ur.terminationError = err
}
