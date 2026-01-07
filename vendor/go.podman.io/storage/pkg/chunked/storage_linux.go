package chunked

import (
	archivetar "archive/tar"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/containerd/stargz-snapshotter/estargz"
	securejoin "github.com/cyphar/filepath-securejoin"
	jsoniter "github.com/json-iterator/go"
	"github.com/klauspost/compress/zstd"
	"github.com/klauspost/pgzip"
	digest "github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"github.com/vbatts/tar-split/archive/tar"
	"github.com/vbatts/tar-split/tar/asm"
	tsStorage "github.com/vbatts/tar-split/tar/storage"
	storage "go.podman.io/storage"
	graphdriver "go.podman.io/storage/drivers"
	"go.podman.io/storage/pkg/archive"
	"go.podman.io/storage/pkg/chunked/compressor"
	"go.podman.io/storage/pkg/chunked/internal/minimal"
	path "go.podman.io/storage/pkg/chunked/internal/path"
	"go.podman.io/storage/pkg/chunked/toc"
	"go.podman.io/storage/pkg/fsverity"
	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/system"
	"golang.org/x/sys/unix"
)

const (
	maxNumberMissingChunks  = 1024
	autoMergePartsThreshold = 1024 // if the gap between two ranges is below this threshold, automatically merge them.
	newFileFlags            = (unix.O_CREAT | unix.O_TRUNC | unix.O_EXCL | unix.O_WRONLY)
	bigDataKey              = "zstd-chunked-manifest"
	chunkedData             = "zstd-chunked-data"
	chunkedLayerDataKey     = "zstd-chunked-layer-data"
	tocKey                  = "toc"
	fsVerityDigestsKey      = "fs-verity-digests"

	fileTypeZstdChunked = iota
	fileTypeEstargz
	fileTypeNoCompression
	fileTypeHole

	copyGoRoutines = 32
)

type compressedFileType int

type chunkedDiffer struct {
	// Initial parameters, used throughout and never modified
	// ==========
	pullOptions pullOptions
	stream      ImageSourceSeekable
	// blobDigest is the digest of the whole compressed layer.  It is used if
	// convertToZstdChunked to validate a layer when it is converted since there
	// is no TOC referenced by the manifest.
	blobDigest digest.Digest
	blobSize   int64

	// Input format
	// ==========
	fileType compressedFileType
	// convertedToZstdChunked is set to true if the layer needs to
	// be converted to the zstd:chunked format before it can be
	// handled.
	convertToZstdChunked bool

	// Chunked metadata
	// This is usually set in NewDiffer, but if convertToZstdChunked, it is only computed in chunkedDiffer.ApplyDiff
	// ==========
	// tocDigest is the digest of the TOC document when the layer
	// is partially pulled, or "" if not relevant to consumers.
	tocDigest           digest.Digest
	tocOffset           int64
	manifest            []byte
	toc                 *minimal.TOC // The parsed contents of manifest, or nil if not yet available
	tarSplit            *os.File
	uncompressedTarSize int64 // -1 if unknown
	// skipValidation is set to true if the individual files in
	// the layer are trusted and should not be validated.
	skipValidation bool

	// Long-term caches
	// This is set in NewDiffer, when the caller must not hold any storage locks, and later consumed in .ApplyDiff()
	// ==========
	layersCache     *layersCache
	copyBuffer      []byte
	fsVerityMutex   sync.Mutex // protects fsVerityDigests
	fsVerityDigests map[string]string

	// Private state of .ApplyDiff
	// ==========
	gzipReader  *pgzip.Reader
	zstdReader  *zstd.Decoder
	rawReader   io.Reader
	useFsVerity graphdriver.DifferFsVerity
	used        bool // the differ object was already used and cannot be used again for .ApplyDiff
}

var xattrsToIgnore = map[string]any{
	"security.selinux": true,
}

// chunkedLayerData is used to store additional information about the layer
type chunkedLayerData struct {
	Format graphdriver.DifferOutputFormat `json:"format"`
}

// pullOptions contains parsed data from storage.Store.PullOptions.
// TO DO: ideally this should be parsed along with the rest of the config file into StoreOptions directly
// (and then storage.Store.PullOptions would need to be somehow simulated).
type pullOptions struct {
	enablePartialImages                     bool     // enable_partial_images
	convertImages                           bool     // convert_images
	useHardLinks                            bool     // use_hard_links
	insecureAllowUnpredictableImageContents bool     // insecure_allow_unpredictable_image_contents
	ostreeRepos                             []string // ostree_repos
}

func parsePullOptions(store storage.Store) pullOptions {
	options := store.PullOptions()

	res := pullOptions{}
	for _, e := range []struct {
		dest         *bool
		name         string
		defaultValue bool
	}{
		{&res.enablePartialImages, "enable_partial_images", false},
		{&res.convertImages, "convert_images", false},
		{&res.useHardLinks, "use_hard_links", false},
		{&res.insecureAllowUnpredictableImageContents, "insecure_allow_unpredictable_image_contents", false},
	} {
		if value, ok := options[e.name]; ok {
			*e.dest = strings.ToLower(value) == "true"
		} else {
			*e.dest = e.defaultValue
		}
	}
	res.ostreeRepos = strings.Split(options["ostree_repos"], ":")

	return res
}

func (c *chunkedDiffer) convertTarToZstdChunked(destDirectory string, payload *os.File) (int64, *seekableFile, digest.Digest, map[string]string, error) {
	diff, err := archive.DecompressStream(payload)
	if err != nil {
		return 0, nil, "", nil, err
	}

	defer diff.Close()

	f, err := openTmpFile(destDirectory)
	if err != nil {
		return 0, nil, "", nil, err
	}

	newAnnotations := make(map[string]string)
	chunked, err := compressor.NoCompression(f, newAnnotations)
	if err != nil {
		f.Close()
		return 0, nil, "", nil, err
	}

	convertedOutputDigester := digest.Canonical.Digester()
	copied, err := io.CopyBuffer(io.MultiWriter(chunked, convertedOutputDigester.Hash()), diff, c.copyBuffer)
	if err != nil {
		f.Close()
		return 0, nil, "", nil, err
	}
	if err := chunked.Close(); err != nil {
		f.Close()
		return 0, nil, "", nil, err
	}

	return copied, newSeekableFile(f), convertedOutputDigester.Digest(), newAnnotations, nil
}

func (c *chunkedDiffer) Close() error {
	if c.tarSplit != nil {
		err := c.tarSplit.Close()
		c.tarSplit = nil
		return err
	}
	return nil
}

// NewDiffer returns a differ than can be used with [Store.PrepareStagedLayer].
// If it returns an error that matches ErrFallbackToOrdinaryLayerDownload, the caller can
// retry the operation with a different method.
// The caller must call Close() on the returned Differ.
func NewDiffer(ctx context.Context, store storage.Store, blobDigest digest.Digest, blobSize int64, annotations map[string]string, iss ImageSourceSeekable) (graphdriver.Differ, error) {
	pullOptions := parsePullOptions(store)

	if !pullOptions.enablePartialImages {
		// If pullOptions.convertImages is set, the two options disagree whether fallback is permissible.
		// Right now, we enable it, but that’s not a promise; rather, such a configuration should ideally be rejected.
		return nil, newErrFallbackToOrdinaryLayerDownload(errors.New("partial images are disabled"))
	}
	// pullOptions.convertImages also serves as a “must not fallback to non-partial pull” option (?!)

	graphDriver, err := store.GraphDriver()
	if err != nil {
		return nil, err
	}
	if _, partialSupported := graphDriver.(graphdriver.DriverWithDiffer); !partialSupported {
		if pullOptions.convertImages {
			return nil, fmt.Errorf("graph driver %s does not support partial pull but convert_images requires that", graphDriver.String())
		}
		return nil, newErrFallbackToOrdinaryLayerDownload(fmt.Errorf("graph driver %s does not support partial pull", graphDriver.String()))
	}

	differ, err := getProperDiffer(store, blobDigest, blobSize, annotations, iss, pullOptions)
	if err != nil {
		var fallbackErr ErrFallbackToOrdinaryLayerDownload
		if !errors.As(err, &fallbackErr) {
			return nil, err
		}
		// If convert_images is enabled, always attempt to convert it instead of returning an error or falling back to a different method.
		if !pullOptions.convertImages {
			return nil, err
		}
		var canConvertErr errFallbackCanConvert
		if !errors.As(err, &canConvertErr) {
			// We are supposed to use makeConvertFromRawDiffer, but that would not work.
			// Fail, and make sure the error does _not_ match ErrFallbackToOrdinaryLayerDownload: use only the error text,
			// discard all type information.
			return nil, fmt.Errorf("neither a partial pull nor convert_images is possible: %s", err.Error())
		}
		logrus.Debugf("Created differ to convert blob %q", blobDigest)
		return makeConvertFromRawDiffer(store, blobDigest, blobSize, iss, pullOptions)
	}

	return differ, nil
}

