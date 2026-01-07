package chunked

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/go-units"
	jsoniter "github.com/json-iterator/go"
	digest "github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	storage "go.podman.io/storage"
	graphdriver "go.podman.io/storage/drivers"
	"go.podman.io/storage/pkg/chunked/internal/minimal"
	"go.podman.io/storage/pkg/ioutils"
	"golang.org/x/sys/unix"
)

const (
	cacheKey     = "chunked-manifest-cache"
	cacheVersion = 3

	digestSha256Empty = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// Using 3 hashes functions and n/m = 10 gives a false positive rate of ~1.7%:
	// https://pages.cs.wisc.edu/~cao/papers/summary-cache/node8.html
	bloomFilterScale  = 10 // how much bigger is the bloom filter than the number of entries
	bloomFilterHashes = 3  // number of hash functions for the bloom filter

	maxTagsLen = 100 * units.MB // max size for tags len
)

type cacheFile struct {
	tagLen      int
	digestLen   int
	fnamesLen   int
	tags        []byte
	vdata       []byte
	fnames      []byte
	bloomFilter *bloomFilter
}

type layer struct {
	id        string
	cacheFile *cacheFile
	target    string
	// mmapBuffer is nil when the cache file is fully loaded in memory.
	// Otherwise it points to a mmap'ed buffer that is referenced by cacheFile.vdata.
	mmapBuffer []byte

	// reloadWithMmap is set when the current process generates the cache file,
	// and cacheFile reuses the memory buffer used by the generation function.
	// Next time the layer cache is used, attempt to reload the file using
	// mmap.
	reloadWithMmap bool
}

type layersCache struct {
	layers []*layer
	refs   int
	store  storage.Store
	mutex  sync.RWMutex
}

var (
	cacheMutex sync.Mutex
	cache      *layersCache
)

func (c *layer) release() {
	runtime.SetFinalizer(c, nil)
	if c.mmapBuffer != nil {
		if err := unix.Munmap(c.mmapBuffer); err != nil {
			logrus.Warnf("Error Munmap: layer %q: %v", c.id, err)
		}
		c.mmapBuffer = nil
	}
}

func layerFinalizer(c *layer) {
	c.release()
}

func (c *layersCache) release() {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	c.refs--
	if c.refs != 0 {
		return
	}
	for _, l := range c.layers {
		l.release()
	}
	cache = nil
}

func getLayersCacheRef(store storage.Store) *layersCache {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	if cache != nil && cache.store == store {
		cache.refs++
		return cache
	}
	cache = &layersCache{
		store: store,
		refs:  1,
	}
	return cache
}

func getLayersCache(store storage.Store) (*layersCache, error) {
	c := getLayersCacheRef(store)

	if err := c.load(); err != nil {
		c.release()
		return nil, err
	}
	return c, nil
}

// loadLayerBigData attempts to load the specified cacheKey from a file and mmap its content.
// If the cache is not backed by a file, then it loads the entire content in memory.
// Returns the cache content, and if mmap'ed, the mmap buffer to Munmap.
func (c *layersCache) loadLayerBigData(layerID, bigDataKey string) ([]byte, []byte, error) {
	inputFile, err := c.store.LayerBigData(layerID, bigDataKey)
	if err != nil {
		return nil, nil, err
	}
	defer inputFile.Close()

	// if the cache is backed by a file, attempt to mmap it.
	if osFile, ok := inputFile.(*os.File); ok {
		st, err := osFile.Stat()
		if err != nil {
			logrus.Warningf("Error stat'ing cache file for layer %q: %v", layerID, err)
			goto fallback
		}
		size := st.Size()
		if size == 0 {
			logrus.Warningf("Cache file size is zero for layer %q: %v", layerID, err)
			goto fallback
		}
		buf, err := unix.Mmap(int(osFile.Fd()), 0, int(size), unix.PROT_READ, unix.MAP_SHARED)
		if err != nil {
			logrus.Warningf("Error mmap'ing cache file for layer %q: %v", layerID, err)
			goto fallback
		}
		// best effort advise to the kernel.
		_ = unix.Madvise(buf, unix.MADV_RANDOM)

		return buf, buf, nil
	}
fallback:
	buf, err := io.ReadAll(inputFile)
	return buf, nil, err
}

