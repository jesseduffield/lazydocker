// Copyright 2014 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/moby/go-archive"
	"github.com/moby/patternmatcher"
)

func createTarStream(srcPath, dockerfilePath string) (io.ReadCloser, error) {
	srcPath, err := filepath.Abs(srcPath)
	if err != nil {
		return nil, err
	}

	excludes, err := parseDockerignore(srcPath)
	if err != nil {
		return nil, err
	}

	includes := []string{"."}

	// If .dockerignore mentions .dockerignore or the Dockerfile
	// then make sure we send both files over to the daemon
	// because Dockerfile is, obviously, needed no matter what, and
	// .dockerignore is needed to know if either one needs to be
	// removed.  The deamon will remove them for us, if needed, after it
	// parses the Dockerfile.
	//
	// https://github.com/docker/docker/issues/8330
	//
	forceIncludeFiles := []string{".dockerignore", dockerfilePath}

	for _, includeFile := range forceIncludeFiles {
		if includeFile == "" {
			continue
		}
		keepThem, err := patternmatcher.Matches(includeFile, excludes)
		if err != nil {
			return nil, fmt.Errorf("cannot match .dockerfileignore: '%s', error: %w", includeFile, err)
		}
		if keepThem {
			includes = append(includes, includeFile)
		}
	}

	if err := validateContextDirectory(srcPath, excludes); err != nil {
		return nil, err
	}
	tarOpts := &archive.TarOptions{
		ExcludePatterns: excludes,
		IncludeFiles:    includes,
		Compression:     archive.Uncompressed,
		NoLchown:        true,
	}
	return archive.TarWithOptions(srcPath, tarOpts)
}

// validateContextDirectory checks if all the contents of the directory
// can be read and returns an error if some files can't be read.
// Symlinks which point to non-existing files don't trigger an error
func validateContextDirectory(srcPath string, excludes []string) error {
	return filepath.Walk(filepath.Join(srcPath, "."), func(filePath string, f os.FileInfo, err error) error {
		// skip this directory/file if it's not in the path, it won't get added to the context
		if relFilePath, relErr := filepath.Rel(srcPath, filePath); relErr != nil {
			return relErr
		} else if skip, matchErr := patternmatcher.Matches(relFilePath, excludes); matchErr != nil {
			return matchErr
		} else if skip {
			if f.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if err != nil {
			if os.IsPermission(err) {
				return fmt.Errorf("cannot stat %q: %w", filePath, err)
			}
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		// skip checking if symlinks point to non-existing files, such symlinks can be useful
		// also skip named pipes, because they hanging on open
		if f.Mode()&(os.ModeSymlink|os.ModeNamedPipe) != 0 {
			return nil
		}

		if !f.IsDir() {
			currentFile, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("cannot open %q for reading: %w", filePath, err)
			}
			currentFile.Close()
		}
		return nil
	})
}

func parseDockerignore(root string) ([]string, error) {
	var excludes []string
	ignore, err := os.ReadFile(path.Join(root, ".dockerignore"))
	if err != nil && !os.IsNotExist(err) {
		return excludes, fmt.Errorf("error reading .dockerignore: %w", err)
	}
	excludes = strings.Split(string(ignore), "\n")

	return excludes, nil
}
