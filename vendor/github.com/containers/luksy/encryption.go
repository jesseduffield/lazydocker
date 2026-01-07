package luksy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"strings"

	"github.com/aead/serpent"
	"golang.org/x/crypto/cast5"
	"golang.org/x/crypto/ripemd160"
	"golang.org/x/crypto/twofish"
	"golang.org/x/crypto/xts"
)

func v1encrypt(cipherName, cipherMode string, ivTweak int, key []byte, plaintext []byte, sectorSize int, bulk bool) ([]byte, error) {
	var err error
	var newBlockCipher func([]byte) (cipher.Block, error)
	ciphertext := make([]byte, len(plaintext))

	switch cipherName {
	case "aes":
		newBlockCipher = aes.NewCipher
	case "twofish":
		newBlockCipher = func(key []byte) (cipher.Block, error) { return twofish.NewCipher(key) }
	case "cast5":
		newBlockCipher = func(key []byte) (cipher.Block, error) { return cast5.NewCipher(key) }
	case "serpent":
		newBlockCipher = serpent.NewCipher
	default:
		return nil, fmt.Errorf("unsupported cipher %s", cipherName)
	}
	if sectorSize == 0 {
		sectorSize = V1SectorSize
	}
	switch sectorSize {
	default:
		return nil, fmt.Errorf("invalid sector size %d", sectorSize)
	case 512, 1024, 2048, 4096:
	}

	switch cipherMode {
	case "ecb":
		cipher, err := newBlockCipher(key)
		if err != nil {
			return nil, fmt.Errorf("initializing encryption: %w", err)
		}
		for processed := 0; processed < len(plaintext); processed += cipher.BlockSize() {
			blockLeft := sectorSize
			if processed+blockLeft > len(plaintext) {
				blockLeft = len(plaintext) - processed
			}
			cipher.Encrypt(ciphertext[processed:processed+blockLeft], plaintext[processed:processed+blockLeft])
		}
	case "cbc-plain":
		block, err := newBlockCipher(key)
		if err != nil {
			return nil, fmt.Errorf("initializing encryption: %w", err)
		}
		for processed := 0; processed < len(plaintext); processed += sectorSize {
			blockLeft := sectorSize
			if processed+blockLeft > len(plaintext) {
				blockLeft = len(plaintext) - processed
			}
			ivValue := processed/sectorSize + ivTweak
			if bulk { // iv_large_sectors is not being used
				ivValue *= sectorSize / V1SectorSize
			}
			iv0 := make([]byte, block.BlockSize())
			binary.LittleEndian.PutUint32(iv0, uint32(ivValue))
			cipher := cipher.NewCBCEncrypter(block, iv0)
			cipher.CryptBlocks(ciphertext[processed:processed+blockLeft], plaintext[processed:processed+blockLeft])
		}
	case "cbc-plain64":
		block, err := newBlockCipher(key)
		if err != nil {
			return nil, fmt.Errorf("initializing encryption: %w", err)
		}
		for processed := 0; processed < len(plaintext); processed += sectorSize {
			blockLeft := sectorSize
			if processed+blockLeft > len(plaintext) {
				blockLeft = len(plaintext) - processed
			}
			ivValue := processed/sectorSize + ivTweak
			if bulk { // iv_large_sectors is not being used
				ivValue *= sectorSize / V1SectorSize
			}
			iv0 := make([]byte, block.BlockSize())
			binary.LittleEndian.PutUint64(iv0, uint64(ivValue))
			cipher := cipher.NewCBCEncrypter(block, iv0)
			cipher.CryptBlocks(ciphertext[processed:processed+blockLeft], plaintext[processed:processed+blockLeft])
		}
	case "cbc-essiv:sha256":
		hasherName := strings.TrimPrefix(cipherMode, "cbc-essiv:")
		hasher, err := hasherByName(hasherName)
		if err != nil {
			return nil, fmt.Errorf("initializing encryption using hash %s: %w", hasherName, err)
		}
		h := hasher()
		h.Write(key)
		makeiv, err := newBlockCipher(h.Sum(nil))
		if err != nil {
			return nil, fmt.Errorf("initializing encryption: %w", err)
		}
		block, err := newBlockCipher(key)
		if err != nil {
			return nil, fmt.Errorf("initializing encryption: %w", err)
		}
		for processed := 0; processed < len(plaintext); processed += sectorSize {
			blockLeft := sectorSize
			if processed+blockLeft > len(plaintext) {
				blockLeft = len(plaintext) - processed
			}
			ivValue := (processed/sectorSize + ivTweak)
			if bulk { // iv_large_sectors is not being used
				ivValue *= sectorSize / V1SectorSize
			}
			plain0 := make([]byte, makeiv.BlockSize())
			binary.LittleEndian.PutUint64(plain0, uint64(ivValue))
			iv0 := make([]byte, makeiv.BlockSize())
			makeiv.Encrypt(iv0, plain0)
			cipher := cipher.NewCBCEncrypter(block, iv0)
			cipher.CryptBlocks(ciphertext[processed:processed+blockLeft], plaintext[processed:processed+blockLeft])
		}
	case "xts-plain":
		cipher, err := xts.NewCipher(newBlockCipher, key)
		if err != nil {
			return nil, fmt.Errorf("initializing encryption: %w", err)
		}
		for processed := 0; processed < len(plaintext); processed += sectorSize {
			blockLeft := sectorSize
			if processed+blockLeft > len(plaintext) {
				blockLeft = len(plaintext) - processed
			}
			sector := uint64(processed/sectorSize + ivTweak)
			if bulk { // iv_large_sectors is not being used
				sector *= uint64(sectorSize / V1SectorSize)
			}
			sector = sector % 0x100000000
			cipher.Encrypt(ciphertext[processed:processed+blockLeft], plaintext[processed:processed+blockLeft], sector)
		}
	case "xts-plain64":
		cipher, err := xts.NewCipher(newBlockCipher, key)
		if err != nil {
			return nil, fmt.Errorf("initializing encryption: %w", err)
		}
		for processed := 0; processed < len(plaintext); processed += sectorSize {
			blockLeft := sectorSize
			if processed+blockLeft > len(plaintext) {
				blockLeft = len(plaintext) - processed
			}
			sector := uint64(processed/sectorSize + ivTweak)
			if bulk { // iv_large_sectors is not being used
				sector *= uint64(sectorSize / V1SectorSize)
			}
			cipher.Encrypt(ciphertext[processed:processed+blockLeft], plaintext[processed:processed+blockLeft], sector)
		}
	default:
		return nil, fmt.Errorf("unsupported cipher mode %s", cipherMode)
	}

	if err != nil {
		return nil, fmt.Errorf("cipher error: %w", err)
	}
	return ciphertext, nil
}

