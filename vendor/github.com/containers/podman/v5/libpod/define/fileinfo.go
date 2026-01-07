package define

import (
	"os"
	"time"
)

// FileInfo describes the attributes of a file or directory.
type FileInfo struct {
	Name       string      `json:"name"`
	Size       int64       `json:"size"`
	Mode       os.FileMode `json:"mode"`
	ModTime    time.Time   `json:"mtime"`
	IsDir      bool        `json:"isDir"`
	LinkTarget string      `json:"linkTarget"`
}