// errFallbackCanConvert is an an error type _accompanying_ ErrFallbackToOrdinaryLayerDownload
// within getProperDiffer, to mark that using makeConvertFromRawDiffer makes sense.
// This is used to distinguish between cases where the environment does not support partial pulls
// (e.g. a registry does not support range requests) and convert_images is still possible,
// from cases where the image content is unacceptable for partial pulls (e.g. exceeds memory limits)
// and convert_images would not help.
type errFallbackCanConvert struct {
	err error
}

func (e errFallbackCanConvert) Error() string {
	return e.err.Error()
}

func (e errFallbackCanConvert) Unwrap() error {
	return e.err
}

// getProperDiffer is an implementation detail of NewDiffer.
// It returns a “proper” differ (not a convert_images one) if possible.
// May return an error matching ErrFallbackToOrdinaryLayerDownload if a fallback to an alternative
// (either makeConvertFromRawDiffer, or a non-partial pull) is permissible.
func getProperDiffer(store storage.Store, blobDigest digest.Digest, blobSize int64, annotations map[string]string, iss ImageSourceSeekable, pullOptions pullOptions) (graphdriver.Differ, error) {
	zstdChunkedTOCDigestString, hasZstdChunkedTOC := annotations[minimal.ManifestChecksumKey]
	estargzTOCDigestString, hasEstargzTOC := annotations[estargz.TOCJSONDigestAnnotation]

	switch {
	case hasZstdChunkedTOC && hasEstargzTOC:
		return nil, errors.New("both zstd:chunked and eStargz TOC found")

	case hasZstdChunkedTOC:
		zstdChunkedTOCDigest, err := digest.Parse(zstdChunkedTOCDigestString)
		if err != nil {
			return nil, err
		}
		differ, err := makeZstdChunkedDiffer(store, blobSize, zstdChunkedTOCDigest, annotations, iss, pullOptions)
		if err != nil {
			logrus.Debugf("Could not create zstd:chunked differ for blob %q: %v", blobDigest, err)
			return nil, err
		}
		logrus.Debugf("Created zstd:chunked differ for blob %q", blobDigest)
		return differ, nil

	case hasEstargzTOC:
		estargzTOCDigest, err := digest.Parse(estargzTOCDigestString)
		if err != nil {
			return nil, err
		}
		differ, err := makeEstargzChunkedDiffer(store, blobSize, estargzTOCDigest, iss, pullOptions)
		if err != nil {
			logrus.Debugf("Could not create estargz differ for blob %q: %v", blobDigest, err)
			return nil, err
		}
		logrus.Debugf("Created eStargz differ for blob %q", blobDigest)
		return differ, nil

	default: // no TOC
		message := "no TOC found"
		if !pullOptions.convertImages {
			message = "no TOC found and convert_images is not configured"
		}
		return nil, errFallbackCanConvert{
			newErrFallbackToOrdinaryLayerDownload(errors.New(message)),
		}
	}
}

func makeConvertFromRawDiffer(store storage.Store, blobDigest digest.Digest, blobSize int64, iss ImageSourceSeekable, pullOptions pullOptions) (*chunkedDiffer, error) {
	layersCache, err := getLayersCache(store)
	if err != nil {
		return nil, err
	}

	return &chunkedDiffer{
		pullOptions: pullOptions,
		stream:      iss,
		blobDigest:  blobDigest,
		blobSize:    blobSize,

		convertToZstdChunked: true,

		uncompressedTarSize: -1, // Will be computed later

		layersCache:     layersCache,
		copyBuffer:      makeCopyBuffer(),
		fsVerityDigests: make(map[string]string),
	}, nil
}

// makeZstdChunkedDiffer sets up a chunkedDiffer for a zstd:chunked layer.
// It may return an error matching ErrFallbackToOrdinaryLayerDownload / errFallbackCanConvert.
func makeZstdChunkedDiffer(store storage.Store, blobSize int64, tocDigest digest.Digest, annotations map[string]string, iss ImageSourceSeekable, pullOptions pullOptions) (_ *chunkedDiffer, retErr error) {
	manifest, toc, tarSplit, tocOffset, err := readZstdChunkedManifest(store.RunRoot(), iss, tocDigest, annotations, true)
	if err != nil { // May be ErrFallbackToOrdinaryLayerDownload / errFallbackCanConvert
		return nil, fmt.Errorf("read zstd:chunked manifest: %w", err)
	}
	defer func() {
		if tarSplit != nil && retErr != nil {
			tarSplit.Close()
		}
	}()

	var uncompressedTarSize int64 = -1
	if tarSplit != nil {
		if _, err := tarSplit.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		uncompressedTarSize, err = tarSizeFromTarSplit(tarSplit)
		if err != nil {
			return nil, fmt.Errorf("computing size from tar-split: %w", err)
		}
	} else if !pullOptions.insecureAllowUnpredictableImageContents { // With no tar-split, we can't compute the traditional UncompressedDigest.
		return nil, errFallbackCanConvert{
			newErrFallbackToOrdinaryLayerDownload(fmt.Errorf("zstd:chunked layers without tar-split data don't support partial pulls with guaranteed consistency with non-partial pulls")),
		}
	}

	layersCache, err := getLayersCache(store)
	if err != nil {
		return nil, err
	}

	return &chunkedDiffer{
		pullOptions: pullOptions,
		stream:      iss,
		blobSize:    blobSize,

		fileType: fileTypeZstdChunked,

		tocDigest:           tocDigest,
		tocOffset:           tocOffset,
		manifest:            manifest,
		toc:                 toc,
		tarSplit:            tarSplit,
		uncompressedTarSize: uncompressedTarSize,

		layersCache:     layersCache,
		copyBuffer:      makeCopyBuffer(),
		fsVerityDigests: make(map[string]string),
	}, nil
}

// makeEstargzChunkedDiffer sets up a chunkedDiffer for an estargz layer.
// It may return an error matching ErrFallbackToOrdinaryLayerDownload / errFallbackCanConvert.
func makeEstargzChunkedDiffer(store storage.Store, blobSize int64, tocDigest digest.Digest, iss ImageSourceSeekable, pullOptions pullOptions) (*chunkedDiffer, error) {
	if !pullOptions.insecureAllowUnpredictableImageContents { // With no tar-split, we can't compute the traditional UncompressedDigest.
		return nil, errFallbackCanConvert{
			newErrFallbackToOrdinaryLayerDownload(fmt.Errorf("estargz layers don't support partial pulls with guaranteed consistency with non-partial pulls")),
		}
	}

	manifest, tocOffset, err := readEstargzChunkedManifest(iss, blobSize, tocDigest)
	if err != nil { // May be ErrFallbackToOrdinaryLayerDownload / errFallbackCanConvert
		return nil, fmt.Errorf("read zstd:chunked manifest: %w", err)
	}
	layersCache, err := getLayersCache(store)
	if err != nil {
		return nil, err
	}

	return &chunkedDiffer{
		pullOptions: pullOptions,
		stream:      iss,
		blobSize:    blobSize,

		fileType: fileTypeEstargz,

		tocDigest:           tocDigest,
		tocOffset:           tocOffset,
		manifest:            manifest,
		uncompressedTarSize: -1, // We would have to read and decompress the whole layer

		layersCache:     layersCache,
		copyBuffer:      makeCopyBuffer(),
		fsVerityDigests: make(map[string]string),
	}, nil
}

func makeCopyBuffer() []byte {
	return make([]byte, 2<<20)
}