func makeBinaryDigest(stringDigest string) ([]byte, error) {
	d, err := digest.Parse(stringDigest)
	if err != nil {
		return nil, err
	}
	digestBytes, err := hex.DecodeString(d.Encoded())
	if err != nil {
		return nil, err
	}
	algo := []byte(d.Algorithm())
	buf := make([]byte, 0, len(algo)+1+len(digestBytes))
	buf = append(buf, algo...)
	buf = append(buf, ':')
	buf = append(buf, digestBytes...)
	return buf, nil
}

// loadLayerCache attempts to load the cache file for the specified layer.
// If the cache file is not present or it it using a different cache file version, then
// the function returns (nil, nil).
func (c *layersCache) loadLayerCache(layerID string) (_ *layer, errRet error) {
	buffer, mmapBuffer, err := c.loadLayerBigData(layerID, cacheKey)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// there is no existing cache to load
	if err != nil || buffer == nil {
		return nil, nil
	}
	defer func() {
		if errRet != nil && mmapBuffer != nil {
			if err := unix.Munmap(mmapBuffer); err != nil {
				logrus.Warnf("Error Munmap: layer %q: %v", layerID, err)
			}
		}
	}()
	cacheFile, err := readCacheFileFromMemory(buffer)
	if err != nil {
		return nil, err
	}
	if cacheFile == nil {
		return nil, nil
	}
	return c.createLayer(layerID, cacheFile, mmapBuffer)
}

// createCacheFileFromTOC attempts to create a cache file for the specified layer.
// If a TOC is not available, the cache won't be created and nil is returned.
func (c *layersCache) createCacheFileFromTOC(layerID string) (*layer, error) {
	clFile, err := c.store.LayerBigData(layerID, chunkedLayerDataKey)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var lcd chunkedLayerData
	if err == nil && clFile != nil {
		defer clFile.Close()
		cl, err := io.ReadAll(clFile)
		if err != nil {
			return nil, fmt.Errorf("open manifest file: %w", err)
		}
		json := jsoniter.ConfigCompatibleWithStandardLibrary

		if err := json.Unmarshal(cl, &lcd); err != nil {
			return nil, err
		}
	}
	manifestReader, err := c.store.LayerBigData(layerID, bigDataKey)
	if err != nil {
		// the cache file is not needed since there is no manifest file.
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer manifestReader.Close()

	manifest, err := io.ReadAll(manifestReader)
	if err != nil {
		return nil, fmt.Errorf("read manifest file: %w", err)
	}

	cacheFile, err := writeCache(manifest, lcd.Format, layerID, c.store)
	if err != nil {
		return nil, err
	}
	l, err := c.createLayer(layerID, cacheFile, nil)
	if err != nil {
		return nil, err
	}
	l.reloadWithMmap = true
	return l, nil
}

func (c *layersCache) load() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	loadedLayers := make(map[string]*layer)
	for _, r := range c.layers {
		loadedLayers[r.id] = r
	}
	allLayers, err := c.store.Layers()
	if err != nil {
		return err
	}

	var newLayers []*layer
	for _, r := range allLayers {
		// The layer is present in the store and it is already loaded.  Attempt to
		// reuse it if mmap'ed.
		if l, found := loadedLayers[r.ID]; found {
			// If the layer is not marked for re-load, move it to newLayers.
			if !l.reloadWithMmap {
				delete(loadedLayers, r.ID)
				newLayers = append(newLayers, l)
				continue
			}
		}
		// try to read the existing cache file.
		l, err := c.loadLayerCache(r.ID)
		if err != nil {
			logrus.Infof("Error loading cache file for layer %q: %v", r.ID, err)
		}
		if l != nil {
			newLayers = append(newLayers, l)
			continue
		}

		if r.ReadOnly {
			// If the layer is coming from a read-only store, do not attempt
			// to write to it.
			// Therefore, we won’t find any matches in read-only-store layers,
			// unless the read-only store layer comes prepopulated with cacheKey data.
			continue
		}

		// the cache file is either not present or broken.  Try to generate it from the TOC.
		l, err = c.createCacheFileFromTOC(r.ID)
		if err != nil && !errors.Is(err, storage.ErrLayerUnknown) {
			logrus.Warningf("Error creating cache file for layer %q: %v", r.ID, err)
		}
		if l != nil {
			newLayers = append(newLayers, l)
		}
	}
	// The layers that are still in loadedLayers are either stale or fully loaded in memory.  Clean them up.
	for _, l := range loadedLayers {
		l.release()
	}
	c.layers = newLayers
	return nil
}

