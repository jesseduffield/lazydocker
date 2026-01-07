package luksy

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

// EncryptV1 prepares to encrypt data using one or more passwords and the
// specified cipher (or a default, if the specified cipher is "").
//
// Returns a fixed LUKSv1 header which contains keying information, a function
// which will encrypt blocks of data in succession, and the size of chunks of
// data that it expects.
func EncryptV1(password []string, cipher string) ([]byte, func([]byte) ([]byte, error), int, error) {
	if len(password) == 0 {
		return nil, nil, -1, errors.New("at least one password is required")
	}
	if len(password) > v1NumKeys {
		return nil, nil, -1, fmt.Errorf("attempted to use %d passwords, only %d possible", len(password), v1NumKeys)
	}
	if cipher == "" {
		cipher = "aes-xts-plain64"
	}

	salt := make([]byte, v1SaltSize)
	n, err := rand.Read(salt)
	if err != nil {
		return nil, nil, -1, fmt.Errorf("reading random data: %w", err)
	}
	if n != len(salt) {
		return nil, nil, -1, errors.New("short read")
	}

	cipherSpec := strings.SplitN(cipher, "-", 3)
	if len(cipherSpec) != 3 || len(cipherSpec[0]) == 0 || len(cipherSpec[1]) == 0 || len(cipherSpec[2]) == 0 {
		return nil, nil, -1, fmt.Errorf("invalid cipher %q", cipher)
	}

	var h V1Header
	if err := h.SetMagic(V1Magic); err != nil {
		return nil, nil, -1, fmt.Errorf("setting magic to v1: %w", err)
	}
	if err := h.SetVersion(1); err != nil {
		return nil, nil, -1, fmt.Errorf("setting version to 1: %w", err)
	}
	h.SetCipherName(cipherSpec[0])
	h.SetCipherMode(cipherSpec[1] + "-" + cipherSpec[2])
	h.SetHashSpec("sha256")
	h.SetKeyBytes(32)
	if cipherSpec[1] == "xts" {
		h.SetKeyBytes(64)
	}
	h.SetMKDigestSalt(salt)
	h.SetMKDigestIter(V1Stripes)
	h.SetUUID(uuid.NewString())

	mkey := make([]byte, h.KeyBytes())
	n, err = rand.Read(mkey)
	if err != nil {
		return nil, nil, -1, fmt.Errorf("reading random data: %w", err)
	}
	if n != len(mkey) {
		return nil, nil, -1, errors.New("short read")
	}

	hasher, err := hasherByName(h.HashSpec())
	if err != nil {
		return nil, nil, -1, errors.New("internal error")
	}

	mkdigest := pbkdf2.Key(mkey, h.MKDigestSalt(), int(h.MKDigestIter()), v1DigestSize, hasher)
	h.SetMKDigest(mkdigest)

	headerLength := roundUpToMultiple(v1HeaderStructSize, V1AlignKeyslots)
	iterations := IterationsPBKDF2(salt, int(h.KeyBytes()), hasher)
	var stripes [][]byte
	ksSalt := make([]byte, v1KeySlotSaltLength)
	for i := 0; i < v1NumKeys; i++ {
		n, err = rand.Read(ksSalt)
		if err != nil {
			return nil, nil, -1, fmt.Errorf("reading random data: %w", err)
		}
		if n != len(ksSalt) {
			return nil, nil, -1, errors.New("short read")
		}
		var keyslot V1KeySlot
		keyslot.SetActive(i < len(password))
		keyslot.SetIterations(uint32(iterations))
		keyslot.SetStripes(V1Stripes)
		keyslot.SetKeySlotSalt(ksSalt)
		if i < len(password) {
			splitKey, err := afSplit(mkey, hasher(), int(h.MKDigestIter()))
			if err != nil {
				return nil, nil, -1, fmt.Errorf("splitting key: %w", err)
			}
			passwordDerived := pbkdf2.Key([]byte(password[i]), keyslot.KeySlotSalt(), int(keyslot.Iterations()), int(h.KeyBytes()), hasher)
			striped, err := v1encrypt(h.CipherName(), h.CipherMode(), 0, passwordDerived, splitKey, V1SectorSize, false)
			if err != nil {
				return nil, nil, -1, fmt.Errorf("encrypting split key with password: %w", err)
			}
			if len(striped) != len(mkey)*int(keyslot.Stripes()) {
				return nil, nil, -1, fmt.Errorf("internal error: got %d stripe bytes, expected %d", len(striped), len(mkey)*int(keyslot.Stripes()))
			}
			stripes = append(stripes, striped)
		}
		keyslot.SetKeyMaterialOffset(uint32(headerLength / V1SectorSize))
		if err := h.SetKeySlot(i, keyslot); err != nil {
			return nil, nil, -1, fmt.Errorf("internal error: setting value for key slot %d: %w", i, err)
		}
		headerLength += len(mkey) * int(keyslot.Stripes())
		headerLength = roundUpToMultiple(headerLength, V1AlignKeyslots)
	}
	headerLength = roundUpToMultiple(headerLength, V1SectorSize)

	h.SetPayloadOffset(uint32(headerLength / V1SectorSize))
	head := make([]byte, headerLength)
	offset := copy(head, h[:])
	offset = roundUpToMultiple(offset, V1AlignKeyslots)
	for _, stripe := range stripes {
		copy(head[offset:], stripe)
		offset = roundUpToMultiple(offset+len(stripe), V1AlignKeyslots)
	}
	ivTweak := 0
	encryptStream := func(plaintext []byte) ([]byte, error) {
		ciphertext, err := v1encrypt(h.CipherName(), h.CipherMode(), ivTweak, mkey, plaintext, V1SectorSize, true)
		ivTweak += len(plaintext) / V1SectorSize
		return ciphertext, err
	}
	return head, encryptStream, V1SectorSize, nil
}

