package mkcw

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/containers/buildah/define"
	"github.com/containers/buildah/internal/mkcw/types"
)

type (
	// WorkloadConfig is the data type which is encoded and stored in an image.
	WorkloadConfig = types.WorkloadConfig
	// SevWorkloadData is the type of data in WorkloadConfig.TeeData when the type is SEV.
	SevWorkloadData = types.SevWorkloadData
	// SnpWorkloadData is the type of data in WorkloadConfig.TeeData when the type is SNP.
	SnpWorkloadData = types.SnpWorkloadData
	// TeeType is one of the known types of trusted execution environments for which we
	// can generate suitable image contents.
	TeeType = define.TeeType
)

const (
	maxWorkloadConfigSize    = 1024 * 1024
	preferredPaddingBoundary = 4096

	// krun looks for its configuration JSON directly in a disk image if the last twelve bytes
	// of the disk image are this magic value followed by a little-endian 64-bit
	// length-of-the-configuration
	krunMagic = "KRUN"
)

//nolint:revive,staticcheck
const (
	// SEV is a known trusted execution environment type: AMD-SEV
	SEV = define.SEV
	// SEV_NO_ES is a known trusted execution environment type: AMD-SEV without encrypted state
	SEV_NO_ES = types.SEV_NO_ES
	// SNP is a known trusted execution environment type: AMD-SNP
	SNP = define.SNP
)

// ReadWorkloadConfigFromImage reads the workload configuration from the
// specified disk image file
func ReadWorkloadConfigFromImage(path string) (WorkloadConfig, error) {
	// Read the last 12 bytes, which should be "KRUN" followed by a 64-bit
	// little-endian length.  The (length) bytes immediately preceding
	// these hold the JSON-encoded workloadConfig.
	var wc WorkloadConfig
	f, err := os.Open(path)
	if err != nil {
		return wc, err
	}
	defer f.Close()

	// Read those last 12 bytes.
	finalTwelve := make([]byte, 12)
	if _, err = f.Seek(-12, io.SeekEnd); err != nil {
		return wc, fmt.Errorf("checking for workload config signature: %w", err)
	}
	if n, err := f.Read(finalTwelve); err != nil || n != len(finalTwelve) {
		if err != nil && !errors.Is(err, io.EOF) {
			return wc, fmt.Errorf("reading workload config signature (%d bytes read): %w", n, err)
		}
		if n != len(finalTwelve) {
			return wc, fmt.Errorf("short read (expected 12 bytes at the end of %q, got %d)", path, n)
		}
	}
	if magic := string(finalTwelve[0:4]); magic != "KRUN" {
		return wc, fmt.Errorf("expected magic string KRUN in %q, found %q)", path, magic)
	}
	length := binary.LittleEndian.Uint64(finalTwelve[4:])
	if length > maxWorkloadConfigSize {
		return wc, fmt.Errorf("workload config in %q is %d bytes long, which seems unreasonable (max allowed %d)", path, length, maxWorkloadConfigSize)
	}

	// Read and decode the config.
	configBytes := make([]byte, length)
	if _, err = f.Seek(-(int64(length) + 12), io.SeekEnd); err != nil {
		return wc, fmt.Errorf("looking for workload config from disk image: %w", err)
	}
	if n, err := f.Read(configBytes); err != nil || n != len(configBytes) {
		if err != nil {
			return wc, fmt.Errorf("reading workload config from disk image: %w", err)
		}
		return wc, fmt.Errorf("short read (expected %d bytes near the end of %q, got %d)", len(configBytes), path, n)
	}
	err = json.Unmarshal(configBytes, &wc)
	if err != nil {
		err = fmt.Errorf("unmarshalling configuration %q: %w", string(configBytes), err)
	}
	return wc, err
}

