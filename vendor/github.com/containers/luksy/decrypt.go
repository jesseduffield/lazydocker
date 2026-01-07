package luksy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

// ReaderAtSeekCloser is a combination of io.ReaderAt, io.Seeker, and io.Closer,
// which is all we really need from an encrypted file.
type ReaderAtSeekCloser interface {
	io.ReaderAt
	io.Seeker
	io.Closer
}

// Decrypt attempts to verify the specified password using information from the
// header and read from the specified file.
//
// Returns a function which will decrypt payload blocks in succession, the size
// of chunks of data that the function expects, the offset in the file where
// the payload begins, and the size of the payload, assuming the payload runs
// to the end of the file.
func (h V1Header) Decrypt(password string, f ReaderAtSeekCloser) (func([]byte) ([]byte, error), int, int64, int64, error) {
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, -1, -1, -1, err
	}
	hasher, err := hasherByName(h.HashSpec())
	if err != nil {
		return nil, -1, -1, -1, fmt.Errorf("unsupported digest algorithm %q: %w", h.HashSpec(), err)
	}

	activeKeys := 0
	for k := 0; k < v1NumKeys; k++ {
		keyslot, err := h.KeySlot(k)
		if err != nil {
			return nil, -1, -1, -1, fmt.Errorf("reading key slot %d: %w", k, err)
		}
		active, err := keyslot.Active()
		if err != nil {
			return nil, -1, -1, -1, fmt.Errorf("checking if key slot %d is active: %w", k, err)
		}
		if !active {
			continue
		}
		activeKeys++

		passwordDerived := pbkdf2.Key([]byte(password), keyslot.KeySlotSalt(), int(keyslot.Iterations()), int(h.KeyBytes()), hasher)
		striped := make([]byte, h.KeyBytes()*keyslot.Stripes())
		n, err := f.ReadAt(striped, int64(keyslot.KeyMaterialOffset())*V1SectorSize)
		if err != nil {
			return nil, -1, -1, -1, fmt.Errorf("reading diffuse material for keyslot %d: %w", k, err)
		}
		if n != len(striped) {
			return nil, -1, -1, -1, fmt.Errorf("short read while reading diffuse material for keyslot %d: expected %d, got %d", k, len(striped), n)
		}
		splitKey, err := v1decrypt(h.CipherName(), h.CipherMode(), 0, passwordDerived, striped, V1SectorSize, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error attempting to decrypt main key: %v\n", err)
			continue
		}
		mkCandidate, err := afMerge(splitKey, hasher(), int(h.KeyBytes()), int(keyslot.Stripes()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error attempting to compute main key: %v\n", err)
			continue
		}
		mkcandidateDerived := pbkdf2.Key(mkCandidate, h.MKDigestSalt(), int(h.MKDigestIter()), v1DigestSize, hasher)
		ivTweak := 0
		decryptStream := func(ciphertext []byte) ([]byte, error) {
			plaintext, err := v1decrypt(h.CipherName(), h.CipherMode(), ivTweak, mkCandidate, ciphertext, V1SectorSize, false)
			ivTweak += len(ciphertext) / V1SectorSize
			return plaintext, err
		}
		if bytes.Equal(mkcandidateDerived, h.MKDigest()) {
			payloadOffset := int64(h.PayloadOffset() * V1SectorSize)
			return decryptStream, V1SectorSize, payloadOffset, size - payloadOffset, nil
		}
	}
	if activeKeys == 0 {
		return nil, -1, -1, -1, errors.New("no passwords set on LUKS1 volume")
	}
	return nil, -1, -1, -1, errors.New("decryption error: incorrect password")
}