// EncryptV2 prepares to encrypt data using one or more passwords and the
// specified cipher (or a default, if the specified cipher is "").
//
// Returns a fixed LUKSv2 header which contains keying information, a
// function which will encrypt blocks of data in succession, and the size of
// chunks of data that it expects.
func EncryptV2(password []string, cipher string, payloadSectorSize int) ([]byte, func([]byte) ([]byte, error), int, error) {
	if len(password) == 0 {
		return nil, nil, -1, errors.New("at least one password is required")
	}
	if cipher == "" {
		cipher = "aes-xts-plain64"
	}
	cipherSpec := strings.SplitN(cipher, "-", 3)
	if len(cipherSpec) != 3 || len(cipherSpec[0]) == 0 || len(cipherSpec[1]) == 0 || len(cipherSpec[2]) == 0 {
		return nil, nil, -1, fmt.Errorf("invalid cipher %q", cipher)
	}
	if payloadSectorSize == 0 {
		payloadSectorSize = V2SectorSize
	}
	switch payloadSectorSize {
	default:
		return nil, nil, -1, fmt.Errorf("invalid sector size %d", payloadSectorSize)
	case 512, 1024, 2048, 4096:
	}

	headerSalts := make([]byte, v1SaltSize*3)
	n, err := rand.Read(headerSalts)
	if err != nil {
		return nil, nil, -1, err
	}
	if n != len(headerSalts) {
		return nil, nil, -1, errors.New("short read")
	}
	hSalt1 := headerSalts[:v1SaltSize]
	hSalt2 := headerSalts[v1SaltSize : v1SaltSize*2]
	mkeySalt := headerSalts[v1SaltSize*2:]

	roundHeaderSize := func(size int) (int, error) {
		switch {
		case size < 0x4000:
			return 0x4000, nil
		case size < 0x8000:
			return 0x8000, nil
		case size < 0x10000:
			return 0x10000, nil
		case size < 0x20000:
			return 0x20000, nil
		case size < 0x40000:
			return 0x40000, nil
		case size < 0x80000:
			return 0x80000, nil
		case size < 0x100000:
			return 0x100000, nil
		case size < 0x200000:
			return 0x200000, nil
		case size < 0x400000:
			return 0x400000, nil
		}
		return 0, fmt.Errorf("internal error: unsupported header size %d", size)
	}

	var h1, h2 V2Header
	if err := h1.SetMagic(V2Magic1); err != nil {
		return nil, nil, -1, fmt.Errorf("setting magic to v2: %w", err)
	}
	if err := h2.SetMagic(V2Magic2); err != nil {
		return nil, nil, -1, fmt.Errorf("setting magic to v2: %w", err)
	}
	if err := h1.SetVersion(2); err != nil {
		return nil, nil, -1, fmt.Errorf("setting version to 2: %w", err)
	}
	if err := h2.SetVersion(2); err != nil {
		return nil, nil, -1, fmt.Errorf("setting version to 2: %w", err)
	}
	h1.SetSequenceID(1)
	h2.SetSequenceID(1)
	h1.SetLabel("")
	h2.SetLabel("")
	h1.SetChecksumAlgorithm("sha256")
	h2.SetChecksumAlgorithm("sha256")
	h1.SetSalt(hSalt1)
	h2.SetSalt(hSalt2)
	uuidString := uuid.NewString()
	h1.SetUUID(uuidString)
	h2.SetUUID(uuidString)
	h1.SetHeaderOffset(0)
	h2.SetHeaderOffset(0)
	h1.SetChecksum(nil)
	h2.SetChecksum(nil)

	mkey := make([]byte, 32)
	if cipherSpec[1] == "xts" {
		mkey = make([]byte, 64)
	}
	n, err = rand.Read(mkey)
	if err != nil {
		return nil, nil, -1, fmt.Errorf("reading random data: %w", err)
	}
	if n != len(mkey) {
		return nil, nil, -1, errors.New("short read")
	}

	tuningSalt := make([]byte, v1SaltSize)
	hasher, err := hasherByName(h1.ChecksumAlgorithm())
	if err != nil {
		return nil, nil, -1, errors.New("internal error")
	}
	iterations := IterationsPBKDF2(tuningSalt, len(mkey), hasher)
	timeCost := 16
	threadsCost := 16
	memoryCost := MemoryCostArgon2(tuningSalt, len(mkey), timeCost, threadsCost)
	priority := V2JSONKeyslotPriorityNormal
	var stripes [][]byte
	var keyslots []V2JSONKeyslot

	mdigest := pbkdf2.Key(mkey, mkeySalt, iterations, len(hasher().Sum([]byte{})), hasher)
	digest0 := V2JSONDigest{
		Type:     "pbkdf2",
		Salt:     mkeySalt,
		Digest:   mdigest,
		Segments: []string{"0"},
		V2JSONDigestPbkdf2: &V2JSONDigestPbkdf2{
			Hash:       h1.ChecksumAlgorithm(),
			Iterations: iterations,
		},
	}

	for i := range password {
		keyslotSalt := make([]byte, v1SaltSize)
		n, err := rand.Read(keyslotSalt)
		if err != nil {
			return nil, nil, -1, err
		}
		if n != len(keyslotSalt) {
			return nil, nil, -1, errors.New("short read")
		}
		key := argon2.Key([]byte(password[i]), keyslotSalt, uint32(timeCost), uint32(memoryCost), uint8(threadsCost), uint32(len(mkey)))
		split, err := afSplit(mkey, hasher(), V2Stripes)
		if err != nil {
			return nil, nil, -1, fmt.Errorf("splitting: %w", err)
		}
		striped, err := v2encrypt(cipher, 0, key, split, V1SectorSize, false)
		if err != nil {
			return nil, nil, -1, fmt.Errorf("encrypting: %w", err)
		}
		stripes = append(stripes, striped)
		keyslot := V2JSONKeyslot{
			Type:    "luks2",
			KeySize: len(mkey),
			Area: V2JSONArea{
				Type:   "raw",
				Offset: 10000000, // gets updated later
				Size:   int64(roundUpToMultiple(len(striped), V2AlignKeyslots)),
				V2JSONAreaRaw: &V2JSONAreaRaw{
					Encryption: cipher,
					KeySize:    len(key),
				},
			},
			Priority: &priority,
			V2JSONKeyslotLUKS2: &V2JSONKeyslotLUKS2{
				AF: V2JSONAF{
					Type: "luks1",
					V2JSONAFLUKS1: &V2JSONAFLUKS1{
						Stripes: V2Stripes,
						Hash:    h1.ChecksumAlgorithm(),
					},
				},
				Kdf: V2JSONKdf{
					Type: "argon2i",
					Salt: keyslotSalt,
					V2JSONKdfArgon2i: &V2JSONKdfArgon2i{
						Time:   timeCost,
						Memory: memoryCost,
						CPUs:   threadsCost,
					},
				},
			},
		}
		keyslots = append(keyslots, keyslot)
		digest0.Keyslots = append(digest0.Keyslots, strconv.Itoa(i))
	}

	segment0 := V2JSONSegment{
		Type:   "crypt",
		Offset: "10000000", // gets updated later
		Size:   "dynamic",
		V2JSONSegmentCrypt: &V2JSONSegmentCrypt{
			IVTweak:    0,
			Encryption: cipher,
			SectorSize: payloadSectorSize,
		},
	}

	j := V2JSON{
		Config:   V2JSONConfig{},
		Keyslots: map[string]V2JSONKeyslot{},
		Digests:  map[string]V2JSONDigest{},
		Segments: map[string]V2JSONSegment{},
		Tokens:   map[string]V2JSONToken{},
	}
rebuild:
	j.Digests["0"] = digest0
	j.Segments["0"] = segment0
	encodedJSON, err := json.Marshal(j)
	if err != nil {
		return nil, nil, -1, err
	}
	headerPlusPaddedJsonSize, err := roundHeaderSize(int(V2SectorSize) /* binary header */ + len(encodedJSON) + 1)
	if err != nil {
		return nil, nil, -1, err
	}
	if j.Config.JsonSize != headerPlusPaddedJsonSize-V2SectorSize {
		j.Config.JsonSize = headerPlusPaddedJsonSize - V2SectorSize
		goto rebuild
	}

	if h1.HeaderSize() != uint64(headerPlusPaddedJsonSize) {
		h1.SetHeaderSize(uint64(headerPlusPaddedJsonSize))
		h2.SetHeaderSize(uint64(headerPlusPaddedJsonSize))
		h1.SetHeaderOffset(0)
		h2.SetHeaderOffset(uint64(headerPlusPaddedJsonSize))
		goto rebuild
	}

	keyslotsOffset := h2.HeaderOffset() * 2
	maxKeys := len(password)
	if maxKeys < 64 {
		maxKeys = 64
	}
	for i := 0; i < len(password); i++ {
		oldOffset := keyslots[i].Area.Offset
		keyslots[i].Area.Offset = int64(keyslotsOffset) + int64(roundUpToMultiple(len(mkey)*V2Stripes, V2AlignKeyslots))*int64(i)
		j.Keyslots[strconv.Itoa(i)] = keyslots[i]
		if keyslots[i].Area.Offset != oldOffset {
			goto rebuild
		}
	}
	keyslotsSize := roundUpToMultiple(len(mkey)*V2Stripes, V2AlignKeyslots) * maxKeys
	if j.Config.KeyslotsSize != keyslotsSize {
		j.Config.KeyslotsSize = keyslotsSize
		goto rebuild
	}

	segmentOffsetInt := roundUpToMultiple(int(keyslotsOffset)+j.Config.KeyslotsSize, V2SectorSize)
	segmentOffset := strconv.Itoa(segmentOffsetInt)
	if segment0.Offset != segmentOffset {
		segment0.Offset = segmentOffset
		goto rebuild
	}

	d1 := hasher()
	h1.SetChecksum(nil)
	d1.Write(h1[:])
	d1.Write(encodedJSON)
	zeropad := make([]byte, headerPlusPaddedJsonSize-len(h1)-len(encodedJSON))
	d1.Write(zeropad)
	h1.SetChecksum(d1.Sum(nil))
	d2 := hasher()
	h2.SetChecksum(nil)
	d2.Write(h2[:])
	d2.Write(encodedJSON)
	d1.Write(zeropad)
	h2.SetChecksum(d2.Sum(nil))

	head := make([]byte, segmentOffsetInt)
	copy(head, h1[:])
	copy(head[V2SectorSize:], encodedJSON)
	copy(head[h2.HeaderOffset():], h2[:])
	copy(head[h2.HeaderOffset()+V2SectorSize:], encodedJSON)
	for i := 0; i < len(password); i++ {
		iAsString := strconv.Itoa(i)
		copy(head[j.Keyslots[iAsString].Area.Offset:], stripes[i])
	}
	ivTweak := 0
	encryptStream := func(plaintext []byte) ([]byte, error) {
		ciphertext, err := v2encrypt(cipher, ivTweak, mkey, plaintext, payloadSectorSize, true)
		ivTweak += len(plaintext) / payloadSectorSize
		return ciphertext, err
	}
	return head, encryptStream, segment0.SectorSize, nil
}
