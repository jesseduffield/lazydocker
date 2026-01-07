package common

import (
	"errors"
	"os"
	"sync"
	"syscall"
)

var bufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, syscall.PathMax)
		return &b
	},
}

// The following three functions are copied from stdlib.

// ignoringEINTR2 is ignoringEINTR, but returning an additional value.
func ignoringEINTR2[T any](fn func() (T, error)) (T, error) {
	for {
		v, err := fn()
		if !errors.Is(err, syscall.EINTR) {
			return v, err
		}
	}
}

// Many functions in package syscall return a count of -1 instead of 0.
// Using fixCount(call()) instead of call() corrects the count.
func fixCount(n int, err error) (int, error) {
	if n < 0 {
		n = 0
	}
	return n, err
}

// Readlink behaves like os.Readlink but caches the buffer passed to syscall.Readlink.
func Readlink(name string) (string, error) {
	b := bufferPool.Get().(*[]byte)

	n, err := ignoringEINTR2(func() (int, error) {
		return fixCount(syscall.Readlink(name, *b))
	})
	if err != nil {
		bufferPool.Put(b)
		return "", &os.PathError{Op: "readlink", Path: name, Err: err}
	}

	result := string((*b)[:n])
	bufferPool.Put(b)
	return result, nil
}