func v1decrypt(cipherName, cipherMode string, ivTweak int, key []byte, ciphertext []byte, sectorSize int, bulk bool) ([]byte, error) {
	var err error
	var newBlockCipher func([]byte) (cipher.Block, error)
	plaintext := make([]byte, len(ciphertext))

	switch cipherName {
	case "aes":
		newBlockCipher = aes.NewCipher
	case "twofish":
		newBlockCipher = func(key []byte) (cipher.Block, error) { return twofish.NewCipher(key) }
	case "cast5":
		newBlockCipher = func(key []byte) (cipher.Block, error) { return cast5.NewCipher(key) }
	case "serpent":
		newBlockCipher = serpent.NewCipher
	default:
		return nil, fmt.Errorf("unsupported cipher %s", cipherName)
	}
	if sectorSize == 0 {
		sectorSize = V1SectorSize
	}
	switch sectorSize {
	default:
		return nil, fmt.Errorf("invalid sector size %d", sectorSize)
	case 512, 1024, 2048, 4096:
	}

	switch cipherMode {
	case "ecb":
		cipher, err := newBlockCipher(key)
		if err != nil {
			return nil, fmt.Errorf("initializing decryption: %w", err)
		}
		for processed := 0; processed < len(ciphertext); processed += cipher.BlockSize() {
			blockLeft := sectorSize
			if processed+blockLeft > len(ciphertext) {
				blockLeft = len(ciphertext) - processed
			}
			cipher.Decrypt(plaintext[processed:processed+blockLeft], ciphertext[processed:processed+blockLeft])
		}
	case "cbc-plain":
		block, err := newBlockCipher(key)
		if err != nil {
			return nil, fmt.Errorf("initializing decryption: %w", err)
		}
		for processed := 0; processed < len(plaintext); processed += sectorSize {
			blockLeft := sectorSize
			if processed+blockLeft > len(plaintext) {
				blockLeft = len(plaintext) - processed
			}
			ivValue := processed/sectorSize + ivTweak
			if bulk { // iv_large_sectors is not being used
				ivValue *= sectorSize / V1SectorSize
			}
			iv0 := make([]byte, block.BlockSize())
			binary.LittleEndian.PutUint32(iv0, uint32(ivValue))
			cipher := cipher.NewCBCDecrypter(block, iv0)
			cipher.CryptBlocks(plaintext[processed:processed+blockLeft], ciphertext[processed:processed+blockLeft])
		}
	case "cbc-plain64":
		block, err := newBlockCipher(key)
		if err != nil {
			return nil, fmt.Errorf("initializing decryption: %w", err)
		}
		for processed := 0; processed < len(plaintext); processed += sectorSize {
			blockLeft := sectorSize
			if processed+blockLeft > len(plaintext) {
				blockLeft = len(plaintext) - processed
			}
			ivValue := processed/sectorSize + ivTweak
			if bulk { // iv_large_sectors is not being used
				ivValue *= sectorSize / V1SectorSize
			}
			iv0 := make([]byte, block.BlockSize())
			binary.LittleEndian.PutUint64(iv0, uint64(ivValue))
			cipher := cipher.NewCBCDecrypter(block, iv0)
			cipher.CryptBlocks(plaintext[processed:processed+blockLeft], ciphertext[processed:processed+blockLeft])
		}
	case "cbc-essiv:sha256":
		hasherName := strings.TrimPrefix(cipherMode, "cbc-essiv:")
		hasher, err := hasherByName(hasherName)
		if err != nil {
			return nil, fmt.Errorf("initializing decryption using hash %s: %w", hasherName, err)
		}
		h := hasher()
		h.Write(key)
		makeiv, err := newBlockCipher(h.Sum(nil))
		if err != nil {
			return nil, fmt.Errorf("initializing decryption: %w", err)
		}
		block, err := newBlockCipher(key)
		if err != nil {
			return nil, fmt.Errorf("initializing decryption: %w", err)
		}
		for processed := 0; processed < len(plaintext); processed += sectorSize {
			blockLeft := sectorSize
			if processed+blockLeft > len(plaintext) {
				blockLeft = len(plaintext) - processed
			}
			ivValue := (processed/sectorSize + ivTweak)
			if bulk { // iv_large_sectors is not being used
				ivValue *= sectorSize / V1SectorSize
			}
			plain0 := make([]byte, makeiv.BlockSize())
			binary.LittleEndian.PutUint64(plain0, uint64(ivValue))
			iv0 := make([]byte, makeiv.BlockSize())
			makeiv.Encrypt(iv0, plain0)
			cipher := cipher.NewCBCDecrypter(block, iv0)
			cipher.CryptBlocks(plaintext[processed:processed+blockLeft], ciphertext[processed:processed+blockLeft])
		}
	case "xts-plain":
		cipher, err := xts.NewCipher(newBlockCipher, key)
		if err != nil {
			return nil, fmt.Errorf("initializing decryption: %w", err)
		}
		for processed := 0; processed < len(ciphertext); processed += sectorSize {
			blockLeft := sectorSize
			if processed+blockLeft > len(ciphertext) {
				blockLeft = len(ciphertext) - processed
			}
			sector := uint64(processed/sectorSize + ivTweak)
			if bulk { // iv_large_sectors is not being used
				sector *= uint64(sectorSize / V1SectorSize)
			}
			sector = sector % 0x100000000
			cipher.Decrypt(plaintext[processed:processed+blockLeft], ciphertext[processed:processed+blockLeft], sector)
		}
	case "xts-plain64":
		cipher, err := xts.NewCipher(newBlockCipher, key)
		if err != nil {
			return nil, fmt.Errorf("initializing decryption: %w", err)
		}
		for processed := 0; processed < len(ciphertext); processed += sectorSize {
			blockLeft := sectorSize
			if processed+blockLeft > len(ciphertext) {
				blockLeft = len(ciphertext) - processed
			}
			sector := uint64(processed/sectorSize + ivTweak)
			if bulk { // iv_large_sectors is not being used
				sector *= uint64(sectorSize / V1SectorSize)
			}
			cipher.Decrypt(plaintext[processed:processed+blockLeft], ciphertext[processed:processed+blockLeft], sector)
		}
	default:
		return nil, fmt.Errorf("unsupported cipher mode %s", cipherMode)
	}

	if err != nil {
		return nil, fmt.Errorf("cipher error: %w", err)
	}
	return plaintext, nil
}

