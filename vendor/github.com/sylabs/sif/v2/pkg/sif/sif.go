// Copyright (c) 2018-2023, Sylabs Inc. All rights reserved.
// Copyright (c) 2017, SingularityWare, LLC. All rights reserved.
// Copyright (c) 2017, Yannick Cote <yhcote@gmail.com> All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

// Package sif implements data structures and routines to create
// and access SIF files.
//
// Layout of a SIF file (example):
//
//	.================================================.
//	| GLOBAL HEADER: Sifheader                       |
//	| - launch: "#!/usr/bin/env..."                  |
//	| - magic: "SIF_MAGIC"                           |
//	| - version: "1"                                 |
//	| - arch: "4"                                    |
//	| - uuid: b2659d4e-bd50-4ea5-bd17-eec5e54f918e   |
//	| - ctime: 1504657553                            |
//	| - mtime: 1504657653                            |
//	| - ndescr: 3                                    |
//	| - descroff: 120                                | --.
//	| - descrlen: 432                                |   |
//	| - dataoff: 4096                                |   |
//	| - datalen: 619362                              |   |
//	|------------------------------------------------| <-'
//	| DESCR[0]: Sifdeffile                           |
//	| - Sifcommon                                    |
//	|   - datatype: DATA_DEFFILE                     |
//	|   - id: 1                                      |
//	|   - groupid: 1                                 |
//	|   - link: NONE                                 |
//	|   - fileoff: 4096                              | --.
//	|   - filelen: 222                               |   |
//	|------------------------------------------------| <-----.
//	| DESCR[1]: Sifpartition                         |   |   |
//	| - Sifcommon                                    |   |   |
//	|   - datatype: DATA_PARTITION                   |   |   |
//	|   - id: 2                                      |   |   |
//	|   - groupid: 1                                 |   |   |
//	|   - link: NONE                                 |   |   |
//	|   - fileoff: 4318                              | ----. |
//	|   - filelen: 618496                            |   | | |
//	| - fstype: Squashfs                             |   | | |
//	| - parttype: System                             |   | | |
//	| - content: Linux                               |   | | |
//	|------------------------------------------------|   | | |
//	| DESCR[2]: Sifsignature                         |   | | |
//	| - Sifcommon                                    |   | | |
//	|   - datatype: DATA_SIGNATURE                   |   | | |
//	|   - id: 3                                      |   | | |
//	|   - groupid: NONE                              |   | | |
//	|   - link: 2                                    | ------'
//	|   - fileoff: 622814                            | ------.
//	|   - filelen: 644                               |   | | |
//	| - hashtype: SHA384                             |   | | |
//	| - entity: @                                    |   | | |
//	|------------------------------------------------| <-' | |
//	| Definition file data                           |     | |
//	| .                                              |     | |
//	| .                                              |     | |
//	| .                                              |     | |
//	|------------------------------------------------| <---' |
//	| File system partition image                    |       |
//	| .                                              |       |
//	| .                                              |       |
//	| .                                              |       |
//	|------------------------------------------------| <-----'
//	| Signed verification data                       |
//	| .                                              |
//	| .                                              |
//	| .                                              |
//	`================================================'
package sif

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

// SIF header constants and quantities.
const (
	hdrLaunchLen  = 32 // len("#!/usr/bin/env... ")
	hdrMagicLen   = 10 // len("SIF_MAGIC")
	hdrVersionLen = 3  // len("99")
)

var hdrMagic = [...]byte{'S', 'I', 'F', '_', 'M', 'A', 'G', 'I', 'C', '\x00'}

// SpecVersion specifies a SIF specification version.
type SpecVersion uint8

func (v SpecVersion) String() string { return fmt.Sprintf("%02d", v) }

// bytes returns the value of b, formatted for direct inclusion in a SIF header.
func (v SpecVersion) bytes() [hdrVersionLen]byte {
	var b [3]byte
	copy(b[:], fmt.Sprintf("%02d", v))
	return b
}

// SIF specification versions.
const (
	version01 SpecVersion = iota + 1
)

// CurrentVersion specifies the current SIF specification version.
const CurrentVersion = version01

const (
	descrGroupMask  = 0xf0000000 // groups start at that offset
	descrEntityLen  = 256        // len("Joe Bloe <jbloe@gmail.com>...")
	descrNameLen    = 128        // descriptor name (string identifier)
	descrMaxPrivLen = 384        // size reserved for descriptor specific data
)

// DataType represents the different SIF data object types stored in the image.
type DataType int32