// WriteWorkloadConfigToImage writes the workload configuration to the
// specified disk image file, overwriting a previous configuration if it's
// asked to and it finds one
func WriteWorkloadConfigToImage(imageFile *os.File, workloadConfigBytes []byte, overwrite bool) error {
	// Read those last 12 bytes to check if there's a configuration there already, which we should overwrite.
	var overwriteOffset int64
	if overwrite {
		finalTwelve := make([]byte, 12)
		if _, err := imageFile.Seek(-12, io.SeekEnd); err != nil {
			return fmt.Errorf("checking for workload config signature: %w", err)
		}
		if n, err := imageFile.Read(finalTwelve); err != nil || n != len(finalTwelve) {
			if err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("reading workload config signature (%d bytes read): %w", n, err)
			}
			if n != len(finalTwelve) {
				return fmt.Errorf("short read (expected 12 bytes at the end of %q, got %d)", imageFile.Name(), n)
			}
		}
		if magic := string(finalTwelve[0:4]); magic == "KRUN" {
			length := binary.LittleEndian.Uint64(finalTwelve[4:])
			if length < maxWorkloadConfigSize {
				overwriteOffset = int64(length + 12)
			}
		}
	}
	// If we found a configuration in the file, try to figure out how much padding was used.
	paddingSize := int64(preferredPaddingBoundary)
	if overwriteOffset != 0 {
		st, err := imageFile.Stat()
		if err != nil {
			return err
		}
		for _, possiblePaddingLength := range []int64{0x100000, 0x10000, 0x1000, 0x200, 0x100} {
			if overwriteOffset > possiblePaddingLength {
				continue
			}
			if st.Size()%possiblePaddingLength != 0 {
				continue
			}
			if _, err := imageFile.Seek(-possiblePaddingLength, io.SeekEnd); err != nil {
				return fmt.Errorf("checking size of padding at end of file: %w", err)
			}
			buf := make([]byte, possiblePaddingLength)
			n, err := imageFile.Read(buf)
			if err != nil {
				return fmt.Errorf("reading possible padding at end of file: %w", err)
			}
			if n != len(buf) {
				return fmt.Errorf("short read checking size of padding at end of file: %d != %d", n, len(buf))
			}
			if bytes.Equal(buf[:possiblePaddingLength-overwriteOffset], make([]byte, possiblePaddingLength-overwriteOffset)) {
				// everything up to the configuration was zero bytes, so it was padding
				overwriteOffset = possiblePaddingLength
				paddingSize = possiblePaddingLength
				break
			}
		}
	}

	// Append the krun configuration to a new buffer.
	var formatted bytes.Buffer
	nWritten, err := formatted.Write(workloadConfigBytes)
	if err != nil {
		return fmt.Errorf("building workload config: %w", err)
	}
	if nWritten != len(workloadConfigBytes) {
		return fmt.Errorf("short write appending configuration to buffer: %d != %d", nWritten, len(workloadConfigBytes))
	}
	// Append the magic string to the buffer.
	nWritten, err = formatted.WriteString(krunMagic)
	if err != nil {
		return fmt.Errorf("building workload config signature: %w", err)
	}
	if nWritten != len(krunMagic) {
		return fmt.Errorf("short write appending krun magic to buffer: %d != %d", nWritten, len(krunMagic))
	}
	// Append the 64-bit little-endian length of the workload configuration to the buffer.
	workloadConfigLengthBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(workloadConfigLengthBytes, uint64(len(workloadConfigBytes)))
	nWritten, err = formatted.Write(workloadConfigLengthBytes)
	if err != nil {
		return fmt.Errorf("building workload config signature size: %w", err)
	}
	if nWritten != len(workloadConfigLengthBytes) {
		return fmt.Errorf("short write appending configuration length to buffer: %d != %d", nWritten, len(workloadConfigLengthBytes))
	}

	// Build a copy of that data, with padding preceding it.
	var padded bytes.Buffer
	if int64(formatted.Len())%paddingSize != 0 {
		extra := paddingSize - (int64(formatted.Len()) % paddingSize)
		nWritten, err := padded.Write(make([]byte, extra))
		if err != nil {
			return fmt.Errorf("buffering padding: %w", err)
		}
		if int64(nWritten) != extra {
			return fmt.Errorf("short write buffering padding for disk image: %d != %d", nWritten, extra)
		}
	}
	extra := int64(formatted.Len())
	nWritten, err = padded.Write(formatted.Bytes())
	if err != nil {
		return fmt.Errorf("buffering workload config: %w", err)
	}
	if int64(nWritten) != extra {
		return fmt.Errorf("short write buffering workload config: %d != %d", nWritten, extra)
	}

	// Write the buffer to the file, starting with padding.
	if _, err = imageFile.Seek(-overwriteOffset, io.SeekEnd); err != nil {
		return fmt.Errorf("preparing to write workload config: %w", err)
	}
	nWritten, err = imageFile.Write(padded.Bytes())
	if err != nil {
		return fmt.Errorf("writing workload config: %w", err)
	}
	if nWritten != padded.Len() {
		return fmt.Errorf("short write writing configuration to disk image: %d != %d", nWritten, padded.Len())
	}
	offset, err := imageFile.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("preparing mark end of disk image: %w", err)
	}
	if err = imageFile.Truncate(offset); err != nil {
		return fmt.Errorf("marking end of disk image: %w", err)
	}
	return nil
}
