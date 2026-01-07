// Copyright (c) 2018-2024, Sylabs Inc. All rights reserved.
// Copyright (c) 2017, SingularityWare, LLC. All rights reserved.
// Copyright (c) 2017, Yannick Cote <yhcote@gmail.com> All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package sif

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"encoding"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// rawDescriptor represents an on-disk object descriptor.
type rawDescriptor struct {
	DataType        DataType
	Used            bool
	ID              uint32
	GroupID         uint32
	LinkedID        uint32
	Offset          int64
	Size            int64
	SizeWithPadding int64

	CreatedAt  int64
	ModifiedAt int64
	UID        int64 // Deprecated: UID exists for historical compatibility and should not be used.
	GID        int64 // Deprecated: GID exists for historical compatibility and should not be used.
	Name       [descrNameLen]byte
	Extra      [descrMaxPrivLen]byte
}

// partition represents the SIF partition data object descriptor.
type partition struct {
	Fstype   FSType
	Parttype PartType
	Arch     archType
}

// MarshalBinary encodes p into binary format.
func (p partition) MarshalBinary() ([]byte, error) {
	return binaryMarshaler{p}.MarshalBinary()
}

// signature represents the SIF signature data object descriptor.
type signature struct {
	Hashtype hashType
	Entity   [descrEntityLen]byte
}

// cryptoMessage represents the SIF crypto message object descriptor.
type cryptoMessage struct {
	Formattype  FormatType
	Messagetype MessageType
}

// sbom represents the SIF SBOM data object descriptor.
type sbom struct {
	Format SBOMFormat
}

// ociBlob represents the OCI Blob data object descriptor.
type ociBlob struct {
	hasher hash.Hash // accumulates hash while writing blob.
	digest v1.Hash
}

// newOCIBlobDigest returns a new ociBlob, that accumulates the digest of an OCI blob as it is
// read. The caller should take care to ensure that the entire contents of the blob have been
// written to the returned ociBlob prior to calling MarshalBinary.
func newOCIBlobDigest() *ociBlob {
	return &ociBlob{
		hasher: sha256.New(),
		digest: v1.Hash{
			Algorithm: "sha256",
		},
	}
}

// MarshalBinary encodes ob into binary format.
func (ob *ociBlob) MarshalBinary() ([]byte, error) {
	if ob.digest.Hex == "" {
		ob.digest.Hex = hex.EncodeToString(ob.hasher.Sum(nil))
	}

	return ob.digest.MarshalText()
}

// UnmarshalBinary decodes b into ob.
func (ob *ociBlob) UnmarshalBinary(b []byte) error {
	if before, _, ok := bytes.Cut(b, []byte{0x00}); ok {
		b = before
	}

	return ob.digest.UnmarshalText(b)
}

// The binaryMarshaler type is an adapter that allows a type suitable for use with the
// encoding/binary package to be used as an encoding.BinaryMarshaler.
type binaryMarshaler struct{ any }

// MarshalBinary encodes m into binary format.
func (m binaryMarshaler) MarshalBinary() ([]byte, error) {
	var b bytes.Buffer
	err := binary.Write(&b, binary.LittleEndian, m.any)
	return b.Bytes(), err
}

// The binaryUnmarshaler type is an adapter that allows a type suitable for use with the
// encoding/binary package to be used as an encoding.BinaryUnmarshaler.
type binaryUnmarshaler struct{ any }

// UnmarshalBinary decodes b into u.
func (u binaryUnmarshaler) UnmarshalBinary(b []byte) error {
	return binary.Read(bytes.NewReader(b), binary.LittleEndian, u.any)
}

var errNameTooLarge = errors.New("name value too large")

// setName encodes name into the name field of d.
func (d *rawDescriptor) setName(name string) error {
	if len(name) > len(d.Name) {
		return errNameTooLarge
	}

	for i := copy(d.Name[:], name); i < len(d.Name); i++ {
		d.Name[i] = 0
	}

	return nil
}

var errExtraTooLarge = errors.New("extra value too large")

