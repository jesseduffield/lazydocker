package minimal

// NOTE: This is used from github.com/containers/image by callers that
// don't otherwise use containers/storage, so don't make this depend on any
// larger software like the graph drivers.

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/klauspost/compress/zstd"
	"github.com/opencontainers/go-digest"
	"github.com/vbatts/tar-split/archive/tar"
	"go.podman.io/storage/pkg/archive"
)

// ZstdWriter is an interface that wraps standard io.WriteCloser and Reset() to reuse the compressor with a new writer.
type ZstdWriter interface {
	io.WriteCloser
	Reset(dest io.Writer)
}

// CreateZstdWriterFunc is a function that creates a ZstdWriter for the provided destination writer.
type CreateZstdWriterFunc func(dest io.Writer) (ZstdWriter, error)

// TOC is short for Table of Contents and is used by the zstd:chunked
// file format to effectively add an overall index into the contents
// of a tarball; it also includes file metadata.
type TOC struct {
	// Version is currently expected to be 1
	Version int `json:"version"`
	// Entries is the list of file metadata in this TOC.
	// The ordering in this array currently defaults to being the same
	// as that of the tar stream; however, this should not be relied on.
	Entries []FileMetadata `json:"entries"`
	// TarSplitDigest is the checksum of the "tar-split" data which
	// is included as a distinct skippable zstd frame before the TOC.
	TarSplitDigest digest.Digest `json:"tarSplitDigest,omitempty"`
}

// FileMetadata is an entry in the TOC that includes both generic file metadata
// that duplicates what can found in the tar header (and should match), but
// also special/custom content (see below).
//
// Regular files may optionally be represented as a sequence of “chunks”,
// which may be ChunkTypeData or ChunkTypeZeros (and ChunkTypeData boundaries
// are heuristically determined to increase chance of chunk matching / reuse
// similar to rsync). In that case, the regular file is represented
// as an initial TypeReg entry (with all metadata for the file as a whole)
// immediately followed by zero or more TypeChunk entries (containing only Type,
// Name and Chunk* fields); if there is at least one TypeChunk entry, the Chunk*
// fields are relevant in all of these entries, including the initial
// TypeReg one.
//
// Note that the metadata here, when fetched by a zstd:chunked aware client,
// is used instead of that in the tar stream.  The contents of the tar stream
// are not used in this scenario.
type FileMetadata struct {
	// If you add any fields, update ensureFileMetadataMatches as well!

	// The metadata below largely duplicates that in the tar headers.
	Type       string            `json:"type"`
	Name       string            `json:"name"`
	Linkname   string            `json:"linkName,omitempty"`
	Mode       int64             `json:"mode,omitempty"`
	Size       int64             `json:"size,omitempty"`
	UID        int               `json:"uid,omitempty"`
	GID        int               `json:"gid,omitempty"`
	ModTime    *time.Time        `json:"modtime,omitempty"`
	AccessTime *time.Time        `json:"accesstime,omitempty"`
	ChangeTime *time.Time        `json:"changetime,omitempty"`
	Devmajor   int64             `json:"devMajor,omitempty"`
	Devminor   int64             `json:"devMinor,omitempty"`
	Xattrs     map[string]string `json:"xattrs,omitempty"`
	// Digest is a hexadecimal sha256 checksum of the file contents; it
	// is empty for empty files
	Digest    string `json:"digest,omitempty"`
	Offset    int64  `json:"offset,omitempty"`
	EndOffset int64  `json:"endOffset,omitempty"`

	ChunkSize   int64  `json:"chunkSize,omitempty"`
	ChunkOffset int64  `json:"chunkOffset,omitempty"`
	ChunkDigest string `json:"chunkDigest,omitempty"`
	ChunkType   string `json:"chunkType,omitempty"`
}

const (
	ChunkTypeData  = ""
	ChunkTypeZeros = "zeros"
)

const (
	// The following types correspond to regular types of entries that can
	// appear in a tar archive.
	TypeReg     = "reg"
	TypeLink    = "hardlink"
	TypeChar    = "char"
	TypeBlock   = "block"
	TypeDir     = "dir"
	TypeFifo    = "fifo"
	TypeSymlink = "symlink"
	// TypeChunk is special; in zstd:chunked not only are files individually
	// compressed and indexable, there is a "rolling checksum" used to compute
	// "chunks" of individual file contents, that are also added to the TOC
	TypeChunk = "chunk"
)