// copyFileFromOtherLayer copies a file from another layer
// file is the file to look for.
// source is the path to the source layer checkout.
// name is the path to the file to copy in source.
// dirfd is an open file descriptor to the destination root directory.
// useHardLinks defines whether the deduplication can be performed using hard links.
func copyFileFromOtherLayer(file *fileMetadata, source string, name string, dirfd int, useHardLinks bool) (bool, *os.File, int64, error) {
	srcDirfd, err := unix.Open(source, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return false, nil, 0, &fs.PathError{Op: "open", Path: source, Err: err}
	}
	defer unix.Close(srcDirfd)

	srcFile, err := openFileUnderRoot(srcDirfd, name, unix.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return false, nil, 0, err
	}
	defer srcFile.Close()

	dstFile, written, err := copyFileContent(int(srcFile.Fd()), file, dirfd, 0, useHardLinks)
	if err != nil {
		return false, nil, 0, fmt.Errorf("copy content to %q: %w", file.Name, err)
	}
	return true, dstFile, written, nil
}

// canDedupMetadataWithHardLink says whether it is possible to deduplicate file with otherFile.
// It checks that the two files have the same UID, GID, file mode and xattrs.
func canDedupMetadataWithHardLink(file *fileMetadata, otherFile *fileMetadata) bool {
	if file.UID != otherFile.UID {
		return false
	}
	if file.GID != otherFile.GID {
		return false
	}
	if file.Mode != otherFile.Mode {
		return false
	}
	if !reflect.DeepEqual(file.Xattrs, otherFile.Xattrs) {
		return false
	}
	return true
}

// canDedupFileWithHardLink checks if the specified file can be deduplicated by an
// open file, given its descriptor and stat data.
func canDedupFileWithHardLink(file *fileMetadata, fd int, s os.FileInfo) bool {
	st, ok := s.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}

	path := procPathForFd(fd)

	listXattrs, err := system.Llistxattr(path)
	if err != nil {
		return false
	}

	xattrs := make(map[string]string)
	for _, x := range listXattrs {
		v, err := system.Lgetxattr(path, x)
		if err != nil {
			return false
		}

		if _, found := xattrsToIgnore[x]; found {
			continue
		}
		xattrs[x] = string(v)
	}
	// fill only the attributes used by canDedupMetadataWithHardLink.
	otherFile := fileMetadata{
		FileMetadata: minimal.FileMetadata{
			UID:    int(st.Uid),
			GID:    int(st.Gid),
			Mode:   int64(st.Mode),
			Xattrs: xattrs,
		},
	}
	return canDedupMetadataWithHardLink(file, &otherFile)
}

// findFileInOSTreeRepos checks whether the requested file already exist in one of the OSTree repo and copies the file content from there if possible.
// file is the file to look for.
// ostreeRepos is a list of OSTree repos.
// dirfd is an open fd to the destination checkout.
// useHardLinks defines whether the deduplication can be performed using hard links.
func findFileInOSTreeRepos(file *fileMetadata, ostreeRepos []string, dirfd int, useHardLinks bool) (bool, *os.File, int64, error) {
	digest, err := digest.Parse(file.Digest)
	if err != nil {
		logrus.Debugf("could not parse digest: %v", err)
		return false, nil, 0, nil
	}
	payloadLink := digest.Encoded() + ".payload-link"
	if len(payloadLink) < 2 {
		return false, nil, 0, nil
	}

	for _, repo := range ostreeRepos {
		sourceFile := filepath.Join(repo, "objects", payloadLink[:2], payloadLink[2:])
		st, err := os.Stat(sourceFile)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		if st.Size() != file.Size {
			continue
		}
		fd, err := unix.Open(sourceFile, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			logrus.Debugf("could not open sourceFile %s: %v", sourceFile, err)
			return false, nil, 0, nil
		}
		f := os.NewFile(uintptr(fd), "fd")
		defer f.Close()

		// check if the open file can be deduplicated with hard links
		if useHardLinks && !canDedupFileWithHardLink(file, fd, st) {
			continue
		}

		dstFile, written, err := copyFileContent(fd, file, dirfd, 0, useHardLinks)
		if err != nil {
			logrus.Debugf("could not copyFileContent: %v", err)
			return false, nil, 0, nil
		}
		return true, dstFile, written, nil
	}
	// If hard links deduplication was used and it has failed, try again without hard links.
	if useHardLinks {
		return findFileInOSTreeRepos(file, ostreeRepos, dirfd, false)
	}

	return false, nil, 0, nil
}

// findFileInOtherLayers finds the specified file in other layers.
// cache is the layers cache to use.
// file is the file to look for.
// dirfd is an open file descriptor to the checkout root directory.
// useHardLinks defines whether the deduplication can be performed using hard links.
func findFileInOtherLayers(cache *layersCache, file *fileMetadata, dirfd int, useHardLinks bool) (bool, *os.File, int64, error) {
	target, name, err := cache.findFileInOtherLayers(file, useHardLinks)
	if err != nil || name == "" {
		return false, nil, 0, err
	}
	return copyFileFromOtherLayer(file, target, name, dirfd, useHardLinks)
}

