//go:build !linux

package archive

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"go.podman.io/storage/pkg/idtools"
	"go.podman.io/storage/pkg/system"
)

func collectFileInfoForChanges(oldDir, newDir string, oldIDMap, newIDMap *idtools.IDMappings) (*FileInfo, *FileInfo, error) {
	var (
		oldRoot, newRoot *FileInfo
		err1, err2       error
		errs             = make(chan error, 2)
	)
	go func() {
		oldRoot, err1 = collectFileInfo(oldDir, oldIDMap)
		errs <- err1
	}()
	go func() {
		newRoot, err2 = collectFileInfo(newDir, newIDMap)
		errs <- err2
	}()

	// block until both routines have returned
	for range 2 {
		if err := <-errs; err != nil {
			return nil, nil, err
		}
	}

	return oldRoot, newRoot, nil
}

func collectFileInfo(sourceDir string, idMappings *idtools.IDMappings) (*FileInfo, error) {
	root := newRootFileInfo(idMappings)

	sourceStat, err := system.Lstat(sourceDir)
	if err != nil {
		return nil, err
	}

	err = filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Rebase path
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// As this runs on the daemon side, file paths are OS specific.
		relPath = filepath.Join(string(os.PathSeparator), relPath)

		// See https://github.com/golang/go/issues/9168 - bug in filepath.Join.
		// Temporary workaround. If the returned path starts with two backslashes,
		// trim it down to a single backslash. Only relevant on Windows.
		if runtime.GOOS == "windows" {
			if strings.HasPrefix(relPath, `\\`) {
				relPath = relPath[1:]
			}
		}

		if relPath == string(os.PathSeparator) {
			return nil
		}

		parent := root.LookUp(filepath.Dir(relPath))
		if parent == nil {
			return fmt.Errorf("collectFileInfo: Unexpectedly no parent for %s", relPath)
		}

		info := &FileInfo{
			name:       filepath.Base(relPath),
			children:   make(map[string]*FileInfo),
			parent:     parent,
			idMappings: idMappings,
		}

		s, err := system.Lstat(path)
		if err != nil {
			return err
		}

		// Don't cross mount points. This ignores file mounts to avoid
		// generating a diff which deletes all files following the
		// mount.
		if s.Dev() != sourceStat.Dev() && s.IsDir() {
			return filepath.SkipDir
		}

		info.stat = s
		info.capability, _ = system.Lgetxattr(path, "security.capability")

		parent.children[info.name] = info

		return nil
	})
	if err != nil {
		return nil, err
	}
	return root, nil
}