var TarTypes = map[byte]string{
	tar.TypeReg:     TypeReg,
	tar.TypeLink:    TypeLink,
	tar.TypeChar:    TypeChar,
	tar.TypeBlock:   TypeBlock,
	tar.TypeDir:     TypeDir,
	tar.TypeFifo:    TypeFifo,
	tar.TypeSymlink: TypeSymlink,
}

func GetType(t byte) (string, error) {
	r, found := TarTypes[t]
	if !found {
		return "", fmt.Errorf("unknown tarball type: %v", t)
	}
	return r, nil
}

const (
	// ManifestChecksumKey is a hexadecimal sha256 digest of the compressed manifest digest.
	ManifestChecksumKey = "io.github.containers.zstd-chunked.manifest-checksum"
	// ManifestInfoKey is an annotation that signals the start of the TOC (manifest)
	// contents which are embedded as a skippable zstd frame.  It has a format of
	// four decimal integers separated by `:` as follows:
	// <offset>:<length>:<uncompressed length>:<type>
	// The <type> is ManifestTypeCRFS which should have the value `1`.
	ManifestInfoKey = "io.github.containers.zstd-chunked.manifest-position"
	// TarSplitInfoKey is an annotation that signals the start of the "tar-split" metadata
	// contents which are embedded as a skippable zstd frame.  It has a format of
	// three decimal integers separated by `:` as follows:
	// <offset>:<length>:<uncompressed length>
	TarSplitInfoKey = "io.github.containers.zstd-chunked.tarsplit-position"

	// TarSplitChecksumKey is no longer used and is replaced by the TOC.TarSplitDigest field instead.
	// The value is retained here as a constant as a historical reference for older zstd:chunked images.
	//
	// Deprecated: This field should never be relied on - use the digest in the TOC instead.
	TarSplitChecksumKey = "io.github.containers.zstd-chunked.tarsplit-checksum"

	// ManifestTypeCRFS is a manifest file compatible with the CRFS TOC file.
	ManifestTypeCRFS = 1

	// FooterSizeSupported is the footer size supported by this implementation.
	// Newer versions of the image format might increase this value, so reject
	// any version that is not supported.
	FooterSizeSupported = 64
)

var (
	// when the zstd decoder encounters a skippable frame + 1 byte for the size, it
	// will ignore it.
	// https://tools.ietf.org/html/rfc8478#section-3.1.2
	skippableFrameMagic = []byte{0x50, 0x2a, 0x4d, 0x18}

	ZstdChunkedFrameMagic = []byte{0x47, 0x4e, 0x55, 0x6c, 0x49, 0x6e, 0x55, 0x78}
)

func appendZstdSkippableFrame(dest io.Writer, data []byte) error {
	if _, err := dest.Write(skippableFrameMagic); err != nil {
		return err
	}

	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(data)))
	if _, err := dest.Write(size); err != nil {
		return err
	}
	if _, err := dest.Write(data); err != nil {
		return err
	}
	return nil
}

type TarSplitData struct {
	Data             []byte
	Digest           digest.Digest
	UncompressedSize int64
}

func WriteZstdChunkedManifest(dest io.Writer, outMetadata map[string]string, offset uint64, tarSplitData *TarSplitData, metadata []FileMetadata, createZstdWriter CreateZstdWriterFunc) error {
	// 8 is the size of the zstd skippable frame header + the frame size
	const zstdSkippableFrameHeader = 8
	manifestOffset := offset + zstdSkippableFrameHeader

	toc := TOC{
		Version:        1,
		Entries:        metadata,
		TarSplitDigest: tarSplitData.Digest,
	}

	json := jsoniter.ConfigCompatibleWithStandardLibrary
	// Generate the manifest
	manifest, err := json.Marshal(toc)
	if err != nil {
		return err
	}

	var compressedBuffer bytes.Buffer
	zstdWriter, err := createZstdWriter(&compressedBuffer)
	if err != nil {
		return err
	}
	if _, err := zstdWriter.Write(manifest); err != nil {
		zstdWriter.Close()
		return err
	}
	if err := zstdWriter.Close(); err != nil {
		return err
	}
	compressedManifest := compressedBuffer.Bytes()

	manifestDigester := digest.Canonical.Digester()
	manifestChecksum := manifestDigester.Hash()
	if _, err := manifestChecksum.Write(compressedManifest); err != nil {
		return err
	}

	outMetadata[ManifestChecksumKey] = manifestDigester.Digest().String()
	outMetadata[ManifestInfoKey] = fmt.Sprintf("%d:%d:%d:%d", manifestOffset, len(compressedManifest), len(manifest), ManifestTypeCRFS)
	if err := appendZstdSkippableFrame(dest, compressedManifest); err != nil {
		return err
	}

	tarSplitOffset := manifestOffset + uint64(len(compressedManifest)) + zstdSkippableFrameHeader
	outMetadata[TarSplitInfoKey] = fmt.Sprintf("%d:%d:%d", tarSplitOffset, len(tarSplitData.Data), tarSplitData.UncompressedSize)
	if err := appendZstdSkippableFrame(dest, tarSplitData.Data); err != nil {
		return err
	}

	footer := ZstdChunkedFooterData{
		ManifestType:               uint64(ManifestTypeCRFS),
		Offset:                     manifestOffset,
		LengthCompressed:           uint64(len(compressedManifest)),
		LengthUncompressed:         uint64(len(manifest)),
		OffsetTarSplit:             tarSplitOffset,
		LengthCompressedTarSplit:   uint64(len(tarSplitData.Data)),
		LengthUncompressedTarSplit: uint64(tarSplitData.UncompressedSize),
	}

	manifestDataLE := footerDataToBlob(footer)

	return appendZstdSkippableFrame(dest, manifestDataLE)
}