// calculateHardLinkFingerprint calculates a hash that can be used to verify if a file
// is usable for deduplication with hardlinks.
// To calculate the digest, it uses the file payload digest, UID, GID, mode and xattrs.
func calculateHardLinkFingerprint(f *fileMetadata) (string, error) {
	digester := digest.Canonical.Digester()

	modeString := fmt.Sprintf("%d:%d:%o", f.UID, f.GID, f.Mode)
	hash := digester.Hash()

	if _, err := hash.Write([]byte(f.Digest)); err != nil {
		return "", err
	}

	if _, err := hash.Write([]byte(modeString)); err != nil {
		return "", err
	}

	if len(f.Xattrs) > 0 {
		keys := make([]string, 0, len(f.Xattrs))
		for k := range f.Xattrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			if _, err := hash.Write([]byte(k)); err != nil {
				return "", err
			}
			if _, err := hash.Write([]byte(f.Xattrs[k])); err != nil {
				return "", err
			}
		}
	}
	return string(digester.Digest()), nil
}

// generateFileLocation generates a file location in the form $OFFSET$LEN$PATH_POS
func generateFileLocation(pathPos int, offset, len uint64) []byte {
	var buf []byte

	buf = binary.AppendUvarint(buf, uint64(pathPos))
	buf = binary.AppendUvarint(buf, offset)
	buf = binary.AppendUvarint(buf, len)

	return buf
}

// parseFileLocation reads what was written by generateFileLocation.
func parseFileLocation(locationData []byte) (int, uint64, uint64, error) {
	reader := bytes.NewReader(locationData)

	pathPos, err := binary.ReadUvarint(reader)
	if err != nil {
		return 0, 0, 0, err
	}

	offset, err := binary.ReadUvarint(reader)
	if err != nil {
		return 0, 0, 0, err
	}

	len, err := binary.ReadUvarint(reader)
	if err != nil {
		return 0, 0, 0, err
	}

	return int(pathPos), offset, len, nil
}

// appendTag appends the $OFFSET$LEN information to the provided $DIGEST.
// The [OFFSET; LEN] points to the variable length data where the file locations
// are stored.  $DIGEST has length digestLen stored in the cache file file header.
func appendTag(digest []byte, offset, len uint64) ([]byte, error) {
	digest = binary.LittleEndian.AppendUint64(digest, offset)
	digest = binary.LittleEndian.AppendUint64(digest, len)
	return digest, nil
}

type setBigData interface {
	// SetLayerBigData stores a (possibly large) chunk of named data
	SetLayerBigData(id, key string, data io.Reader) error
}

func bloomFilterFromTags(tags [][]byte, digestLen int) *bloomFilter {
	bloomFilter := newBloomFilter(len(tags)*bloomFilterScale, bloomFilterHashes)
	for _, t := range tags {
		bloomFilter.add(t[:digestLen])
	}
	return bloomFilter
}