// setExtra marshals metadata from md into the "extra" field of d.
func (d *rawDescriptor) setExtra(md encoding.BinaryMarshaler) error {
	if md == nil {
		return nil
	}

	extra, err := md.MarshalBinary()
	if err != nil {
		return err
	}

	if len(extra) > len(d.Extra) {
		return errExtraTooLarge
	}

	for i := copy(d.Extra[:], extra); i < len(d.Extra); i++ {
		d.Extra[i] = 0
	}

	return nil
}

// getExtra unmarshals metadata from the "extra" field of d into md.
func (d *rawDescriptor) getExtra(md encoding.BinaryUnmarshaler) error {
	return md.UnmarshalBinary(d.Extra[:])
}

// getPartitionMetadata gets metadata for a partition data object.
func (d rawDescriptor) getPartitionMetadata() (FSType, PartType, string, error) {
	if got, want := d.DataType, DataPartition; got != want {
		return 0, 0, "", &unexpectedDataTypeError{got, []DataType{want}}
	}

	var p partition

	if err := d.getExtra(binaryUnmarshaler{&p}); err != nil {
		return 0, 0, "", err
	}

	return p.Fstype, p.Parttype, p.Arch.GoArch(), nil
}

// isPartitionOfType returns true if d is a partition data object of type pt.
func (d rawDescriptor) isPartitionOfType(pt PartType) bool {
	_, t, _, err := d.getPartitionMetadata()
	if err != nil {
		return false
	}
	return t == pt
}

// Descriptor represents the SIF descriptor type.
type Descriptor struct {
	r io.ReaderAt // Backing storage.

	raw rawDescriptor // Raw descriptor from image.

	relativeID uint32 // ID relative to minimum ID of object group.
}

// DataType returns the type of data object.
func (d Descriptor) DataType() DataType { return d.raw.DataType }

// ID returns the data object ID of d.
func (d Descriptor) ID() uint32 { return d.raw.ID }

// GroupID returns the data object group ID of d, or zero if d is not part of a data object
// group.
func (d Descriptor) GroupID() uint32 { return d.raw.GroupID &^ descrGroupMask }

// LinkedID returns the object/group ID d is linked to, or zero if d does not contain a linked
// ID. If isGroup is true, the returned id is an object group ID. Otherwise, the returned id is a
// data object ID.
//
//nolint:nonamedreturns // Named returns effective as documentation.
func (d Descriptor) LinkedID() (id uint32, isGroup bool) {
	return d.raw.LinkedID &^ descrGroupMask, d.raw.LinkedID&descrGroupMask == descrGroupMask
}

// Offset returns the offset of the data object.
func (d Descriptor) Offset() int64 { return d.raw.Offset }

// Size returns the data object size.
func (d Descriptor) Size() int64 { return d.raw.Size }

// CreatedAt returns the creation time of the data object.
func (d Descriptor) CreatedAt() time.Time { return time.Unix(d.raw.CreatedAt, 0) }

// ModifiedAt returns the modification time of the data object.
func (d Descriptor) ModifiedAt() time.Time { return time.Unix(d.raw.ModifiedAt, 0) }

// Name returns the name of the data object.
func (d Descriptor) Name() string { return strings.TrimRight(string(d.raw.Name[:]), "\000") }

// GetMetadata unmarshals metadata from the "extra" field of d into md.
func (d Descriptor) GetMetadata(md encoding.BinaryUnmarshaler) error {
	if err := d.raw.getExtra(md); err != nil {
		return fmt.Errorf("%w", err)
	}
	return nil
}

// PartitionMetadata gets metadata for a partition data object.
//
//nolint:nonamedreturns // Named returns effective as documentation.
func (d Descriptor) PartitionMetadata() (fs FSType, pt PartType, arch string, err error) {
	fs, pt, arch, err = d.raw.getPartitionMetadata()
	if err != nil {
		return 0, 0, "", fmt.Errorf("%w", err)
	}
	return fs, pt, arch, err
}

var errHashUnsupported = errors.New("hash algorithm unsupported")

