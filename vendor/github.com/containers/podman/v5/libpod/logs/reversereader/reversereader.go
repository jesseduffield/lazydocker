package reversereader

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ReverseReader structure for reading a file backwards
type ReverseReader struct {
	reader   *os.File
	offset   int64
	readSize int64
}

// NewReverseReader returns a reader that reads from the end of a file
// rather than the beginning.  It sets the readsize to pagesize and determines
// the first offset using modulus.
func NewReverseReader(reader *os.File) (*ReverseReader, error) {
	// pagesize should be safe for memory use and file reads should be on page
	// boundaries as well
	pageSize := int64(os.Getpagesize())
	stat, err := reader.Stat()
	if err != nil {
		return nil, err
	}
	// figure out the last page boundary
	remainder := stat.Size() % pageSize
	end, err := reader.Seek(0, 2)
	if err != nil {
		return nil, err
	}
	// set offset (starting position) to the last page boundary or
	// zero if fits in one page
	startOffset := max(end-remainder, 0)
	rr := ReverseReader{
		reader:   reader,
		offset:   startOffset,
		readSize: pageSize,
	}
	return &rr, nil
}

// ReverseReader reads from a given offset to the previous offset and
// then sets the newoff set one pagesize less than the previous read.
func (r *ReverseReader) Read() (string, error) {
	if r.offset < 0 {
		return "", fmt.Errorf("at beginning of file: %w", io.EOF)
	}
	// Read from given offset
	b := make([]byte, r.readSize)
	n, err := r.reader.ReadAt(b, r.offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	// Move the offset one pagesize up
	r.offset -= r.readSize
	return string(b[:n]), nil
}