func writeCacheFileToWriter(writer io.Writer, bloomFilter *bloomFilter, tags [][]byte, tagLen, digestLen int, vdata, fnames bytes.Buffer, tagsBuffer *bytes.Buffer) error {
	sort.Slice(tags, func(i, j int) bool {
		return bytes.Compare(tags[i], tags[j]) == -1
	})
	for _, t := range tags {
		if _, err := tagsBuffer.Write(t); err != nil {
			return err
		}
	}

	// version
	if err := binary.Write(writer, binary.LittleEndian, uint64(cacheVersion)); err != nil {
		return err
	}

	// len of a tag
	if err := binary.Write(writer, binary.LittleEndian, uint64(tagLen)); err != nil {
		return err
	}

	// len of a digest
	if err := binary.Write(writer, binary.LittleEndian, uint64(digestLen)); err != nil {
		return err
	}

	// bloom filter
	if err := bloomFilter.writeTo(writer); err != nil {
		return err
	}

	// tags length
	if err := binary.Write(writer, binary.LittleEndian, uint64(tagsBuffer.Len())); err != nil {
		return err
	}

	// vdata length
	if err := binary.Write(writer, binary.LittleEndian, uint64(vdata.Len())); err != nil {
		return err
	}

	// fnames length
	if err := binary.Write(writer, binary.LittleEndian, uint64(fnames.Len())); err != nil {
		return err
	}

	// tags
	if _, err := writer.Write(tagsBuffer.Bytes()); err != nil {
		return err
	}

	// variable length data
	if _, err := writer.Write(vdata.Bytes()); err != nil {
		return err
	}

	// file names
	if _, err := writer.Write(fnames.Bytes()); err != nil {
		return err
	}

	return nil
}

// writeCache write a cache for the layer ID.
// It generates a sorted list of digests with their offset to the path location and offset.
// The same cache is used to lookup files, chunks and candidates for deduplication with hard links.
// There are 3 kind of digests stored:
// - digest(file.payload))
// - digest(digest(file.payload) + file.UID + file.GID + file.mode + file.xattrs)
// - digest(i) for each i in chunks(file payload)
func writeCache(manifest []byte, format graphdriver.DifferOutputFormat, id string, dest setBigData) (*cacheFile, error) {
	var vdata, tagsBuffer, fnames bytes.Buffer
	tagLen := 0
	digestLen := 0

	toc, err := prepareCacheFile(manifest, format)
	if err != nil {
		return nil, err
	}

	fnamesMap := make(map[string]int)
	getFileNamePosition := func(name string) (int, error) {
		if pos, found := fnamesMap[name]; found {
			return pos, nil
		}
		pos := fnames.Len()
		fnamesMap[name] = pos

		if err := binary.Write(&fnames, binary.LittleEndian, uint32(len(name))); err != nil {
			return 0, err
		}
		if _, err := fnames.WriteString(name); err != nil {
			return 0, err
		}
		return pos, nil
	}

	var tags [][]byte
	for _, k := range toc {
		if k.Digest != "" {
			digest, err := makeBinaryDigest(k.Digest)
			if err != nil {
				return nil, err
			}
			fileNamePos, err := getFileNamePosition(k.Name)
			if err != nil {
				return nil, err
			}
			location := generateFileLocation(fileNamePos, 0, uint64(k.Size))
			off := uint64(vdata.Len())
			l := uint64(len(location))

			tag, err := appendTag(digest, off, l)
			if err != nil {
				return nil, err
			}
			if tagLen == 0 {
				tagLen = len(tag)
			}
			if tagLen != len(tag) {
				return nil, errors.New("digest with different length found")
			}
			tags = append(tags, tag)

			fp, err := calculateHardLinkFingerprint(k)
			if err != nil {
				return nil, err
			}
			digestHardLink, err := makeBinaryDigest(fp)
			if err != nil {
				return nil, err
			}
			tag, err = appendTag(digestHardLink, off, l)
			if err != nil {
				return nil, err
			}
			if tagLen != len(tag) {
				return nil, errors.New("digest with different length found")
			}
			tags = append(tags, tag)

			if _, err := vdata.Write(location); err != nil {
				return nil, err
			}
			digestLen = len(digestHardLink)
		}
		if k.ChunkDigest != "" {
			fileNamePos, err := getFileNamePosition(k.Name)
			if err != nil {
				return nil, err
			}
			location := generateFileLocation(fileNamePos, uint64(k.ChunkOffset), uint64(k.ChunkSize))
			off := uint64(vdata.Len())
			l := uint64(len(location))

			digest, err := makeBinaryDigest(k.ChunkDigest)
			if err != nil {
				return nil, err
			}
			d, err := appendTag(digest, off, l)
			if err != nil {
				return nil, err
			}
			if tagLen == 0 {
				tagLen = len(d)
			}
			if tagLen != len(d) {
				return nil, errors.New("digest with different length found")
			}
			tags = append(tags, d)

			if _, err := vdata.Write(location); err != nil {
				return nil, err
			}
			digestLen = len(digest)
		}
	}

	bloomFilter := bloomFilterFromTags(tags, digestLen)

	pipeReader, pipeWriter := io.Pipe()
	errChan := make(chan error, 1)
	go func() {
		defer pipeWriter.Close()
		defer close(errChan)

		errChan <- writeCacheFileToWriter(pipeWriter, bloomFilter, tags, tagLen, digestLen, vdata, fnames, &tagsBuffer)
	}()
	defer pipeReader.Close()

	counter := ioutils.NewWriteCounter(io.Discard)

	r := io.TeeReader(pipeReader, counter)

	if err := dest.SetLayerBigData(id, cacheKey, r); err != nil {
		return nil, err
	}

	if err := <-errChan; err != nil {
		return nil, err
	}

	logrus.Debugf("Written lookaside cache for layer %q with length %v", id, counter.Count)

	return &cacheFile{
		digestLen:   digestLen,
		tagLen:      tagLen,
		tags:        tagsBuffer.Bytes(),
		vdata:       vdata.Bytes(),
		fnames:      fnames.Bytes(),
		fnamesLen:   len(fnames.Bytes()),
		bloomFilter: bloomFilter,
	}, nil
}