// getHashType converts ht into a crypto.Hash.
func getHashType(ht hashType) (crypto.Hash, error) {
	switch ht {
	case hashSHA256:
		return crypto.SHA256, nil
	case hashSHA384:
		return crypto.SHA384, nil
	case hashSHA512:
		return crypto.SHA512, nil
	case hashBLAKE2S:
		return crypto.BLAKE2s_256, nil
	case hashBLAKE2B:
		return crypto.BLAKE2b_256, nil
	}
	return 0, errHashUnsupported
}

// SignatureMetadata gets metadata for a signature data object.
//
//nolint:nonamedreturns // Named returns effective as documentation.
func (d Descriptor) SignatureMetadata() (ht crypto.Hash, fp []byte, err error) {
	if got, want := d.raw.DataType, DataSignature; got != want {
		return ht, fp, &unexpectedDataTypeError{got, []DataType{want}}
	}

	var s signature

	if err := d.raw.getExtra(binaryUnmarshaler{&s}); err != nil {
		return ht, fp, fmt.Errorf("%w", err)
	}

	if ht, err = getHashType(s.Hashtype); err != nil {
		return ht, fp, fmt.Errorf("%w", err)
	}

	fp = make([]byte, 20)

	if bytes.Equal(s.Entity[:len(fp)], fp) {
		return ht, nil, nil // Fingerprint not present.
	}

	copy(fp, s.Entity[:])

	return ht, fp, nil
}

// CryptoMessageMetadata gets metadata for a crypto message data object.
func (d Descriptor) CryptoMessageMetadata() (FormatType, MessageType, error) {
	if got, want := d.raw.DataType, DataCryptoMessage; got != want {
		return 0, 0, &unexpectedDataTypeError{got, []DataType{want}}
	}

	var m cryptoMessage

	if err := d.raw.getExtra(binaryUnmarshaler{&m}); err != nil {
		return 0, 0, fmt.Errorf("%w", err)
	}

	return m.Formattype, m.Messagetype, nil
}

// SBOMMetadata gets metadata for a SBOM data object.
func (d Descriptor) SBOMMetadata() (SBOMFormat, error) {
	if got, want := d.raw.DataType, DataSBOM; got != want {
		return 0, &unexpectedDataTypeError{got, []DataType{want}}
	}

	var s sbom

	if err := d.raw.getExtra(binaryUnmarshaler{&s}); err != nil {
		return 0, fmt.Errorf("%w", err)
	}

	return s.Format, nil
}

// OCIBlobDigest returns the digest for a OCI blob object.
func (d Descriptor) OCIBlobDigest() (v1.Hash, error) {
	if got := d.raw.DataType; got != DataOCIRootIndex && got != DataOCIBlob {
		return v1.Hash{}, &unexpectedDataTypeError{got, []DataType{DataOCIRootIndex, DataOCIBlob}}
	}

	var o ociBlob

	if err := d.raw.getExtra(&o); err != nil {
		return v1.Hash{}, fmt.Errorf("%w", err)
	}

	return o.digest, nil
}

// GetData returns the data object associated with descriptor d.
func (d Descriptor) GetData() ([]byte, error) {
	b := make([]byte, d.raw.Size)
	if _, err := io.ReadFull(d.GetReader(), b); err != nil {
		return nil, err
	}
	return b, nil
}

// GetReader returns a io.Reader that reads the data object associated with descriptor d.
func (d Descriptor) GetReader() io.Reader {
	return io.NewSectionReader(d.r, d.raw.Offset, d.raw.Size)
}

// GetIntegrityReader returns an io.Reader that reads the integrity-protected fields from d.
func (d Descriptor) GetIntegrityReader() io.Reader {
	fields := []interface{}{
		d.raw.DataType,
		d.raw.Used,
		d.relativeID,
		d.raw.LinkedID,
		d.raw.Size,
		d.raw.CreatedAt,
		d.raw.UID,
		d.raw.GID,
	}

	// Encode endian-sensitive fields.
	data := bytes.Buffer{}
	for _, f := range fields {
		if err := binary.Write(&data, binary.LittleEndian, f); err != nil {
			panic(err) // (*bytes.Buffer).Write() is documented as always returning a nil error.
		}
	}

	return io.MultiReader(
		&data,
		bytes.NewReader(d.raw.Name[:]),
		bytes.NewReader(d.raw.Extra[:]),
	)
}
