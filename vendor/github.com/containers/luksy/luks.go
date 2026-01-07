package luksy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// ReadHeaderOptions can control some of what ReadHeaders() does.
type ReadHeaderOptions struct{}

// ReadHeaders reads LUKS headers from the specified file, returning either a
// LUKSv1 header, or two LUKSv2 headers and a LUKSv2 JSON block, depending on
// which format is detected.
func ReadHeaders(f io.ReaderAt, options ReadHeaderOptions) (*V1Header, *V2Header, *V2Header, *V2JSON, error) {
	var v1 V1Header
	var v2a, v2b V2Header
	n, err := f.ReadAt(v2a[:], 0)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if n != len(v2a) {
		return nil, nil, nil, nil, fmt.Errorf("only able to read %d bytes - file truncated?", n)
	}
	if n, err = f.ReadAt(v1[:], 0); err != nil {
		return nil, nil, nil, nil, err
	}
	if n != len(v1) {
		return nil, nil, nil, nil, fmt.Errorf("only able to read %d bytes - file truncated?", n)
	}
	if v2a.Magic() != V2Magic1 {
		return nil, nil, nil, nil, fmt.Errorf("internal error: magic mismatch in LUKS header (%q)", v2a.Magic())
	}
	switch v2a.Version() { // is it a v1 header, or the first v2 header?
	case 1:
		return &v1, nil, nil, nil, nil
	case 2:
		size := v2a.HeaderSize()
		if size > 0x7fffffffffffffff {
			return nil, nil, nil, nil, fmt.Errorf("unsupported header size while looking for second header")
		}
		if size < 4096 {
			return nil, nil, nil, nil, fmt.Errorf("unsupported header size while looking for JSON data")
		}
		if n, err = f.ReadAt(v2b[:], int64(size)); err != nil || n != len(v2b) {
			if err == nil && n != len(v2b) {
				err = fmt.Errorf("short read: read only %d bytes, should have read %d", n, len(v2b))
			}
			return nil, nil, nil, nil, err
		}
		if v2b.Magic() != V2Magic2 {
			return nil, nil, nil, nil, fmt.Errorf("internal error: magic mismatch in second LUKS header (%q)", v2b.Magic())
		}
		jsonSize := size - 4096
		buf := make([]byte, jsonSize)
		n, err = f.ReadAt(buf[:], 4096)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("internal error: while reading JSON data: %w", err)
		}
		if n < 0 || uint64(n) != jsonSize {
			return nil, nil, nil, nil, fmt.Errorf("internal error: short read while reading JSON data (wanted %d, got %d)", jsonSize, n)
		}
		var jsonData V2JSON
		buf = bytes.TrimRightFunc(buf, func(r rune) bool { return r == 0 })
		if err = json.Unmarshal(buf, &jsonData); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("internal error: decoding JSON data: %w", err)
		}
		if uint64(jsonData.Config.JsonSize) != jsonSize {
			return nil, nil, nil, nil, fmt.Errorf("internal error: JSON data size mismatch: (expected %d, used %d)", jsonData.Config.JsonSize, jsonSize)
		}
		return nil, &v2a, &v2b, &jsonData, nil
	}
	return nil, nil, nil, nil, fmt.Errorf("error reading LUKS header - magic identifier not found")
}