func maybeDoIDRemap(manifest []fileMetadata, options *archive.TarOptions) error {
	if options.ChownOpts == nil && len(options.UIDMaps) == 0 || len(options.GIDMaps) == 0 {
		return nil
	}

	idMappings := idtools.NewIDMappingsFromMaps(options.UIDMaps, options.GIDMaps)

	for i := range manifest {
		if options.ChownOpts != nil {
			manifest[i].UID = options.ChownOpts.UID
			manifest[i].GID = options.ChownOpts.GID
		} else {
			pair := idtools.IDPair{
				UID: manifest[i].UID,
				GID: manifest[i].GID,
			}
			var err error
			manifest[i].UID, manifest[i].GID, err = idMappings.ToContainer(pair)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func mapToSlice(inputMap map[uint32]struct{}) []uint32 {
	var out []uint32
	for value := range inputMap {
		out = append(out, value)
	}
	return out
}

func collectIDs(entries []fileMetadata) ([]uint32, []uint32) {
	uids := make(map[uint32]struct{})
	gids := make(map[uint32]struct{})
	for _, entry := range entries {
		uids[uint32(entry.UID)] = struct{}{}
		gids[uint32(entry.GID)] = struct{}{}
	}
	return mapToSlice(uids), mapToSlice(gids)
}

type originFile struct {
	Root   string
	Path   string
	Offset int64
}

type missingFileChunk struct {
	Gap  int64
	Hole bool

	File *fileMetadata

	CompressedSize   int64
	UncompressedSize int64
}

type missingPart struct {
	Hole        bool
	SourceChunk *ImageSourceChunk
	OriginFile  *originFile
	Chunks      []missingFileChunk
}

func (o *originFile) OpenFile() (io.ReadCloser, error) {
	srcDirfd, err := unix.Open(o.Root, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: o.Root, Err: err}
	}
	defer unix.Close(srcDirfd)

	srcFile, err := openFileUnderRoot(srcDirfd, o.Path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}

	if _, err := srcFile.Seek(o.Offset, io.SeekStart); err != nil {
		srcFile.Close()
		return nil, err
	}
	return srcFile, nil
}

func (c *chunkedDiffer) prepareCompressedStreamToFile(partCompression compressedFileType, mf *missingFileChunk) (compressedFileType, error) {
	switch {
	case partCompression == fileTypeHole:
		// The entire part is a hole.  Do not need to read from a file.
		return fileTypeHole, nil
	case mf.Hole:
		// Only the missing chunk in the requested part refers to a hole.
		// The received data must be discarded.
		_, err := io.CopyBuffer(io.Discard, c.rawReader, c.copyBuffer)
		return fileTypeHole, err
	case partCompression == fileTypeZstdChunked:
		if c.zstdReader == nil {
			r, err := zstd.NewReader(c.rawReader)
			if err != nil {
				return partCompression, err
			}
			c.zstdReader = r
		} else {
			if err := c.zstdReader.Reset(c.rawReader); err != nil {
				return partCompression, err
			}
		}
	case partCompression == fileTypeEstargz:
		if c.gzipReader == nil {
			r, err := pgzip.NewReader(c.rawReader)
			if err != nil {
				return partCompression, err
			}
			c.gzipReader = r
		} else {
			if err := c.gzipReader.Reset(c.rawReader); err != nil {
				return partCompression, err
			}
		}
	case partCompression == fileTypeNoCompression:
		return fileTypeNoCompression, nil
	default:
		return partCompression, fmt.Errorf("unknown file type %q", c.fileType)
	}
	return partCompression, nil
}

// hashHole writes SIZE zeros to the specified hasher
func hashHole(h hash.Hash, size int64, copyBuffer []byte) error {
	count := min(size, int64(len(copyBuffer)))
	for i := int64(0); i < count; i++ {
		copyBuffer[i] = 0
	}
	for size > 0 {
		count = min(size, int64(len(copyBuffer)))
		if _, err := h.Write(copyBuffer[:count]); err != nil {
			return err
		}
		size -= count
	}
	return nil
}

func (c *chunkedDiffer) appendCompressedStreamToFile(compression compressedFileType, destFile *destinationFile, size int64) error {
	switch compression {
	case fileTypeZstdChunked:
		defer func() {
			if err := c.zstdReader.Reset(nil); err != nil {
				logrus.Warnf("release of references to the previous zstd reader failed: %v", err)
			}
		}()
		if _, err := io.CopyBuffer(destFile.to, io.LimitReader(c.zstdReader, size), c.copyBuffer); err != nil {
			return err
		}
	case fileTypeEstargz:
		defer c.gzipReader.Close()
		if _, err := io.CopyBuffer(destFile.to, io.LimitReader(c.gzipReader, size), c.copyBuffer); err != nil {
			return err
		}
	case fileTypeNoCompression:
		if _, err := io.CopyBuffer(destFile.to, io.LimitReader(c.rawReader, size), c.copyBuffer); err != nil {
			return err
		}
	case fileTypeHole:
		if err := appendHole(int(destFile.file.Fd()), destFile.metadata.Name, size); err != nil {
			return err
		}
		if destFile.hash != nil {
			if err := hashHole(destFile.hash, size, c.copyBuffer); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unknown file type %q", c.fileType)
	}
	return nil
}

type recordFsVerityFunc func(string, *os.File) error

type destinationFile struct {
	digester       digest.Digester
	dirfd          int
	file           *os.File
	hash           hash.Hash
	metadata       *fileMetadata
	options        *archive.TarOptions
	skipValidation bool
	to             io.Writer
	recordFsVerity recordFsVerityFunc
}

func openDestinationFile(dirfd int, metadata *fileMetadata, options *archive.TarOptions, skipValidation bool, recordFsVerity recordFsVerityFunc) (*destinationFile, error) {
	file, err := openFileUnderRoot(dirfd, metadata.Name, newFileFlags, 0)
	if err != nil {
		return nil, err
	}

	var digester digest.Digester
	var hash hash.Hash
	var to io.Writer

	if skipValidation {
		to = file
	} else {
		digester = digest.Canonical.Digester()
		hash = digester.Hash()
		to = io.MultiWriter(file, hash)
	}

	return &destinationFile{
		file:           file,
		digester:       digester,
		hash:           hash,
		to:             to,
		metadata:       metadata,
		options:        options,
		dirfd:          dirfd,
		skipValidation: skipValidation,
		recordFsVerity: recordFsVerity,
	}, nil
}

func (d *destinationFile) Close() (Err error) {
	defer func() {
		var roFile *os.File
		var err error

		if d.recordFsVerity != nil {
			roFile, err = reopenFileReadOnly(d.file)
			if err == nil {
				defer roFile.Close()
			} else if Err == nil {
				Err = err
			}
		}

		err = d.file.Close()
		if Err == nil {
			Err = err
		}

		if Err == nil && roFile != nil {
			Err = d.recordFsVerity(d.metadata.Name, roFile)
		}
	}()

	if !d.skipValidation {
		manifestChecksum, err := digest.Parse(d.metadata.Digest)
		if err != nil {
			return err
		}
		if d.digester.Digest() != manifestChecksum {
			return fmt.Errorf("checksum mismatch for %q (got %q instead of %q)", d.file.Name(), d.digester.Digest(), manifestChecksum)
		}
	}

	mode := os.FileMode(d.metadata.Mode)
	if d.options.ForceMask != nil {
		mode = *d.options.ForceMask
	}

	return setFileAttrs(d.dirfd, d.file, mode, d.metadata, d.options, false)
}

func closeDestinationFiles(files chan *destinationFile, errors chan error) {
	for f := range files {
		errors <- f.Close()
	}
	close(errors)
}

func (c *chunkedDiffer) recordFsVerity(path string, roFile *os.File) error {
	if c.useFsVerity == graphdriver.DifferFsVerityDisabled {
		return nil
	}
	// fsverity.EnableVerity doesn't return an error if fs-verity was already
	// enabled on the file.
	err := fsverity.EnableVerity(path, int(roFile.Fd()))
	if err != nil {
		if c.useFsVerity == graphdriver.DifferFsVerityRequired {
			return err
		}

		// If it is not required, ignore the error if the filesystem does not support it.
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.ENOTTY) {
			return nil
		}
	}
	verity, err := fsverity.MeasureVerity(path, int(roFile.Fd()))
	if err != nil {
		return err
	}

	c.fsVerityMutex.Lock()
	c.fsVerityDigests[path] = verity
	c.fsVerityMutex.Unlock()

	return nil
}

func (c *chunkedDiffer) storeMissingFiles(streams chan io.ReadCloser, errs chan error, dirfd int, missingParts []missingPart, options *archive.TarOptions) (Err error) {
	var destFile *destinationFile

	filesToClose := make(chan *destinationFile, 3)
	closeFilesErrors := make(chan error, 2)

	go closeDestinationFiles(filesToClose, closeFilesErrors)
	defer func() {
		close(filesToClose)
		for e := range closeFilesErrors {
			if e != nil && Err == nil {
				Err = e
			}
		}
	}()

	for _, missingPart := range missingParts {
		var part io.ReadCloser
		partCompression := c.fileType
		readingFromLocalFile := false
		switch {
		case missingPart.Hole:
			partCompression = fileTypeHole
		case missingPart.OriginFile != nil:
			var err error
			part, err = missingPart.OriginFile.OpenFile()
			if err != nil {
				return err
			}
			partCompression = fileTypeNoCompression
			readingFromLocalFile = true
		case missingPart.SourceChunk != nil:
			select {
			case p := <-streams:
				part = p
			case err := <-errs:
				if err == nil {
					return errors.New("not enough data returned from the server")
				}
				return err
			}
			if part == nil {
				return errors.New("invalid stream returned")
			}
		default:
			return errors.New("internal error: missing part misses both local and remote data stream")
		}

		for _, mf := range missingPart.Chunks {
			if mf.Gap > 0 {
				limitReader := io.LimitReader(part, mf.Gap)
				_, err := io.CopyBuffer(io.Discard, limitReader, c.copyBuffer)
				if err != nil {
					Err = err
					goto exit
				}
				continue
			}

			if mf.File.Name == "" {
				Err = errors.New("file name empty")
				goto exit
			}

			c.rawReader = nil
			if part != nil {
				limit := mf.CompressedSize
				// If we are reading from a source file, use the uncompressed size to limit the reader, because
				// the compressed size refers to the original layer stream.
				if readingFromLocalFile {
					limit = mf.UncompressedSize
				}
				c.rawReader = io.LimitReader(part, limit)
			}

			compression, err := c.prepareCompressedStreamToFile(partCompression, &mf)
			if err != nil {
				Err = err
				goto exit
			}

			// Open the new file if it is different that what is already
			// opened
			if destFile == nil || destFile.metadata.Name != mf.File.Name {
				var err error
				if destFile != nil {
				cleanup:
					for {
						select {
						case err = <-closeFilesErrors:
							if err != nil {
								Err = err
								goto exit
							}
						default:
							break cleanup
						}
					}
					filesToClose <- destFile
				}
				recordFsVerity := c.recordFsVerity
				if c.useFsVerity == graphdriver.DifferFsVerityDisabled {
					recordFsVerity = nil
				}
				destFile, err = openDestinationFile(dirfd, mf.File, options, c.skipValidation, recordFsVerity)
				if err != nil {
					Err = err
					goto exit
				}
			}

			if err := c.appendCompressedStreamToFile(compression, destFile, mf.UncompressedSize); err != nil {
				Err = err
				goto exit
			}
			if c.rawReader != nil {
				if _, err := io.CopyBuffer(io.Discard, c.rawReader, c.copyBuffer); err != nil {
					Err = err
					goto exit
				}
			}
		}
	exit:
		if part != nil {
			part.Close()
			if Err != nil {
				break
			}
		}
	}

	if destFile != nil {
		return destFile.Close()
	}

	return nil
}

func mergeMissingChunks(missingParts []missingPart, target int) []missingPart {
	getGap := func(missingParts []missingPart, i int) uint64 {
		prev := missingParts[i-1].SourceChunk.Offset + missingParts[i-1].SourceChunk.Length
		return missingParts[i].SourceChunk.Offset - prev
	}

	// simple case: merge chunks from the same file.  Useful to reduce the number of parts to work with later.
	newMissingParts := missingParts[0:1]
	prevIndex := 0
	for i := 1; i < len(missingParts); i++ {
		gap := getGap(missingParts, i)
		if gap == 0 && missingParts[prevIndex].OriginFile == nil &&
			missingParts[i].OriginFile == nil &&
			!missingParts[prevIndex].Hole && !missingParts[i].Hole &&
			len(missingParts[prevIndex].Chunks) == 1 && len(missingParts[i].Chunks) == 1 &&
			missingParts[prevIndex].Chunks[0].File.Name == missingParts[i].Chunks[0].File.Name {
			missingParts[prevIndex].SourceChunk.Length += gap + missingParts[i].SourceChunk.Length
			missingParts[prevIndex].Chunks[0].CompressedSize += missingParts[i].Chunks[0].CompressedSize
			missingParts[prevIndex].Chunks[0].UncompressedSize += missingParts[i].Chunks[0].UncompressedSize
		} else {
			newMissingParts = append(newMissingParts, missingParts[i])
			prevIndex++
		}
	}
	missingParts = newMissingParts

	type gap struct {
		from int
		to   int
		cost uint64
	}
	var requestGaps []gap
	lastOffset := int(-1)
	numberSourceChunks := 0
	for i, c := range missingParts {
		if c.OriginFile != nil || c.Hole {
			// it does not require a network request
			continue
		}
		numberSourceChunks++
		if lastOffset >= 0 {
			prevEnd := missingParts[lastOffset].SourceChunk.Offset + missingParts[lastOffset].SourceChunk.Length
			cost := c.SourceChunk.Offset - prevEnd
			g := gap{
				from: lastOffset,
				to:   i,
				cost: cost,
			}
			requestGaps = append(requestGaps, g)
		}
		lastOffset = i
	}
	sort.Slice(requestGaps, func(i, j int) bool {
		return requestGaps[i].cost < requestGaps[j].cost
	})
	toMergeMap := make([]bool, len(missingParts))
	remainingToMerge := numberSourceChunks - target
	for _, g := range requestGaps {
		if remainingToMerge < 0 && g.cost > autoMergePartsThreshold {
			continue
		}
		for i := g.from + 1; i <= g.to; i++ {
			toMergeMap[i] = true
		}
		remainingToMerge--
	}

	newMissingParts = missingParts[0:1]
	for i := 1; i < len(missingParts); i++ {
		if !toMergeMap[i] {
			newMissingParts = append(newMissingParts, missingParts[i])
		} else {
			gap := getGap(missingParts, i)
			prev := &newMissingParts[len(newMissingParts)-1]
			prev.SourceChunk.Length += gap + missingParts[i].SourceChunk.Length
			prev.Hole = false
			prev.OriginFile = nil
			if gap > 0 {
				gapFile := missingFileChunk{
					Gap: int64(gap),
				}
				prev.Chunks = append(prev.Chunks, gapFile)
			}
			prev.Chunks = append(prev.Chunks, missingParts[i].Chunks...)
		}
	}
	return newMissingParts
}

func (c *chunkedDiffer) retrieveMissingFiles(stream ImageSourceSeekable, dirfd int, missingParts []missingPart, options *archive.TarOptions) error {
	var chunksToRequest []ImageSourceChunk

	calculateChunksToRequest := func() {
		chunksToRequest = []ImageSourceChunk{}
		for _, c := range missingParts {
			if c.OriginFile == nil && !c.Hole {
				chunksToRequest = append(chunksToRequest, *c.SourceChunk)
			}
		}
	}

	missingParts = mergeMissingChunks(missingParts, maxNumberMissingChunks)
	calculateChunksToRequest()

	// There are some missing files.  Prepare a multirange request for the missing chunks.
	var streams chan io.ReadCloser
	var err error
	var errs chan error
	for {
		streams, errs, err = stream.GetBlobAt(chunksToRequest)
		if err == nil {
			break
		}

		if _, ok := err.(ErrBadRequest); ok {
			if len(chunksToRequest) == 1 {
				return err
			}
			// Merge more chunks to request
			missingParts = mergeMissingChunks(missingParts, len(chunksToRequest)/2)
			calculateChunksToRequest()
			continue
		}
		return err
	}

	if err := c.storeMissingFiles(streams, errs, dirfd, missingParts, options); err != nil {
		return err
	}
	return nil
}

type hardLinkToCreate struct {
	dest     string
	dirfd    int
	mode     os.FileMode
	metadata *fileMetadata
}

type findAndCopyFileOptions struct {
	useHardLinks bool
	ostreeRepos  []string
	options      *archive.TarOptions
}

func reopenFileReadOnly(f *os.File) (*os.File, error) {
	path := procPathForFile(f)
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), f.Name()), nil
}

