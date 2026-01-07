package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/buildah/pkg/parse"
)

// Mirrors path to a tmpfile if path points to a
// file descriptor instead of actual file on filesystem
// reason: operations with file descriptors are can lead
// to edge cases where content on FD is not in a consumable
// state after first consumption.
// returns path as string and bool to confirm if temp file
// was created and needs to be cleaned up.
func MirrorToTempFileIfPathIsDescriptor(file string) (string, bool) {
	// one use-case is discussed here
	// https://github.com/containers/buildah/issues/3070
	if !strings.HasPrefix(file, "/dev/fd/") {
		return file, false
	}
	b, err := os.ReadFile(file)
	if err != nil {
		// if anything goes wrong return original path
		return file, false
	}
	tmpfile, err := os.CreateTemp(parse.GetTempDir(), "buildah-temp-file")
	if err != nil {
		return file, false
	}
	defer tmpfile.Close()
	if _, err := tmpfile.Write(b); err != nil {
		// if anything goes wrong return original path
		return file, false
	}

	return tmpfile.Name(), true
}

// DiscoverContainerfile tries to find a Containerfile or a Dockerfile within the provided `path`.
func DiscoverContainerfile(path string) (foundCtrFile string, err error) {
	// Test for existence of the file
	target, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("discovering Containerfile: %w", err)
	}

	switch mode := target.Mode(); {
	case mode.IsDir():
		// If the path is a real directory, we assume a Containerfile or a Dockerfile within it
		ctrfile := filepath.Join(path, "Containerfile")

		// Test for existence of the Containerfile file
		file, err := os.Stat(ctrfile)
		if err != nil {
			// See if we have a Dockerfile within it
			ctrfile = filepath.Join(path, "Dockerfile")

			// Test for existence of the Dockerfile file
			file, err = os.Stat(ctrfile)
			if err != nil {
				return "", fmt.Errorf("cannot find Containerfile or Dockerfile in context directory: %w", err)
			}
		}

		// The file exists, now verify the correct mode
		if mode := file.Mode(); mode.IsRegular() {
			foundCtrFile = ctrfile
		} else {
			return "", fmt.Errorf("assumed Containerfile %q is not a file", ctrfile)
		}

	case mode.IsRegular():
		// If the context dir is a file, we assume this as Containerfile
		foundCtrFile = path
	}

	return foundCtrFile, nil
}