func v2encrypt(cipherSuite string, ivTweak int, key []byte, ciphertext []byte, sectorSize int, bulk bool) ([]byte, error) {
	var cipherName, cipherMode string
	switch {
	default:
		cipherSpec := strings.SplitN(cipherSuite, "-", 2)
		if len(cipherSpec) < 2 {
			return nil, fmt.Errorf("unrecognized cipher suite %q", cipherSuite)
		}
		cipherName = cipherSpec[0]
		cipherMode = cipherSpec[1]
	}
	return v1encrypt(cipherName, cipherMode, ivTweak, key, ciphertext, sectorSize, bulk)
}

func v2decrypt(cipherSuite string, ivTweak int, key []byte, ciphertext []byte, sectorSize int, bulk bool) ([]byte, error) {
	var cipherName, cipherMode string
	switch {
	default:
		cipherSpec := strings.SplitN(cipherSuite, "-", 2)
		if len(cipherSpec) < 2 {
			return nil, fmt.Errorf("unrecognized cipher suite %q", cipherSuite)
		}
		cipherName = cipherSpec[0]
		cipherMode = cipherSpec[1]
	}
	return v1decrypt(cipherName, cipherMode, ivTweak, key, ciphertext, sectorSize, bulk)
}

func diffuse(key []byte, h hash.Hash) []byte {
	sum := make([]byte, len(key))
	counter := uint32(0)
	for summed := 0; summed < len(key); summed += h.Size() {
		h.Reset()
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], counter)
		h.Write(buf[:])
		needed := len(key) - summed
		if needed > h.Size() {
			needed = h.Size()
		}
		h.Write(key[summed : summed+needed])
		partial := h.Sum(nil)
		copy(sum[summed:summed+needed], partial)
		counter++
	}
	return sum
}