func (c *chunkedDiffer) findAndCopyFile(dirfd int, r *fileMetadata, copyOptions *findAndCopyFileOptions, mode os.FileMode) (bool, error) {
	finalizeFile := func(dstFile *os.File) error {
		if dstFile == nil {
			return nil
		}
		err := setFileAttrs(dirfd, dstFile, mode, r, copyOptions.options, false)
		if err != nil {
			dstFile.Close()
			return err
		}
		var roFile *os.File
		if c.useFsVerity != graphdriver.DifferFsVerityDisabled {
			roFile, err = reopenFileReadOnly(dstFile)
		}
		dstFile.Close()
		if err != nil {
			return err
		}
		if roFile == nil {
			return nil
		}

		defer roFile.Close()
		return c.recordFsVerity(r.Name, roFile)
	}

	found, dstFile, _, err := findFileInOtherLayers(c.layersCache, r, dirfd, copyOptions.useHardLinks)
	if err != nil {
		return false, err
	}
	if found {
		if err := finalizeFile(dstFile); err != nil {
			return false, err
		}
		return true, nil
	}

	found, dstFile, _, err = findFileInOSTreeRepos(r, copyOptions.ostreeRepos, dirfd, copyOptions.useHardLinks)
	if err != nil {
		return false, err
	}
	if found {
		if err := finalizeFile(dstFile); err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}

// makeEntriesFlat collects regular-file entries from mergedEntries, and produces a new list
// where each file content is only represented once, and uses composefs.RegularFilePathForValidatedDigest for its name.
// If flatPathNameMap is not nil, this function writes to it a mapping from filepath.Clean(originalName) to the composefs name.
func makeEntriesFlat(mergedEntries []fileMetadata, flatPathNameMap map[string]string) ([]fileMetadata, error) {
	var new []fileMetadata

	knownFlatPaths := make(map[string]struct{})
	for i := range mergedEntries {
		if mergedEntries[i].Type != TypeReg {
			continue
		}
		if mergedEntries[i].Digest == "" {
			return nil, fmt.Errorf("missing digest for %q", mergedEntries[i].Name)
		}
		digest, err := digest.Parse(mergedEntries[i].Digest)
		if err != nil {
			return nil, fmt.Errorf("invalid digest %q for %q: %w", mergedEntries[i].Digest, mergedEntries[i].Name, err)
		}
		path, err := path.RegularFilePathForValidatedDigest(digest)
		if err != nil {
			return nil, fmt.Errorf("determining physical file path for %q: %w", mergedEntries[i].Name, err)
		}
		if flatPathNameMap != nil {
			flatPathNameMap[filepath.Clean(mergedEntries[i].Name)] = path
		}

		if _, known := knownFlatPaths[path]; known {
			continue
		}
		knownFlatPaths[path] = struct{}{}

		mergedEntries[i].Name = path
		mergedEntries[i].skipSetAttrs = true

		new = append(new, mergedEntries[i])
	}
	return new, nil
}

type streamOrErr struct {
	stream io.ReadCloser
	err    error
}

// ensureAllBlobsDone ensures that all blobs are closed and returns the first error encountered.
func ensureAllBlobsDone(streamsOrErrors chan streamOrErr) (retErr error) {
	for soe := range streamsOrErrors {
		if soe.stream != nil {
			_ = soe.stream.Close()
		} else if retErr == nil {
			retErr = soe.err
		}
	}
	return retErr
}

// getBlobAtConverterGoroutine reads from the streams and errs channels, then sends
// either a stream or an error to the stream channel.  The streams channel is closed when
// there are no more streams and errors to read.
// It ensures that no more than maxStreams streams are returned, and that every item from the
// streams and errs channels is consumed.
func getBlobAtConverterGoroutine(stream chan streamOrErr, streams chan io.ReadCloser, errs chan error, maxStreams int) {
	tooManyStreams := false
	streamsSoFar := 0

	err := errors.New("unexpected error in getBlobAtGoroutine")

	defer func() {
		if err != nil {
			stream <- streamOrErr{err: err}
		}
		close(stream)
	}()

loop:
	for {
		select {
		case p, ok := <-streams:
			if !ok {
				streams = nil
				break loop
			}
			if streamsSoFar >= maxStreams {
				tooManyStreams = true
				_ = p.Close()
				continue
			}
			streamsSoFar++
			stream <- streamOrErr{stream: p}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				break loop
			}
			stream <- streamOrErr{err: err}
		}
	}
	if streams != nil {
		for p := range streams {
			if streamsSoFar >= maxStreams {
				tooManyStreams = true
				_ = p.Close()
				continue
			}
			streamsSoFar++
			stream <- streamOrErr{stream: p}
		}
	}
	if errs != nil {
		for err := range errs {
			stream <- streamOrErr{err: err}
		}
	}
	if tooManyStreams {
		stream <- streamOrErr{err: fmt.Errorf("too many streams returned, got more than %d", maxStreams)}
	}
	err = nil
}