// Decrypt attempts to verify the specified password using information from the
// header, JSON block, and read from the specified file.
//
// Returns a function which will decrypt payload blocks in succession, the size
// of chunks of data that the function expects, the offset in the file where
// the payload begins, and the size of the payload, assuming the payload runs
// to the end of the file.
func (h V2Header) Decrypt(password string, f ReaderAtSeekCloser, j V2JSON) (func([]byte) ([]byte, error), int, int64, int64, error) {
	foundDigests := 0
	for d, digest := range j.Digests {
		if digest.Type != "pbkdf2" {
			continue
		}
		if digest.V2JSONDigestPbkdf2 == nil {
			return nil, -1, -1, -1, fmt.Errorf("digest %q is corrupt: no pbkdf2 parameters", d)
		}
		foundDigests++
		if len(digest.Segments) == 0 || len(digest.Digest) == 0 {
			continue
		}
		payloadOffset := int64(-1)
		payloadSectorSize := V1SectorSize
		payloadEncryption := ""
		payloadSize := int64(0)
		ivTweak := 0
		for _, segmentID := range digest.Segments {
			segment, ok := j.Segments[segmentID]
			if !ok {
				continue // well, that was misleading
			}
			if segment.Type != "crypt" {
				continue
			}
			tmp, err := strconv.ParseInt(segment.Offset, 10, 64)
			if err != nil {
				continue
			}
			payloadOffset = tmp
			if segment.Size == "dynamic" {
				size, err := f.Seek(0, io.SeekEnd)
				if err != nil {
					continue
				}
				payloadSize = size - payloadOffset
			} else {
				payloadSize, err = strconv.ParseInt(segment.Size, 10, 64)
				if err != nil {
					continue
				}
			}
			payloadSectorSize = segment.SectorSize
			payloadEncryption = segment.Encryption
			ivTweak = segment.IVTweak
			break
		}
		if payloadEncryption == "" {
			continue
		}
		activeKeys := 0
		for k, keyslot := range j.Keyslots {
			if keyslot.Priority != nil && *keyslot.Priority == V2JSONKeyslotPriorityIgnore {
				continue
			}
			applicable := true
			if len(digest.Keyslots) > 0 {
				applicable = false
				for i := 0; i < len(digest.Keyslots); i++ {
					if k == digest.Keyslots[i] {
						applicable = true
						break
					}
				}
			}
			if !applicable {
				continue
			}
			if keyslot.Type != "luks2" {
				continue
			}
			if keyslot.V2JSONKeyslotLUKS2 == nil {
				return nil, -1, -1, -1, fmt.Errorf("key slot %q is corrupt", k)
			}
			if keyslot.V2JSONKeyslotLUKS2.AF.Type != "luks1" {
				continue
			}
			if keyslot.V2JSONKeyslotLUKS2.AF.V2JSONAFLUKS1 == nil {
				return nil, -1, -1, -1, fmt.Errorf("key slot %q is corrupt: no AF parameters", k)
			}
			if keyslot.Area.Type != "raw" {
				return nil, -1, -1, -1, fmt.Errorf("key slot %q is corrupt: key data area is not raw", k)
			}
			if keyslot.Area.KeySize*V2SectorSize < keyslot.KeySize*keyslot.AF.Stripes {
				return nil, -1, -1, -1, fmt.Errorf("key slot %q is corrupt: key data area is too small (%d < %d)", k, keyslot.Area.KeySize*V2SectorSize, keyslot.KeySize*keyslot.AF.Stripes)
			}
			var passwordDerived []byte
			switch keyslot.V2JSONKeyslotLUKS2.Kdf.Type {
			default:
				continue
			case "pbkdf2":
				if keyslot.V2JSONKeyslotLUKS2.Kdf.V2JSONKdfPbkdf2 == nil {
					return nil, -1, -1, -1, fmt.Errorf("key slot %q is corrupt: no pbkdf2 parameters", k)
				}
				hasher, err := hasherByName(keyslot.Kdf.Hash)
				if err != nil {
					return nil, -1, -1, -1, fmt.Errorf("unsupported digest algorithm %q: %w", keyslot.Kdf.Hash, err)
				}
				passwordDerived = pbkdf2.Key([]byte(password), keyslot.Kdf.Salt, keyslot.Kdf.Iterations, keyslot.KeySize, hasher)
			case "argon2i":
				if keyslot.V2JSONKeyslotLUKS2.Kdf.V2JSONKdfArgon2i == nil {
					return nil, -1, -1, -1, fmt.Errorf("key slot %q is corrupt: no argon2i parameters", k)
				}
				passwordDerived = argon2.Key([]byte(password), keyslot.Kdf.Salt, uint32(keyslot.Kdf.Time), uint32(keyslot.Kdf.Memory), uint8(keyslot.Kdf.CPUs), uint32(keyslot.KeySize))
			case "argon2id":
				if keyslot.V2JSONKeyslotLUKS2.Kdf.V2JSONKdfArgon2i == nil {
					return nil, -1, -1, -1, fmt.Errorf("key slot %q is corrupt: no argon2id parameters", k)
				}
				passwordDerived = argon2.IDKey([]byte(password), keyslot.Kdf.Salt, uint32(keyslot.Kdf.Time), uint32(keyslot.Kdf.Memory), uint8(keyslot.Kdf.CPUs), uint32(keyslot.KeySize))
			}
			striped := make([]byte, keyslot.KeySize*keyslot.AF.Stripes)
			n, err := f.ReadAt(striped, int64(keyslot.Area.Offset))
			if err != nil {
				return nil, -1, -1, -1, fmt.Errorf("reading diffuse material for keyslot %q: %w", k, err)
			}
			if n != len(striped) {
				return nil, -1, -1, -1, fmt.Errorf("short read while reading diffuse material for keyslot %q: expected %d, got %d", k, len(striped), n)
			}
			splitKey, err := v2decrypt(keyslot.Area.Encryption, 0, passwordDerived, striped, V1SectorSize, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error attempting to decrypt main key: %v\n", err)
				continue
			}
			afhasher, err := hasherByName(keyslot.AF.Hash)
			if err != nil {
				return nil, -1, -1, -1, fmt.Errorf("unsupported digest algorithm %q: %w", keyslot.AF.Hash, err)
			}
			mkCandidate, err := afMerge(splitKey, afhasher(), int(keyslot.KeySize), int(keyslot.AF.Stripes))
			if err != nil {
				fmt.Fprintf(os.Stderr, "error attempting to compute main key: %v\n", err)
				continue
			}
			digester, err := hasherByName(digest.Hash)
			if err != nil {
				return nil, -1, -1, -1, fmt.Errorf("unsupported digest algorithm %q: %w", digest.Hash, err)
			}
			mkcandidateDerived := pbkdf2.Key(mkCandidate, digest.Salt, digest.Iterations, len(digest.Digest), digester)
			decryptStream := func(ciphertext []byte) ([]byte, error) {
				plaintext, err := v2decrypt(payloadEncryption, ivTweak, mkCandidate, ciphertext, payloadSectorSize, true)
				ivTweak += len(ciphertext) / payloadSectorSize
				return plaintext, err
			}
			if bytes.Equal(mkcandidateDerived, digest.Digest) {
				return decryptStream, payloadSectorSize, payloadOffset, payloadSize, nil
			}
			activeKeys++
		}
		if activeKeys == 0 {
			return nil, -1, -1, -1, fmt.Errorf("no passwords set on LUKS2 volume for digest %q", d)
		}
	}
	if foundDigests == 0 {
		return nil, -1, -1, -1, errors.New("no usable password-verification digests set on LUKS2 volume")
	}
	return nil, -1, -1, -1, errors.New("decryption error: incorrect password")
}