func afMerge(splitKey []byte, h hash.Hash, keysize int, stripes int) ([]byte, error) {
	if len(splitKey) != keysize*stripes {
		return nil, fmt.Errorf("expected %d af bytes, got %d", keysize*stripes, len(splitKey))
	}
	d := make([]byte, keysize)
	for i := 0; i < stripes-1; i++ {
		for j := 0; j < keysize; j++ {
			d[j] = d[j] ^ splitKey[i*keysize+j]
		}
		d = diffuse(d, h)
	}
	for j := 0; j < keysize; j++ {
		d[j] = d[j] ^ splitKey[(stripes-1)*keysize+j]
	}
	return d, nil
}

func afSplit(key []byte, h hash.Hash, stripes int) ([]byte, error) {
	keysize := len(key)
	s := make([]byte, keysize*stripes)
	d := make([]byte, keysize)
	n, err := rand.Read(s[0 : (keysize-1)*stripes])
	if err != nil {
		return nil, err
	}
	if n != (keysize-1)*stripes {
		return nil, fmt.Errorf("short read when attempting to read random data: %d < %d", n, (keysize-1)*stripes)
	}
	for i := 0; i < stripes-1; i++ {
		for j := 0; j < keysize; j++ {
			d[j] = d[j] ^ s[i*keysize+j]
		}
		d = diffuse(d, h)
	}
	for j := 0; j < keysize; j++ {
		s[(stripes-1)*keysize+j] = d[j] ^ key[j]
	}
	return s, nil
}

func roundUpToMultiple(i, factor int) int {
	if i < 0 {
		return 0
	}
	if factor < 1 {
		return i
	}
	return i + ((factor - (i % factor)) % factor)
}

func roundDownToMultiple(i, factor int) int {
	if i < 0 {
		return 0
	}
	if factor < 1 {
		return i
	}
	return i - (i % factor)
}