// getBlobAt provides a much more convenient way to consume data returned by ImageSourceSeekable.GetBlobAt.
// GetBlobAt returns two channels, forcing a caller to `select` on both of them — and in Go, reading a closed channel
// always succeeds in select.
// Instead, getBlobAt provides a single channel with all events, which can be consumed conveniently using `range`.
func getBlobAt(is ImageSourceSeekable, chunksToRequest ...ImageSourceChunk) (chan streamOrErr, error) {
	streams, errs, err := is.GetBlobAt(chunksToRequest)
	if err != nil {
		return nil, err
	}
	stream := make(chan streamOrErr)
	go getBlobAtConverterGoroutine(stream, streams, errs, len(chunksToRequest))
	return stream, nil
}

func (c *chunkedDiffer) copyAllBlobToFile(destination *os.File) (digest.Digest, error) {
	streamsOrErrors, err := getBlobAt(c.stream, ImageSourceChunk{Offset: 0, Length: uint64(c.blobSize)})
	if err != nil {
		return "", err
	}

	originalRawDigester := digest.Canonical.Digester()
	for soe := range streamsOrErrors {
		if soe.stream != nil {
			r := io.TeeReader(soe.stream, originalRawDigester.Hash())

			// copy the entire tarball and compute its digest
			_, err = io.CopyBuffer(destination, r, c.copyBuffer)
			_ = soe.stream.Close()
		}
		if soe.err != nil && err == nil {
			err = soe.err
		}
	}
	return originalRawDigester.Digest(), err
}

func typeToOsMode(typ string) (os.FileMode, error) {
	switch typ {
	case TypeReg, TypeLink:
		return 0, nil
	case TypeSymlink:
		return os.ModeSymlink, nil
	case TypeDir:
		return os.ModeDir, nil
	case TypeChar:
		return os.ModeDevice | os.ModeCharDevice, nil
	case TypeBlock:
		return os.ModeDevice, nil
	case TypeFifo:
		return os.ModeNamedPipe, nil
	}
	return 0, fmt.Errorf("unknown file type %q", typ)
}

