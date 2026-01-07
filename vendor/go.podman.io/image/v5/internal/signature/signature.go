package signature

import (
	"bytes"
	"errors"
	"fmt"
)

// FIXME FIXME: MIME type? Int? String?
// An interface with a name, parse methods?
type FormatID string

const (
	SimpleSigningFormat FormatID = "simple-signing"
	SigstoreFormat      FormatID = "sigstore-json"
	// Update also UnsupportedFormatError below
)

// Signature is an image signature of some kind.
type Signature interface {
	FormatID() FormatID
	// blobChunk returns a representation of signature as a []byte, suitable for long-term storage.
	// Almost everyone should use signature.Blob() instead.
	blobChunk() ([]byte, error)
}

// Blob returns a representation of sig as a []byte, suitable for long-term storage.
func Blob(sig Signature) ([]byte, error) {
	chunk, err := sig.blobChunk()
	if err != nil {
		return nil, err
	}

	format := sig.FormatID()
	switch format {
	case SimpleSigningFormat:
		// For compatibility with old dir formats:
		return chunk, nil
	default:
		res := []byte{0} // Start with a zero byte to clearly mark this is a binary format, and disambiguate from random text.
		res = append(res, []byte(format)...)
		res = append(res, '\n')
		res = append(res, chunk...)
		return res, nil
	}
}

// FromBlob returns a signature from parsing a blob created by signature.Blob.
func FromBlob(blob []byte) (Signature, error) {
	if len(blob) == 0 {
		return nil, errors.New("empty signature blob")
	}
	// Historically we’ve just been using GPG with no identification; try to auto-detect that.
	switch blob[0] {
	// OpenPGP "compressed data" wrapping the message
	case 0xA0, 0xA1, 0xA2, 0xA3, // bit 7 = 1; bit 6 = 0 (old packet format); bits 5…2 = 8 (tag: compressed data packet); bits 1…0 = length-type (any)
		0xC8, // bit 7 = 1; bit 6 = 1 (new packet format); bits 5…0 = 8 (tag: compressed data packet)
		// OpenPGP “one-pass signature” starting a signature
		0x90, 0x91, 0x92, 0x3d, // bit 7 = 1; bit 6 = 0 (old packet format); bits 5…2 = 4 (tag: one-pass signature packet); bits 1…0 = length-type (any)
		0xC4, // bit 7 = 1; bit 6 = 1 (new packet format); bits 5…0 = 4 (tag: one-pass signature packet)
		// OpenPGP signature packet signing the following data
		0x88, 0x89, 0x8A, 0x8B, // bit 7 = 1; bit 6 = 0 (old packet format); bits 5…2 = 2 (tag: signature packet); bits 1…0 = length-type (any)
		0xC2: // bit 7 = 1; bit 6 = 1 (new packet format); bits 5…0 = 2 (tag: signature packet)
		return SimpleSigningFromBlob(blob), nil

		// The newer format: binary 0, format name, newline, data
	case 0x00:
		blob = blob[1:]
		formatBytes, blobChunk, foundNewline := bytes.Cut(blob, []byte{'\n'})
		if !foundNewline {
			return nil, fmt.Errorf("invalid signature format, missing newline")
		}
		for _, b := range formatBytes {
			if b < 32 || b >= 0x7F {
				return nil, fmt.Errorf("invalid signature format, non-ASCII byte %#x", b)
			}
		}
		switch {
		case bytes.Equal(formatBytes, []byte(SimpleSigningFormat)):
			return SimpleSigningFromBlob(blobChunk), nil
		case bytes.Equal(formatBytes, []byte(SigstoreFormat)):
			return sigstoreFromBlobChunk(blobChunk)
		default:
			return nil, fmt.Errorf("unrecognized signature format %q", string(formatBytes))
		}

	default:
		return nil, fmt.Errorf("unrecognized signature format, starting with binary %#x", blob[0])
	}

}

// UnsupportedFormatError returns an error complaining about sig having an unsupported format.
func UnsupportedFormatError(sig Signature) error {
	formatID := sig.FormatID()
	switch formatID {
	case SimpleSigningFormat, SigstoreFormat:
		return fmt.Errorf("unsupported signature format %s", string(formatID))
	default:
		return fmt.Errorf("unsupported, and unrecognized, signature format %q", string(formatID))
	}
}
