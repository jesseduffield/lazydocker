// Package ctime includes a utility for determining file-creation times.
package ctime

import (
	"os"
	"time"
)

// Created returns the file-creation time.
func Created(fi os.FileInfo) time.Time {
	return created(fi)
}
