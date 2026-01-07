package dedup

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

type deviceInodePair struct {
	dev uint64
	ino uint64
}

type dedupFiles struct {
	lock          sync.Mutex
	visitedInodes map[deviceInodePair]struct{}
}

func newDedupFiles() (*dedupFiles, error) {
	return &dedupFiles{
		visitedInodes: make(map[deviceInodePair]struct{}),
	}, nil
}

func (d *dedupFiles) recordInode(dev, ino uint64) (bool, error) {
	d.lock.Lock()
	defer d.lock.Unlock()

	di := deviceInodePair{
		dev: dev,
		ino: ino,
	}

	_, visited := d.visitedInodes[di]
	d.visitedInodes[di] = struct{}{}
	return visited, nil
}

// isFirstVisitOf records that the file is being processed.  Returns true if the file was already visited.
func (d *dedupFiles) isFirstVisitOf(fi fs.FileInfo) (bool, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("unable to get raw syscall.Stat_t data")
	}
	return d.recordInode(uint64(st.Dev), st.Ino) //nolint:unconvert
}

// dedup deduplicates the file at src path to dst path
func (d *dedupFiles) dedup(src, dst string, fiDst fs.FileInfo) (uint64, error) {
	srcFile, err := os.OpenFile(src, os.O_RDONLY, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to open destination file: %w", err)
	}
	defer dstFile.Close()

	stSrc, err := srcFile.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to stat source file: %w", err)
	}
	sSrc, ok := stSrc.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("unable to get raw syscall.Stat_t data")
	}
	sDest, ok := fiDst.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("unable to get raw syscall.Stat_t data")
	}
	if sSrc.Dev == sDest.Dev && sSrc.Ino == sDest.Ino {
		// same inode, we are dealing with a hard link, no need to deduplicate
		return 0, nil
	}

	value := unix.FileDedupeRange{
		Src_offset: 0,
		Src_length: uint64(stSrc.Size()),
		Info: []unix.FileDedupeRangeInfo{
			{
				Dest_fd:     int64(dstFile.Fd()),
				Dest_offset: 0,
			},
		},
	}
	err = unix.IoctlFileDedupeRange(int(srcFile.Fd()), &value)
	if err == nil {
		return value.Info[0].Bytes_deduped, nil
	}

	if errors.Is(err, unix.ENOTSUP) {
		return 0, errNotSupported
	}
	return 0, fmt.Errorf("failed to clone file %q: %w", src, err)
}

func readAllFile(path string, info fs.FileInfo, fn func([]byte) (string, error)) (string, error) {
	size := info.Size()
	if size == 0 {
		return fn(nil)
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if size < 4096 {
		// small file, read it all
		data := make([]byte, size)
		_, err = io.ReadFull(file, data)
		if err != nil {
			return "", err
		}
		return fn(data)
	}

	mmap, err := unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		return "", fmt.Errorf("failed to mmap file: %w", err)
	}
	defer func() {
		_ = unix.Munmap(mmap)
	}()

	_ = unix.Madvise(mmap, unix.MADV_SEQUENTIAL)

	return fn(mmap)
}