func ZstdWriterWithLevel(dest io.Writer, level int) (ZstdWriter, error) {
	el := zstd.EncoderLevelFromZstd(level)
	return zstd.NewWriter(dest, zstd.WithEncoderLevel(el))
}

// ZstdChunkedFooterData contains all the data stored in the zstd:chunked footer.
// This footer exists to make the blobs self-describing, our implementation
// never reads it:
// Partial pull security hinges on the TOC digest, and that exists as a layer annotation;
// so we are relying on the layer annotations anyway, and doing so means we can avoid
// a round-trip to fetch this binary footer.
type ZstdChunkedFooterData struct {
	ManifestType uint64

	Offset             uint64
	LengthCompressed   uint64
	LengthUncompressed uint64

	OffsetTarSplit             uint64
	LengthCompressedTarSplit   uint64
	LengthUncompressedTarSplit uint64
	ChecksumAnnotationTarSplit string // Deprecated: This field is not a part of the footer and not used for any purpose.
}

func footerDataToBlob(footer ZstdChunkedFooterData) []byte {
	// Store the offset to the manifest and its size in LE order
	manifestDataLE := make([]byte, FooterSizeSupported)
	binary.LittleEndian.PutUint64(manifestDataLE[8*0:], footer.Offset)
	binary.LittleEndian.PutUint64(manifestDataLE[8*1:], footer.LengthCompressed)
	binary.LittleEndian.PutUint64(manifestDataLE[8*2:], footer.LengthUncompressed)
	binary.LittleEndian.PutUint64(manifestDataLE[8*3:], footer.ManifestType)
	binary.LittleEndian.PutUint64(manifestDataLE[8*4:], footer.OffsetTarSplit)
	binary.LittleEndian.PutUint64(manifestDataLE[8*5:], footer.LengthCompressedTarSplit)
	binary.LittleEndian.PutUint64(manifestDataLE[8*6:], footer.LengthUncompressedTarSplit)
	copy(manifestDataLE[8*7:], ZstdChunkedFrameMagic)

	return manifestDataLE
}

// timeIfNotZero returns a pointer to the time.Time if it is not zero, otherwise it returns nil.
func timeIfNotZero(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	return t
}

// NewFileMetadata creates a basic FileMetadata entry for hdr.
// The caller must set DigestOffset/EndOffset, and the Chunk* values, separately.
func NewFileMetadata(hdr *tar.Header) (FileMetadata, error) {
	typ, err := GetType(hdr.Typeflag)
	if err != nil {
		return FileMetadata{}, err
	}
	xattrs := make(map[string]string)
	for k, v := range hdr.PAXRecords {
		xattrKey, ok := strings.CutPrefix(k, archive.PaxSchilyXattr)
		if !ok {
			continue
		}
		xattrs[xattrKey] = base64.StdEncoding.EncodeToString([]byte(v))
	}
	return FileMetadata{
		Type:       typ,
		Name:       hdr.Name,
		Linkname:   hdr.Linkname,
		Mode:       hdr.Mode,
		Size:       hdr.Size,
		UID:        hdr.Uid,
		GID:        hdr.Gid,
		ModTime:    timeIfNotZero(&hdr.ModTime),
		AccessTime: timeIfNotZero(&hdr.AccessTime),
		ChangeTime: timeIfNotZero(&hdr.ChangeTime),
		Devmajor:   hdr.Devmajor,
		Devminor:   hdr.Devminor,
		Xattrs:     xattrs,
	}, nil
}