func (c *chunkedDiffer) ApplyDiff(dest string, options *archive.TarOptions, differOpts *graphdriver.DifferOptions) (graphdriver.DriverWithDifferOutput, error) {
	if c.used {
		return graphdriver.DriverWithDifferOutput{}, fmt.Errorf("internal error: chunked differ already used")
	}
	c.used = true

	defer c.layersCache.release()
	defer func() {
		if c.zstdReader != nil {
			c.zstdReader.Close()
		}
	}()

	c.useFsVerity = differOpts.UseFsVerity

	// stream to use for reading the zstd:chunked or Estargz file.
	stream := c.stream

	var compressedDigest digest.Digest
	var uncompressedDigest digest.Digest

	if c.convertToZstdChunked {
		fd, err := unix.Open(dest, unix.O_TMPFILE|unix.O_RDWR|unix.O_CLOEXEC, 0o600)
		if err != nil {
			return graphdriver.DriverWithDifferOutput{}, &fs.PathError{Op: "open", Path: dest, Err: err}
		}
		blobFile := os.NewFile(uintptr(fd), "blob-file")
		defer func() {
			if blobFile != nil {
				blobFile.Close()
			}
		}()

		// calculate the checksum before accessing the file.
		compressedDigest, err = c.copyAllBlobToFile(blobFile)
		if err != nil {
			return graphdriver.DriverWithDifferOutput{}, err
		}

		if compressedDigest != c.blobDigest {
			return graphdriver.DriverWithDifferOutput{}, fmt.Errorf("invalid digest to convert: expected %q, got %q", c.blobDigest, compressedDigest)
		}

		if _, err := blobFile.Seek(0, io.SeekStart); err != nil {
			return graphdriver.DriverWithDifferOutput{}, err
		}

		tarSize, fileSource, diffID, annotations, err := c.convertTarToZstdChunked(dest, blobFile)
		if err != nil {
			return graphdriver.DriverWithDifferOutput{}, err
		}

		c.uncompressedTarSize = tarSize

		// fileSource is a O_TMPFILE file descriptor, so we
		// need to keep it open until the entire file is processed.
		defer fileSource.Close()

		// Close the file so that the file descriptor is released and the file is deleted.
		blobFile.Close()
		blobFile = nil

		tocDigest, err := toc.GetTOCDigest(annotations)
		if err != nil {
			return graphdriver.DriverWithDifferOutput{}, fmt.Errorf("internal error: parsing just-created zstd:chunked TOC digest: %w", err)
		}
		if tocDigest == nil {
			return graphdriver.DriverWithDifferOutput{}, fmt.Errorf("internal error: just-created zstd:chunked missing TOC digest")
		}
		manifest, toc, tarSplit, tocOffset, err := readZstdChunkedManifest(dest, fileSource, *tocDigest, annotations, false)
		if err != nil {
			return graphdriver.DriverWithDifferOutput{}, fmt.Errorf("read zstd:chunked manifest: %w", err)
		}

		// Use the new file for accessing the zstd:chunked file.
		stream = fileSource

		// fill the chunkedDiffer with the data we just read.
		c.fileType = fileTypeNoCompression
		c.manifest = manifest
		c.toc = toc
		c.tarSplit = tarSplit
		c.tocOffset = tocOffset

		// the file was generated by us and the digest for each file was already computed, no need to validate it again.
		c.skipValidation = true
		// since we retrieved the whole file and it was validated, set the uncompressed digest.
		uncompressedDigest = diffID
	}

	lcd := chunkedLayerData{
		Format: differOpts.Format,
	}

	json := jsoniter.ConfigCompatibleWithStandardLibrary
	lcdBigData, err := json.Marshal(lcd)
	if err != nil {
		return graphdriver.DriverWithDifferOutput{}, err
	}

	// Generate the manifest
	toc := c.toc
	if toc == nil {
		toc_, err := unmarshalToc(c.manifest)
		if err != nil {
			return graphdriver.DriverWithDifferOutput{}, err
		}
		toc = toc_
	}

	output := graphdriver.DriverWithDifferOutput{
		Differ:   c,
		TarSplit: c.tarSplit,
		BigData: map[string][]byte{
			bigDataKey:          c.manifest,
			chunkedLayerDataKey: lcdBigData,
		},
		Artifacts: map[string]any{
			tocKey: toc,
		},
		TOCDigest:          c.tocDigest,
		UncompressedDigest: uncompressedDigest,
		CompressedDigest:   compressedDigest,
		Size:               c.uncompressedTarSize,
	}

	whiteoutConverter := archive.GetWhiteoutConverter(options.WhiteoutFormat, options.WhiteoutData)

	var missingParts []missingPart

	mergedEntries, err := c.mergeTocEntries(c.fileType, toc.Entries)
	if err != nil {
		return output, err
	}

	output.UIDs, output.GIDs = collectIDs(mergedEntries)

	if err := maybeDoIDRemap(mergedEntries, options); err != nil {
		return output, err
	}

	if options.ForceMask != nil {
		uid, gid, mode, err := archive.GetFileOwner(dest)
		if err == nil {
			value := idtools.Stat{
				IDs:  idtools.IDPair{UID: int(uid), GID: int(gid)},
				Mode: os.ModeDir | os.FileMode(mode),
			}
			if err := idtools.SetContainersOverrideXattr(dest, value); err != nil {
				return output, err
			}
		}
	}

	dirfd, err := unix.Open(dest, unix.O_RDONLY|unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return output, &fs.PathError{Op: "open", Path: dest, Err: err}
	}
	dirFile := os.NewFile(uintptr(dirfd), dest)
	defer dirFile.Close()

	var flatPathNameMap map[string]string // = nil
	if differOpts != nil && differOpts.Format == graphdriver.DifferOutputFormatFlat {
		flatPathNameMap = map[string]string{}
		mergedEntries, err = makeEntriesFlat(mergedEntries, flatPathNameMap)
		if err != nil {
			return output, err
		}
		createdDirs := make(map[string]struct{})
		for _, e := range mergedEntries {
			// This hard-codes an assumption that RegularFilePathForValidatedDigest creates paths with exactly one directory component.
			d := filepath.Dir(e.Name)
			if _, found := createdDirs[d]; !found {
				if err := unix.Mkdirat(dirfd, d, 0o755); err != nil {
					return output, &fs.PathError{Op: "mkdirat", Path: d, Err: err}
				}
				createdDirs[d] = struct{}{}
			}
		}
	}

	// hardlinks can point to missing files.  So create them after all files
	// are retrieved
	var hardLinks []hardLinkToCreate

	missingPartsSize, totalChunksSize := int64(0), int64(0)

	copyOptions := findAndCopyFileOptions{
		// When the hard links deduplication is used, file attributes are ignored because setting them
		// modifies the source file as well.
		useHardLinks: c.pullOptions.useHardLinks,
		ostreeRepos:  c.pullOptions.ostreeRepos, // List of OSTree repositories to use for deduplication
		options:      options,
	}

	type copyFileJob struct {
		njob     int
		index    int
		mode     os.FileMode
		metadata *fileMetadata

		found bool
		err   error
	}

	var wg sync.WaitGroup

	copyResults := make([]copyFileJob, len(mergedEntries))

	copyFileJobs := make(chan copyFileJob)
	defer func() {
		if copyFileJobs != nil {
			close(copyFileJobs)
		}
		wg.Wait()
	}()

	for range copyGoRoutines {
		wg.Add(1)
		jobs := copyFileJobs

		go func() {
			defer wg.Done()
			for job := range jobs {
				found, err := c.findAndCopyFile(dirfd, job.metadata, &copyOptions, job.mode)
				job.err = err
				job.found = found
				copyResults[job.njob] = job
			}
		}()
	}

	filesToWaitFor := 0
	for i := range mergedEntries {
		r := &mergedEntries[i]

		mode := os.FileMode(r.Mode)

		t, err := typeToTarType(r.Type)
		if err != nil {
			return output, err
		}

		size := r.Size

		// update also the implementation of ForceMask in pkg/archive
		if options.ForceMask != nil {
			mode = *options.ForceMask

			// special files will be stored as regular files
			if t != tar.TypeDir && t != tar.TypeSymlink && t != tar.TypeReg && t != tar.TypeLink {
				t = tar.TypeReg
				size = 0
			}

			// if the entry will be stored as a directory or a regular file, store in a xattr the original
			// owner and mode.
			if t == tar.TypeDir || t == tar.TypeReg {
				typeMode, err := typeToOsMode(r.Type)
				if err != nil {
					return output, err
				}
				value := idtools.FormatContainersOverrideXattrDevice(r.UID, r.GID, typeMode|fs.FileMode(r.Mode), int(r.Devmajor), int(r.Devminor))
				if r.Xattrs == nil {
					r.Xattrs = make(map[string]string)
				}
				r.Xattrs[idtools.ContainersOverrideXattr] = base64.StdEncoding.EncodeToString([]byte(value))
			}
		}

		r.Name = path.CleanAbsPath(r.Name)
		// do not modify the value of symlinks
		if r.Linkname != "" && t != tar.TypeSymlink {
			r.Linkname = path.CleanAbsPath(r.Linkname)
		}

		if whiteoutConverter != nil {
			hdr := archivetar.Header{
				Typeflag: t,
				Name:     r.Name,
				Linkname: r.Linkname,
				Size:     size,
				Mode:     int64(mode),
				Uid:      r.UID,
				Gid:      r.GID,
			}
			handler := whiteoutHandler{
				Dirfd: dirfd,
				Root:  dest,
			}
			writeFile, err := whiteoutConverter.ConvertReadWithHandler(&hdr, r.Name, &handler)
			if err != nil {
				return output, err
			}
			if !writeFile {
				continue
			}
		}
		switch t {
		case tar.TypeReg:
			// Create directly empty files.
			if size == 0 {
				// Used to have a scope for cleanup.
				createEmptyFile := func() error {
					file, err := openFileUnderRoot(dirfd, r.Name, newFileFlags, 0)
					if err != nil {
						return err
					}
					defer file.Close()
					if err := setFileAttrs(dirfd, file, mode, r, options, false); err != nil {
						return err
					}
					return nil
				}
				if err := createEmptyFile(); err != nil {
					return output, err
				}
				continue
			}

		case tar.TypeDir:
			if r.Name == "/" {
				output.RootDirMode = &mode
			}
			if err := safeMkdir(dirfd, mode, r.Name, r, options); err != nil {
				return output, err
			}
			continue

		case tar.TypeLink:
			dest := dest
			dirfd := dirfd
			mode := mode
			r := r
			hardLinks = append(hardLinks, hardLinkToCreate{
				dest:     dest,
				dirfd:    dirfd,
				mode:     mode,
				metadata: r,
			})
			continue

		case tar.TypeSymlink:
			if err := safeSymlink(dirfd, r); err != nil {
				return output, err
			}
			continue

		case tar.TypeChar:
		case tar.TypeBlock:
		case tar.TypeFifo:
			/* Ignore.  */
		default:
			return output, fmt.Errorf("invalid type %q", t)
		}

		totalChunksSize += size

		if t == tar.TypeReg {
			index := i
			njob := filesToWaitFor
			job := copyFileJob{
				mode:     mode,
				metadata: &mergedEntries[index],
				index:    index,
				njob:     njob,
			}
			copyFileJobs <- job
			filesToWaitFor++
		}
	}

	close(copyFileJobs)
	copyFileJobs = nil

	wg.Wait()

	for _, res := range copyResults[:filesToWaitFor] {
		r := &mergedEntries[res.index]

		if res.err != nil {
			return output, res.err
		}
		// the file was already copied to its destination
		// so nothing left to do.
		if res.found {
			continue
		}

		missingPartsSize += r.Size

		remainingSize := r.Size

		// the file is missing, attempt to find individual chunks.
		for _, chunk := range r.chunks {
			compressedSize := chunk.EndOffset - chunk.Offset
			size := remainingSize
			if chunk.ChunkSize > 0 {
				size = chunk.ChunkSize
			}
			remainingSize = remainingSize - size

			rawChunk := ImageSourceChunk{
				Offset: uint64(chunk.Offset),
				Length: uint64(compressedSize),
			}
			file := missingFileChunk{
				File:             &mergedEntries[res.index],
				CompressedSize:   compressedSize,
				UncompressedSize: size,
			}
			mp := missingPart{
				SourceChunk: &rawChunk,
				Chunks: []missingFileChunk{
					file,
				},
			}

			switch chunk.ChunkType {
			case minimal.ChunkTypeData:
				root, path, offset, err := c.layersCache.findChunkInOtherLayers(chunk)
				if err != nil {
					return output, err
				}
				if offset >= 0 && validateChunkChecksum(chunk, root, path, offset, c.copyBuffer) {
					missingPartsSize -= size
					mp.OriginFile = &originFile{
						Root:   root,
						Path:   path,
						Offset: offset,
					}
				}
			case minimal.ChunkTypeZeros:
				missingPartsSize -= size
				mp.Hole = true
				// Mark all chunks belonging to the missing part as holes
				for i := range mp.Chunks {
					mp.Chunks[i].Hole = true
				}
			}
			missingParts = append(missingParts, mp)
		}
	}
	// There are some missing files.  Prepare a multirange request for the missing chunks.
	if len(missingParts) > 0 {
		if err := c.retrieveMissingFiles(stream, dirfd, missingParts, options); err != nil {
			return output, err
		}
	}

	for _, m := range hardLinks {
		if err := safeLink(m.dirfd, m.mode, m.metadata, options); err != nil {
			return output, err
		}
	}

	// To ensure that consumers of the layer who decompress and read the full tar stream,
	// and consumers who consume the data via the TOC, both see exactly the same data and metadata,
	// compute the UncompressedDigest.
	// c/image will then ensure that this value matches the value in the image config’s RootFS.DiffID, i.e. the image must commit
	// to one UncompressedDigest value for each layer, and that will avoid the ambiguity (in consumers who validate layers against DiffID).
	//
	// c/image also uses the UncompressedDigest as a layer ID, allowing it to use the traditional layer and image IDs.
	//
	// This is, sadly, quite costly: Up to now we might have only have had to write, and digest, only the new/modified files.
	// Here we need to read, and digest, the whole layer, even if almost all of it was already present locally previously.
	// So, really specialized (EXTREMELY RARE) users can opt out of this check using insecureAllowUnpredictableImageContents .
	//
	// Layers without a tar-split (estargz layers and old zstd:chunked layers) can't produce an UncompressedDigest that
	// matches the expected RootFS.DiffID; we always fall back to full pulls, again unless the user opts out
	// via insecureAllowUnpredictableImageContents .
	if output.UncompressedDigest == "" {
		switch {
		case c.pullOptions.insecureAllowUnpredictableImageContents:
			// Oh well.  Skip the costly digest computation.
		case output.TarSplit != nil:
			if _, err := output.TarSplit.Seek(0, io.SeekStart); err != nil {
				return output, err
			}
			metadata := tsStorage.NewJSONUnpacker(output.TarSplit)
			fg := newStagedFileGetter(dirFile, flatPathNameMap)
			digester := digest.Canonical.Digester()
			if err := asm.WriteOutputTarStream(fg, metadata, digester.Hash()); err != nil {
				return output, fmt.Errorf("digesting staged uncompressed stream: %w", err)
			}
			output.UncompressedDigest = digester.Digest()
		default:
			// We are checking for this earlier in NewDiffer, so this should not be reachable.
			return output, fmt.Errorf(`internal error: layer's UncompressedDigest is unknown and "insecure_allow_unpredictable_image_contents" is not set`)
		}
	}

	if totalChunksSize > 0 {
		logrus.Debugf("Missing %d bytes out of %d (%.2f %%)", missingPartsSize, totalChunksSize, float32(missingPartsSize*100.0)/float32(totalChunksSize))
	}

	output.Artifacts[fsVerityDigestsKey] = c.fsVerityDigests

	// on success steal the reference to the tarSplit file
	c.tarSplit = nil

	return output, nil
}