// readCacheFileFromMemory reads a cache file from a buffer.
// It can return (nil, nil) if the cache file uses a different file version that the one currently supported.
func readCacheFileFromMemory(bigDataBuffer []byte) (*cacheFile, error) {
	bigData := bytes.NewReader(bigDataBuffer)

	var version, tagLen, digestLen, tagsLen, fnamesLen, vdataLen uint64
	if err := binary.Read(bigData, binary.LittleEndian, &version); err != nil {
		return nil, err
	}
	if version != cacheVersion {
		return nil, nil //nolint: nilnil
	}
	if err := binary.Read(bigData, binary.LittleEndian, &tagLen); err != nil {
		return nil, err
	}
	if err := binary.Read(bigData, binary.LittleEndian, &digestLen); err != nil {
		return nil, err
	}

	bloomFilter, err := readBloomFilter(bigData)
	if err != nil {
		return nil, err
	}

	if err := binary.Read(bigData, binary.LittleEndian, &tagsLen); err != nil {
		return nil, err
	}
	if err := binary.Read(bigData, binary.LittleEndian, &vdataLen); err != nil {
		return nil, err
	}

	if err := binary.Read(bigData, binary.LittleEndian, &fnamesLen); err != nil {
		return nil, err
	}

	if tagsLen > maxTagsLen {
		return nil, fmt.Errorf("tags len %d exceeds the maximum allowed size %d", tagsLen, maxTagsLen)
	}
	if digestLen > tagLen {
		return nil, fmt.Errorf("digest len %d exceeds the tag len %d", digestLen, tagLen)
	}

	tags := make([]byte, tagsLen)
	if _, err := bigData.Read(tags); err != nil {
		return nil, err
	}

	// retrieve the unread part of the buffer.
	remaining := bigDataBuffer[len(bigDataBuffer)-bigData.Len():]

	if vdataLen >= uint64(len(remaining)) {
		return nil, fmt.Errorf("vdata len %d exceeds the remaining buffer size %d", vdataLen, len(remaining))
	}

	vdata := remaining[:vdataLen]
	fnames := remaining[vdataLen:]

	return &cacheFile{
		bloomFilter: bloomFilter,
		digestLen:   int(digestLen),
		fnames:      fnames,
		fnamesLen:   int(fnamesLen),
		tagLen:      int(tagLen),
		tags:        tags,
		vdata:       vdata,
	}, nil
}

