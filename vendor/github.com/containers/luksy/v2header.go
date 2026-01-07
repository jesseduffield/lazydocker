package luksy

import (
	"fmt"
	"strings"
	"syscall"
)

type V2Header [4096]uint8

const (
	// Mostly verbatim from LUKS2 On-Disk Format Specification version 1.1.1
	V2Magic1                  = V1Magic
	V2Magic2                  = "SKUL\xba\xbe"
	v2MagicStart              = 0
	v2MagicLength             = 6
	v2VersionStart            = v2MagicStart + v2MagicLength
	v2VersionLength           = 2
	v2HeaderSizeStart         = v2VersionStart + v2VersionLength
	v2HeaderSizeLength        = 8
	v2SequenceIDStart         = v2HeaderSizeStart + v2HeaderSizeLength
	v2SequenceIDLength        = 8
	v2LabelStart              = v2SequenceIDStart + v2SequenceIDLength
	v2LabelLength             = 48
	v2ChecksumAlgorithmStart  = v2LabelStart + v2LabelLength
	v2ChecksumAlgorithmLength = 32
	v2SaltStart               = v2ChecksumAlgorithmStart + v2ChecksumAlgorithmLength
	v2SaltLength              = 64
	v2UUIDStart               = v2SaltStart + v2SaltLength
	v2UUIDLength              = 40
	v2SubsystemStart          = v2UUIDStart + v2UUIDLength
	v2SubsystemLength         = v2LabelLength
	v2HeaderOffsetStart       = v2SubsystemStart + v2SubsystemLength
	v2HeaderOffsetLength      = 8
	v2Padding1Start           = v2HeaderOffsetStart + v2HeaderOffsetLength
	v2Padding1Length          = 184
	v2ChecksumStart           = v2Padding1Start + v2Padding1Length
	v2ChecksumLength          = 64
	v2Padding4096Start        = v2ChecksumStart + v2ChecksumLength
	v2Padding4096Length       = 7 * 512
	v2HeaderStructSize        = v2Padding4096Start + v2Padding4096Length

	V2Stripes       = 4000
	V2AlignKeyslots = 4096
	V2SectorSize    = 4096
)

func (h V2Header) Magic() string {
	return string(h[v2MagicStart : v2MagicStart+v2MagicLength])
}

func (h *V2Header) SetMagic(magic string) error {
	switch magic {
	case V2Magic1, V2Magic2:
		copy(h[v2MagicStart:v2MagicStart+v2MagicLength], []uint8(magic))
		return nil
	}
	return fmt.Errorf("magic %q not acceptable, only %q and %q are acceptable magic values: %w", magic, V2Magic1, V2Magic2, syscall.EINVAL)
}

func (h V2Header) readu2(offset int) uint16 {
	t := uint16(0)
	for i := 0; i < 2; i++ {
		t = (t << 8) + uint16(h[offset+i])
	}
	return t
}

func (h V2Header) readu8(offset int) uint64 {
	t := uint64(0)
	for i := 0; i < 8; i++ {
		t = (t << 8) + uint64(h[offset+i])
	}
	return t
}

func (h *V2Header) writeu2(offset int, value uint16) {
	t := value
	for i := 0; i < 2; i++ {
		h[offset+1-i] = uint8(uint64(t) & 0xff)
		t >>= 8
	}
}

func (h *V2Header) writeu8(offset int, value uint64) {
	t := value
	for i := 0; i < 8; i++ {
		h[offset+7-i] = uint8(uint64(t) & 0xff)
		t >>= 8
	}
}

func (h V2Header) Version() uint16 {
	return h.readu2(v2VersionStart)
}

func (h *V2Header) SetVersion(version uint16) error {
	switch version {
	case 2:
		h.writeu2(v2VersionStart, version)
		return nil
	}
	return fmt.Errorf("version %d not acceptable, only 2 is an acceptable version: %w", version, syscall.EINVAL)
}

func (h V2Header) HeaderSize() uint64 {
	return h.readu8(v2HeaderSizeStart)
}

func (h *V2Header) SetHeaderSize(size uint64) {
	h.writeu8(v2HeaderSizeStart, size)
}

func (h V2Header) SequenceID() uint64 {
	return h.readu8(v2SequenceIDStart)
}

func (h *V2Header) SetSequenceID(id uint64) {
	h.writeu8(v2SequenceIDStart, id)
}

func trimZeroPad(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool { return r == 0 })
}

func (h V2Header) Label() string {
	return trimZeroPad(string(h[v2LabelStart : v2LabelStart+v2LabelLength]))
}

func (h *V2Header) setZeroString(offset int, value string, length int) {
	for len(value) < length {
		value = value + "\000"
	}
	copy(h[offset:offset+length], []uint8(value))
}

func (h *V2Header) SetLabel(label string) {
	h.setZeroString(v2LabelStart, label, v2LabelLength)
}

func (h V2Header) ChecksumAlgorithm() string {
	return trimZeroPad(string(h[v2ChecksumAlgorithmStart : v2ChecksumAlgorithmStart+v2ChecksumAlgorithmLength]))
}

func (h *V2Header) SetChecksumAlgorithm(alg string) {
	h.setZeroString(v2ChecksumAlgorithmStart, alg, v2ChecksumAlgorithmLength)
}

func dupInt8(s []uint8) []uint8 {
	c := make([]uint8, len(s))
	copy(c, s)
	return c
}

func (h *V2Header) setInt8(offset int, s []uint8, length int) {
	t := make([]byte, length)
	copy(t, s)
	copy(h[offset:offset+length], t)
}

func (h V2Header) Salt() []uint8 {
	return dupInt8(h[v2SaltStart : v2SaltStart+v2SaltLength])
}

func (h *V2Header) SetSalt(salt []uint8) {
	h.setInt8(v2SaltStart, salt, v2SaltLength)
}

func (h V2Header) UUID() string {
	return trimZeroPad(string(h[v2UUIDStart : v2UUIDStart+v2UUIDLength]))
}

func (h *V2Header) SetUUID(uuid string) {
	h.setZeroString(v2UUIDStart, uuid, v2UUIDLength)
}

func (h V2Header) Subsystem() string {
	return trimZeroPad(string(h[v2SubsystemStart : v2SubsystemStart+v2SubsystemLength]))
}

func (h *V2Header) SetSubsystem(ss string) {
	h.setZeroString(v2SubsystemStart, ss, v2SubsystemLength)
}

func (h V2Header) HeaderOffset() uint64 {
	return h.readu8(v2HeaderOffsetStart)
}

func (h *V2Header) SetHeaderOffset(o uint64) {
	h.writeu8(v2HeaderOffsetStart, o)
}

func (h V2Header) Checksum() []uint8 {
	hasher, err := hasherByName(h.ChecksumAlgorithm())
	if err == nil {
		return dupInt8(h[v2ChecksumStart : v2ChecksumStart+hasher().Size()])
	}
	return dupInt8(h[v2ChecksumStart : v2ChecksumStart+v2ChecksumLength])
}

func (h *V2Header) SetChecksum(sum []uint8) {
	h.setInt8(v2ChecksumStart, sum, v2ChecksumLength)
}