func mustSkipFile(fileType compressedFileType, e minimal.FileMetadata) bool {
	// ignore the metadata files for the estargz format.
	if fileType != fileTypeEstargz {
		return false
	}
	switch e.Name {
	// ignore the metadata files for the estargz format.
	case estargz.PrefetchLandmark, estargz.NoPrefetchLandmark, estargz.TOCTarName:
		return true
	}
	return false
}

func (c *chunkedDiffer) mergeTocEntries(fileType compressedFileType, entries []minimal.FileMetadata) ([]fileMetadata, error) {
	countNextChunks := func(start int) int {
		count := 0
		for _, e := range entries[start:] {
			if e.Type != TypeChunk {
				return count
			}
			count++
		}
		return count
	}

	size := 0
	for _, entry := range entries {
		if mustSkipFile(fileType, entry) {
			continue
		}
		if entry.Type != TypeChunk {
			size++
		}
	}

	mergedEntries := make([]fileMetadata, size)
	m := 0
	for i := 0; i < len(entries); i++ {
		e := fileMetadata{FileMetadata: entries[i]}
		if mustSkipFile(fileType, entries[i]) {
			continue
		}

		if e.Type == TypeChunk {
			return nil, fmt.Errorf("chunk type without a regular file")
		}

		if e.Type == TypeReg {
			nChunks := countNextChunks(i + 1)

			e.chunks = make([]*minimal.FileMetadata, nChunks+1)
			for j := 0; j <= nChunks; j++ {
				// we need a copy here, otherwise we override the
				// .Size later
				copy := entries[i+j]
				e.chunks[j] = &copy
				e.EndOffset = entries[i+j].EndOffset
			}
			i += nChunks
		}
		mergedEntries[m] = e
		m++
	}
	// stargz/estargz doesn't store EndOffset so let's calculate it here
	lastOffset := c.tocOffset
	for i := len(mergedEntries) - 1; i >= 0; i-- {
		if mergedEntries[i].EndOffset == 0 {
			mergedEntries[i].EndOffset = lastOffset
		}
		if mergedEntries[i].Offset != 0 {
			lastOffset = mergedEntries[i].Offset
		}

		lastChunkOffset := mergedEntries[i].EndOffset
		for j := len(mergedEntries[i].chunks) - 1; j >= 0; j-- {
			mergedEntries[i].chunks[j].EndOffset = lastChunkOffset
			mergedEntries[i].chunks[j].Size = mergedEntries[i].chunks[j].EndOffset - mergedEntries[i].chunks[j].Offset
			lastChunkOffset = mergedEntries[i].chunks[j].Offset
		}
	}
	return mergedEntries, nil
}

// validateChunkChecksum checks if the file at $root/$path[offset:chunk.ChunkSize] has the
// same digest as chunk.ChunkDigest
func validateChunkChecksum(chunk *minimal.FileMetadata, root, path string, offset int64, copyBuffer []byte) bool {
	parentDirfd, err := unix.Open(root, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	defer unix.Close(parentDirfd)

	fd, err := openFileUnderRoot(parentDirfd, path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	defer fd.Close()

	if _, err := fd.Seek(offset, io.SeekStart); err != nil {
		return false
	}

	r := io.LimitReader(fd, chunk.ChunkSize)
	digester := digest.Canonical.Digester()

	if _, err := io.CopyBuffer(digester.Hash(), r, copyBuffer); err != nil {
		return false
	}

	digest, err := digest.Parse(chunk.ChunkDigest)
	if err != nil {
		return false
	}

	return digester.Digest() == digest
}

// newStagedFileGetter returns an object usable as storage.FileGetter for rootDir.
// if flatPathNameMap is not nil, it must be used to map logical file names into the backing file paths.
func newStagedFileGetter(rootDir *os.File, flatPathNameMap map[string]string) *stagedFileGetter {
	return &stagedFileGetter{
		rootDir:         rootDir,
		flatPathNameMap: flatPathNameMap,
	}
}

type stagedFileGetter struct {
	rootDir         *os.File
	flatPathNameMap map[string]string // nil, or a map from filepath.Clean()ed tar file names to expected on-filesystem names
}

func (fg *stagedFileGetter) Get(filename string) (io.ReadCloser, error) {
	if fg.flatPathNameMap != nil {
		path, ok := fg.flatPathNameMap[filepath.Clean(filename)]
		if !ok {
			return nil, fmt.Errorf("no path mapping exists for tar entry %q", filename)
		}
		filename = path
	}
	pathFD, err := securejoin.OpenatInRoot(fg.rootDir, filename)
	if err != nil {
		return nil, err
	}
	defer pathFD.Close()
	return securejoin.Reopen(pathFD, unix.O_RDONLY)
}