func hasherByName(name string) (func() hash.Hash, error) {
	switch name {
	case "sha1":
		return sha1.New, nil
	case "sha256":
		return sha256.New, nil
	case "sha512":
		return sha512.New, nil
	case "ripemd160":
		return ripemd160.New, nil
	default:
		return nil, fmt.Errorf("unsupported digest algorithm %q", name)
	}
}

type wrapper struct {
	fn        func(plaintext []byte) ([]byte, error)
	blockSize int
	buf       []byte
	buffered  int
	processed int
	reader    io.Reader
	eof       bool
	writer    io.Writer
}

func (w *wrapper) partialWrite() error {
	if w.buffered-w.processed >= w.blockSize {
		toProcess := roundDownToMultiple(w.buffered-w.processed, w.blockSize)
		processed, err := w.fn(w.buf[w.processed : w.processed+toProcess])
		if err != nil {
			return err
		}
		nProcessed := copy(w.buf[w.processed:], processed)
		w.processed += nProcessed
	}
	if w.processed >= w.blockSize {
		nWritten, err := w.writer.Write(w.buf[:w.processed])
		if err != nil {
			return err
		}
		copy(w.buf, w.buf[nWritten:w.buffered])
		w.buffered -= nWritten
		w.processed -= nWritten
		if w.processed != 0 {
			return fmt.Errorf("short write: %d != %d", nWritten, nWritten+w.processed)
		}
	}
	return nil
}

func (w *wrapper) Write(buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		nBuffered := copy(w.buf[w.buffered:], buf[n:])
		w.buffered += nBuffered
		n += nBuffered
		if err := w.partialWrite(); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (w *wrapper) Read(buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		if !w.eof {
			nRead, err := w.reader.Read(w.buf[w.buffered:])
			if err != nil {
				if !errors.Is(err, io.EOF) {
					w.buffered += nRead
					return n, err
				}
				w.eof = true
			}
			w.buffered += nRead
		}
		if w.buffered == 0 && w.eof {
			return n, io.EOF
		}
		if w.buffered-w.processed >= w.blockSize {
			toProcess := roundDownToMultiple(w.buffered-w.processed, w.blockSize)
			processed, err := w.fn(w.buf[w.processed : w.processed+toProcess])
			if err != nil {
				return n, err
			}
			nProcessed := copy(w.buf[w.processed:], processed)
			w.processed += nProcessed
		}
		nRead := copy(buf[n:], w.buf[:w.processed])
		n += nRead
		copy(w.buf, w.buf[nRead:w.buffered])
		w.processed -= nRead
		w.buffered -= nRead
		if w.buffered-w.processed < w.blockSize {
			break
		}
	}
	return n, nil
}

func (w *wrapper) Close() error {
	if w.writer != nil {
		if w.buffered%w.blockSize != 0 {
			nPadding := w.blockSize - w.buffered%w.blockSize
			nWritten, err := w.Write(make([]byte, nPadding))
			if err != nil {
				return fmt.Errorf("flushing write: %v", err)
			}
			if nWritten < nPadding {
				return fmt.Errorf("flushing write: %d != %d", nPadding, nWritten)
			}
		}
	}
	return nil
}

// EncryptWriter creates an io.WriteCloser which buffers writes through an
// encryption function, transforming and writing multiples of the blockSize.
// After writing a final block, the returned writer should be closed.
// If only a partial block has been written when Close() is called, a final
// block with its length padded with zero bytes will be transformed and
// written.
func EncryptWriter(fn func(plaintext []byte) ([]byte, error), writer io.Writer, blockSize int) io.WriteCloser {
	bufferSize := roundUpToMultiple(1024*1024, blockSize)
	return &wrapper{fn: fn, blockSize: blockSize, buf: make([]byte, bufferSize), writer: writer}
}

// DecryptReader creates an io.ReadCloser which buffers reads through a
// decryption function, decrypting and returning multiples of the blockSize
// until it reaches the end of the file.  When data will no longer be read, the
// returned reader should be closed.
func DecryptReader(fn func(ciphertext []byte) ([]byte, error), reader io.Reader, blockSize int) io.ReadCloser {
	bufferSize := roundUpToMultiple(1024*1024, blockSize)
	return &wrapper{fn: fn, blockSize: blockSize, buf: make([]byte, bufferSize), reader: reader}
}
