package dedup

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc64"
	"io/fs"
	"sync"

	"github.com/opencontainers/selinux/pkg/pwalkdir"
	"github.com/sirupsen/logrus"
)

var errNotSupported = errors.New("reflinks are not supported on this platform")

const (
	DedupHashInvalid DedupHashMethod = iota
	DedupHashCRC
	DedupHashFileSize
	DedupHashSHA256
)

type DedupHashMethod int

type DedupOptions struct {
	// HashMethod is the hash function to use to find identical files
	HashMethod DedupHashMethod
}

type DedupResult struct {
	// Deduped represents the total number of bytes saved by deduplication.
	// This value accounts also for all previously deduplicated data, not only the savings
	// from the last run.
	Deduped uint64
}

func getFileChecksum(hashMethod DedupHashMethod, path string, info fs.FileInfo) (string, error) {
	switch hashMethod {
	case DedupHashInvalid:
		return "", fmt.Errorf("invalid hash method: %v", hashMethod)
	case DedupHashFileSize:
		return fmt.Sprintf("%v", info.Size()), nil
	case DedupHashSHA256:
		return readAllFile(path, info, func(buf []byte) (string, error) {
			h := sha256.New()
			if _, err := h.Write(buf); err != nil {
				return "", err
			}
			return string(h.Sum(nil)), nil
		})
	case DedupHashCRC:
		return readAllFile(path, info, func(buf []byte) (string, error) {
			c := crc64.New(crc64.MakeTable(crc64.ECMA))
			if _, err := c.Write(buf); err != nil {
				return "", err
			}
			bufRet := make([]byte, 8)
			binary.BigEndian.PutUint64(bufRet, c.Sum64())
			return string(bufRet), nil
		})
	default:
		return "", fmt.Errorf("unknown hash method: %v", hashMethod)
	}
}

type pathsLocked struct {
	paths []string
	lock  sync.Mutex
}

func DedupDirs(dirs []string, options DedupOptions) (DedupResult, error) {
	res := DedupResult{}
	hashToPaths := make(map[string]*pathsLocked)
	lock := sync.Mutex{} // protects `hashToPaths` and `res`

	dedup, err := newDedupFiles()
	if err != nil {
		return res, err
	}

	for _, dir := range dirs {
		logrus.Debugf("Deduping directory %s", dir)
		if err := pwalkdir.Walk(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			size := uint64(info.Size())
			if size == 0 {
				// do not bother with empty files
				return nil
			}

			// the file was already deduplicated
			if visited, err := dedup.isFirstVisitOf(info); err != nil {
				return err
			} else if visited {
				return nil
			}

			h, err := getFileChecksum(options.HashMethod, path, info)
			if err != nil {
				return err
			}

			lock.Lock()
			item, foundItem := hashToPaths[h]
			if !foundItem {
				item = &pathsLocked{paths: []string{path}}
				hashToPaths[h] = item
				lock.Unlock()
				return nil
			}
			item.lock.Lock()
			lock.Unlock()

			dedupBytes, err := func() (uint64, error) { // function to have a scope for the defer statement
				defer item.lock.Unlock()

				var dedupBytes uint64
				for _, src := range item.paths {
					deduped, err := dedup.dedup(src, path, info)
					if err == nil && deduped > 0 {
						logrus.Debugf("Deduped %q -> %q (%d bytes)", src, path, deduped)
						dedupBytes += deduped
						break
					}
					logrus.Debugf("Failed to deduplicate: %v", err)
					if errors.Is(err, errNotSupported) {
						return dedupBytes, err
					}
				}
				if dedupBytes == 0 {
					item.paths = append(item.paths, path)
				}
				return dedupBytes, nil
			}()
			if err != nil {
				return err
			}

			lock.Lock()
			res.Deduped += dedupBytes
			lock.Unlock()
			return nil
		}); err != nil {
			// if reflinks are not supported, return immediately without errors
			if errors.Is(err, errNotSupported) {
				return res, nil
			}
			return res, err
		}
	}
	return res, nil
}
