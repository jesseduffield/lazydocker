package system

import (
	"os"
	"time"
)

// StatT type contains status of a file. It contains metadata
// like permission, size, etc about a file.
type StatT struct {
	mode os.FileMode
	size int64
	mtim time.Time
	platformStatT
}

// Size returns file's size.
func (s StatT) Size() int64 {
	return s.size
}

// Mode returns file's permission mode.
func (s StatT) Mode() os.FileMode {
	return os.FileMode(s.mode)
}

// Mtim returns file's last modification time.
func (s StatT) Mtim() time.Time {
	return time.Time(s.mtim)
}

// UID returns file's user id of owner.
//
// on windows this is always 0 because there is no concept of UID
func (s StatT) UID() uint32 {
	return 0
}

// GID returns file's group id of owner.
//
// on windows this is always 0 because there is no concept of GID
func (s StatT) GID() uint32 {
	return 0
}

// Dev returns a unique identifier for owning filesystem
func (s StatT) Dev() uint64 {
	return 0
}

func (s StatT) IsDir() bool {
	return s.Mode().IsDir()
}

// Stat takes a path to a file and returns
// a system.StatT type pertaining to that file.
//
// Throws an error if the file does not exist
func Stat(path string) (*StatT, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return fromStatT(&fi)
}

// fromStatT converts a os.FileInfo type to a system.StatT type
func fromStatT(fi *os.FileInfo) (*StatT, error) {
	return &StatT{
		size: (*fi).Size(),
		mode: (*fi).Mode(),
		mtim: (*fi).ModTime(),
	}, nil
}
