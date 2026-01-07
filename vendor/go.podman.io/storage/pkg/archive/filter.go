package archive

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

var filterPath sync.Map

func getFilterPath(name string) string {
	path, ok := filterPath.Load(name)
	if ok {
		return path.(string)
	}

	path, err := exec.LookPath(name)
	if err != nil {
		path = ""
	}

	filterPath.Store(name, path)
	return path.(string)
}

type errorRecordingReader struct {
	r   io.Reader
	err error
}

func (r *errorRecordingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if r.err == nil && err != io.EOF {
		r.err = err
	}
	return n, err
}

// tryProcFilter tries to run the command specified in args, passing input to its stdin and returning its stdout.
// cleanup() is a caller provided function that will be called when the command finishes running, regardless of
// whether it succeeds or fails.
// If the command is not found, it returns (nil, false) and the cleanup function is not called.
func tryProcFilter(args []string, input io.Reader, cleanup func()) (io.ReadCloser, bool) {
	path := getFilterPath(args[0])
	if path == "" {
		return nil, false
	}

	var stderrBuf bytes.Buffer

	inputWithError := &errorRecordingReader{r: input}

	r, w := io.Pipe()
	cmd := exec.Command(path, args[1:]...)
	cmd.Stdin = inputWithError
	cmd.Stdout = w
	cmd.Stderr = &stderrBuf
	go func() {
		err := cmd.Run()
		// if there is an error reading from input, prefer to return that error
		if inputWithError.err != nil {
			err = inputWithError.err
		} else if err != nil && stderrBuf.Len() > 0 {
			err = fmt.Errorf("%s: %w", strings.TrimRight(stderrBuf.String(), "\n"), err)
		}
		w.CloseWithError(err) // CloseWithErr(nil) == Close()
		cleanup()
	}()
	return r, true
}