func prepareCacheFile(manifest []byte, format graphdriver.DifferOutputFormat) ([]*fileMetadata, error) {
	toc, err := unmarshalToc(manifest)
	if err != nil {
		// ignore errors here.  They might be caused by a different manifest format.
		logrus.Debugf("could not unmarshal manifest: %v", err)
		return nil, nil //nolint: nilnil
	}

	var entries []fileMetadata
	for i := range toc.Entries {
		entries = append(entries, fileMetadata{
			FileMetadata: toc.Entries[i],
		})
	}

	switch format {
	case graphdriver.DifferOutputFormatDir:
	case graphdriver.DifferOutputFormatFlat:
		entries, err = makeEntriesFlat(entries, nil)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown format %q", format)
	}

	var r []*fileMetadata
	chunkSeen := make(map[string]bool)
	for i := range entries {
		d := entries[i].Digest
		if d != "" {
			r = append(r, &entries[i])
			continue
		}

		// chunks do not use hard link dedup so keeping just one candidate is enough
		cd := toc.Entries[i].ChunkDigest
		if cd != "" && !chunkSeen[cd] {
			r = append(r, &entries[i])
			chunkSeen[cd] = true
		}
	}

	return r, nil
}

func (c *layersCache) createLayer(id string, cacheFile *cacheFile, mmapBuffer []byte) (*layer, error) {
	target, err := c.store.DifferTarget(id)
	if err != nil {
		return nil, fmt.Errorf("get checkout directory layer %q: %w", id, err)
	}
	l := &layer{
		id:         id,
		cacheFile:  cacheFile,
		target:     target,
		mmapBuffer: mmapBuffer,
	}
	if mmapBuffer != nil {
		runtime.SetFinalizer(l, layerFinalizer)
	}
	return l, nil
}

func findBinaryTag(binaryDigest []byte, cacheFile *cacheFile) (bool, uint64, uint64) {
	nElements := len(cacheFile.tags) / cacheFile.tagLen

	i := sort.Search(nElements, func(i int) bool {
		d := cacheFile.tags[i*cacheFile.tagLen : i*cacheFile.tagLen+cacheFile.digestLen]
		return bytes.Compare(d, binaryDigest) >= 0
	})
	if i < nElements {
		d := cacheFile.tags[i*cacheFile.tagLen : i*cacheFile.tagLen+cacheFile.digestLen]
		if bytes.Equal(binaryDigest, d) {
			startOff := i*cacheFile.tagLen + cacheFile.digestLen

			// check for corrupted data, there must be 2 u64 (off and len) after the digest.
			if cacheFile.tagLen < cacheFile.digestLen+16 {
				return false, 0, 0
			}

			offsetAndLen := cacheFile.tags[startOff : (i+1)*cacheFile.tagLen]

			off := binary.LittleEndian.Uint64(offsetAndLen[:8])
			len := binary.LittleEndian.Uint64(offsetAndLen[8:16])

			return true, off, len
		}
	}
	return false, 0, 0
}

func (c *layersCache) findDigestInternal(digest string) (string, string, int64, error) {
	if digest == "" {
		return "", "", -1, nil
	}

	c.mutex.RLock()
	defer c.mutex.RUnlock()

	binaryDigest, err := makeBinaryDigest(digest)
	if err != nil {
		return "", "", 0, err
	}

	for _, layer := range c.layers {
		if !layer.cacheFile.bloomFilter.maybeContains(binaryDigest) {
			continue
		}
		found, off, tagLen := findBinaryTag(binaryDigest, layer.cacheFile)
		if found {
			if uint64(len(layer.cacheFile.vdata)) < off+tagLen {
				return "", "", 0, fmt.Errorf("corrupted cache file for layer %q", layer.id)
			}
			fileLocationData := layer.cacheFile.vdata[off : off+tagLen]

			fnamePosition, offFile, _, err := parseFileLocation(fileLocationData)
			if err != nil {
				return "", "", 0, fmt.Errorf("corrupted cache file for layer %q", layer.id)
			}

			if len(layer.cacheFile.fnames) < fnamePosition+4 {
				return "", "", 0, fmt.Errorf("corrupted cache file for layer %q", layer.id)
			}
			lenPath := int(binary.LittleEndian.Uint32(layer.cacheFile.fnames[fnamePosition : fnamePosition+4]))

			if len(layer.cacheFile.fnames) < fnamePosition+lenPath+4 {
				return "", "", 0, fmt.Errorf("corrupted cache file for layer %q", layer.id)
			}
			path := string(layer.cacheFile.fnames[fnamePosition+4 : fnamePosition+lenPath+4])

			// parts[1] is the chunk length, currently unused.
			return layer.target, path, int64(offFile), nil
		}
	}

	return "", "", -1, nil
}