// List of supported SIF data types.
const (
	DataDeffile       DataType = iota + 0x4001 // definition file data object
	DataEnvVar                                 // environment variables data object
	DataLabels                                 // JSON labels data object
	DataPartition                              // file system data object
	DataSignature                              // signing/verification data object
	DataGenericJSON                            // generic JSON meta-data
	DataGeneric                                // generic / raw data
	DataCryptoMessage                          // cryptographic message data object
	DataSBOM                                   // software bill of materials
	DataOCIRootIndex                           // root OCI index
	DataOCIBlob                                // oci blob data object
)

// String returns a human-readable representation of t.
func (t DataType) String() string {
	switch t {
	case DataDeffile:
		return "Def.FILE"
	case DataEnvVar:
		return "Env.Vars"
	case DataLabels:
		return "JSON.Labels"
	case DataPartition:
		return "FS"
	case DataSignature:
		return "Signature"
	case DataGenericJSON:
		return "JSON.Generic"
	case DataGeneric:
		return "Generic/Raw"
	case DataCryptoMessage:
		return "Cryptographic Message"
	case DataSBOM:
		return "SBOM"
	case DataOCIRootIndex:
		return "OCI.RootIndex"
	case DataOCIBlob:
		return "OCI.Blob"
	}
	return "Unknown"
}

// FSType represents the different SIF file system types found in partition data objects.
type FSType int32

// List of supported file systems.
const (
	FsSquash            FSType = iota + 1 // Squashfs file system, RDONLY
	FsExt3                                // EXT3 file system, RDWR (deprecated)
	FsImmuObj                             // immutable data object archive
	FsRaw                                 // raw data
	FsEncryptedSquashfs                   // Encrypted Squashfs file system, RDONLY
)

// String returns a human-readable representation of t.
func (t FSType) String() string {
	switch t {
	case FsSquash:
		return "Squashfs"
	case FsExt3:
		return "Ext3"
	case FsImmuObj:
		return "Archive"
	case FsRaw:
		return "Raw"
	case FsEncryptedSquashfs:
		return "Encrypted squashfs"
	}
	return "Unknown"
}

// PartType represents the different SIF container partition types (system and data).
type PartType int32

// List of supported partition types.
const (
	PartSystem  PartType = iota + 1 // partition hosts an operating system
	PartPrimSys                     // partition hosts the primary operating system
	PartData                        // partition hosts data only
	PartOverlay                     // partition hosts an overlay
)

// String returns a human-readable representation of t.
func (t PartType) String() string {
	switch t {
	case PartSystem:
		return "System"
	case PartPrimSys:
		return "*System"
	case PartData:
		return "Data"
	case PartOverlay:
		return "Overlay"
	}
	return "Unknown"
}

// hashType represents the different SIF hashing function types used to fingerprint data objects.
type hashType int32

// List of supported hash functions.
const (
	hashSHA256 hashType = iota + 1
	hashSHA384
	hashSHA512
	hashBLAKE2S
	hashBLAKE2B
)

// FormatType represents the different formats used to store cryptographic message objects.
type FormatType int32

// List of supported cryptographic message formats.
const (
	FormatOpenPGP FormatType = iota + 1
	FormatPEM
)

// String returns a human-readable representation of t.
func (t FormatType) String() string {
	switch t {
	case FormatOpenPGP:
		return "OpenPGP"
	case FormatPEM:
		return "PEM"
	}
	return "Unknown"
}

// MessageType represents the different messages stored within cryptographic message objects.
type MessageType int32

// List of supported cryptographic message formats.
const (
	// openPGP formatted messages.
	MessageClearSignature MessageType = 0x100

	// PEM formatted messages.
	MessageRSAOAEP MessageType = 0x200
)

// String returns a human-readable representation of t.
func (t MessageType) String() string {
	switch t {
	case MessageClearSignature:
		return "Clear Signature"
	case MessageRSAOAEP:
		return "RSA-OAEP"
	}
	return "Unknown"
}

// SBOMFormat represents the format used to store an SBOM object.
type SBOMFormat int32

// List of supported SBOM formats.
const (
	SBOMFormatCycloneDXJSON SBOMFormat = iota + 1 // CycloneDX (JSON)
	SBOMFormatCycloneDXXML                        // CycloneDX (XML)
	SBOMFormatGitHubJSON                          // GitHub dependency snapshot (JSON)
	SBOMFormatSPDXJSON                            // SPDX (JSON)
	SBOMFormatSPDXRDF                             // SPDX (RDF/xml)
	SBOMFormatSPDXTagValue                        // SPDX (tag/value)
	SBOMFormatSPDXYAML                            // SPDX (YAML)
	SBOMFormatSyftJSON                            // Syft (JSON)
)