// findFileInOtherLayers finds the specified file in other layers.
// file is the file to look for.
func (c *layersCache) findFileInOtherLayers(file *fileMetadata, useHardLinks bool) (string, string, error) {
	digest := file.Digest
	if useHardLinks {
		var err error
		digest, err = calculateHardLinkFingerprint(file)
		if err != nil {
			return "", "", err
		}
	}
	target, name, off, err := c.findDigestInternal(digest)
	if off == 0 {
		return target, name, err
	}
	return "", "", nil
}

func (c *layersCache) findChunkInOtherLayers(chunk *minimal.FileMetadata) (string, string, int64, error) {
	return c.findDigestInternal(chunk.ChunkDigest)
}

func unmarshalToc(manifest []byte) (*minimal.TOC, error) {
	var toc minimal.TOC

	iter := jsoniter.ParseBytes(jsoniter.ConfigFastest, manifest)

	for field := iter.ReadObject(); field != ""; field = iter.ReadObject() {
		switch strings.ToLower(field) {
		case "version":
			toc.Version = iter.ReadInt()

		case "entries":
			for iter.ReadArray() {
				var m minimal.FileMetadata
				for field := iter.ReadObject(); field != ""; field = iter.ReadObject() {
					switch strings.ToLower(field) {
					case "type":
						m.Type = iter.ReadString()
					case "name":
						m.Name = iter.ReadString()
					case "linkname":
						m.Linkname = iter.ReadString()
					case "mode":
						m.Mode = iter.ReadInt64()
					case "size":
						m.Size = iter.ReadInt64()
					case "uid":
						m.UID = iter.ReadInt()
					case "gid":
						m.GID = iter.ReadInt()
					case "modtime":
						time, err := time.Parse(time.RFC3339, iter.ReadString())
						if err != nil {
							return nil, err
						}
						m.ModTime = &time
					case "accesstime":
						time, err := time.Parse(time.RFC3339, iter.ReadString())
						if err != nil {
							return nil, err
						}
						m.AccessTime = &time
					case "changetime":
						time, err := time.Parse(time.RFC3339, iter.ReadString())
						if err != nil {
							return nil, err
						}
						m.ChangeTime = &time
					case "devmajor":
						m.Devmajor = iter.ReadInt64()
					case "devminor":
						m.Devminor = iter.ReadInt64()
					case "digest":
						m.Digest = iter.ReadString()
					case "offset":
						m.Offset = iter.ReadInt64()
					case "endoffset":
						m.EndOffset = iter.ReadInt64()
					case "chunksize":
						m.ChunkSize = iter.ReadInt64()
					case "chunkoffset":
						m.ChunkOffset = iter.ReadInt64()
					case "chunkdigest":
						m.ChunkDigest = iter.ReadString()
					case "chunktype":
						m.ChunkType = iter.ReadString()
					case "xattrs":
						m.Xattrs = make(map[string]string)
						for key := iter.ReadObject(); key != ""; key = iter.ReadObject() {
							m.Xattrs[key] = iter.ReadString()
						}
					default:
						iter.Skip()
					}
				}
				if m.Type == TypeReg && m.Size == 0 && m.Digest == "" {
					m.Digest = digestSha256Empty
				}
				toc.Entries = append(toc.Entries, m)
			}

		case "tarsplitdigest": // strings.ToLower("tarSplitDigest")
			s := iter.ReadString()
			d, err := digest.Parse(s)
			if err != nil {
				return nil, fmt.Errorf("invalid tarSplitDigest %q: %w", s, err)
			}
			toc.TarSplitDigest = d

		default:
			iter.Skip()
		}
	}

	// validate there is no extra data in the provided input.  This is a security measure to avoid
	// that the digest we calculate for the TOC refers to the entire document.
	if iter.Error != nil && iter.Error != io.EOF {
		return nil, iter.Error
	}
	if iter.WhatIsNext() != jsoniter.InvalidValue || !errors.Is(iter.Error, io.EOF) {
		return nil, fmt.Errorf("unexpected data after manifest")
	}

	return &toc, nil
}