// String returns a human-readable representation of f.
func (f SBOMFormat) String() string {
	switch f {
	case SBOMFormatCycloneDXJSON:
		return "cyclonedx-json"
	case SBOMFormatCycloneDXXML:
		return "cyclonedx-xml"
	case SBOMFormatGitHubJSON:
		return "github-json"
	case SBOMFormatSPDXJSON:
		return "spdx-json"
	case SBOMFormatSPDXRDF:
		return "spdx-rdf"
	case SBOMFormatSPDXTagValue:
		return "spdx-tag-value"
	case SBOMFormatSPDXYAML:
		return "spdx-yaml"
	case SBOMFormatSyftJSON:
		return "syft-json"
	}
	return "unknown"
}

// header describes a loaded SIF file.
type header struct {
	LaunchScript [hdrLaunchLen]byte

	Magic   [hdrMagicLen]byte
	Version [hdrVersionLen]byte
	Arch    archType
	ID      uuid.UUID

	CreatedAt  int64
	ModifiedAt int64

	DescriptorsFree   int64
	DescriptorsTotal  int64
	DescriptorsOffset int64
	DescriptorsSize   int64
	DataOffset        int64
	DataSize          int64
}

// GetIntegrityReader returns an io.Reader that reads the integrity-protected fields from h.
func (h header) GetIntegrityReader() io.Reader {
	return io.MultiReader(
		bytes.NewReader(h.LaunchScript[:]),
		bytes.NewReader(h.Magic[:]),
		bytes.NewReader(h.Version[:]),
		bytes.NewReader(h.ID[:]),
	)
}

// ReadWriter describes the interface required to read and write SIF images.
type ReadWriter interface {
	io.ReaderAt
	io.WriteSeeker
	Truncate(int64) error
}

// FileImage describes the representation of a SIF file in memory.
type FileImage struct {
	rw ReadWriter // Backing storage for image.

	h   header          // Raw global header from image.
	rds []rawDescriptor // Raw descriptors from image.

	closeOnUnload bool              // Close rw on Unload.
	minIDs        map[uint32]uint32 // Minimum object IDs for each group ID.
}

// LaunchScript returns the image launch script.
func (f *FileImage) LaunchScript() string {
	return string(bytes.TrimRight(f.h.LaunchScript[:], "\x00"))
}

// Version returns the SIF specification version of the image.
func (f *FileImage) Version() string {
	return string(bytes.TrimRight(f.h.Version[:], "\x00"))
}

// PrimaryArch returns the primary CPU architecture of the image, or "unknown" if the primary CPU
// architecture cannot be determined.
func (f *FileImage) PrimaryArch() string { return f.h.Arch.GoArch() }

// ID returns the ID of the image.
func (f *FileImage) ID() string { return f.h.ID.String() }

// CreatedAt returns the creation time of the image.
func (f *FileImage) CreatedAt() time.Time { return time.Unix(f.h.CreatedAt, 0) }

// ModifiedAt returns the last modification time of the image.
func (f *FileImage) ModifiedAt() time.Time { return time.Unix(f.h.ModifiedAt, 0) }

// DescriptorsFree returns the number of free descriptors in the image.
func (f *FileImage) DescriptorsFree() int64 { return f.h.DescriptorsFree }

// DescriptorsTotal returns the total number of descriptors in the image.
func (f *FileImage) DescriptorsTotal() int64 { return f.h.DescriptorsTotal }

// DescriptorsOffset returns the offset (in bytes) of the descriptors section in the image.
func (f *FileImage) DescriptorsOffset() int64 { return f.h.DescriptorsOffset }

// DescriptorsSize returns the size (in bytes) of the descriptors section in the image.
func (f *FileImage) DescriptorsSize() int64 { return f.h.DescriptorsSize }

// DataOffset returns the offset (in bytes) of the data section in the image.
func (f *FileImage) DataOffset() int64 { return f.h.DataOffset }

// DataSize returns the size (in bytes) of the data section in the image.
func (f *FileImage) DataSize() int64 { return f.h.DataSize }

// GetHeaderIntegrityReader returns an io.Reader that reads the integrity-protected fields from the
// header of the image.
func (f *FileImage) GetHeaderIntegrityReader() io.Reader {
	return f.h.GetIntegrityReader()
}

// isDeterministic returns true if the UUID and timestamps in the header of f are set to
// deterministic values.
func (f *FileImage) isDeterministic() bool {
	return f.h.ID == uuid.Nil && f.CreatedAt().IsZero() && f.ModifiedAt().IsZero()
}
