// Package hooks implements CRI-O's hook handling.
package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	old "go.podman.io/common/pkg/hooks/0.1.0"
	current "go.podman.io/common/pkg/hooks/1.0.0"
)

type reader func(content []byte) (*current.Hook, error)

var (
	// ErrNoJSONSuffix represents hook-add attempts where the filename
	// does not end in '.json'.
	ErrNoJSONSuffix = errors.New("hook filename does not end in '.json'")

	// Readers registers per-version hook readers.
	Readers = map[string]reader{}
)

// Read reads a hook JSON file, verifies it, and returns the hook configuration.
func Read(path string, extensionStages []string) (*current.Hook, error) {
	if !strings.HasSuffix(path, ".json") {
		return nil, ErrNoJSONSuffix
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	hook, err := read(content)
	if err != nil {
		return nil, fmt.Errorf("parsing hook %q: %w", path, err)
	}
	err = hook.Validate(extensionStages)
	return hook, err
}

func read(content []byte) (hook *current.Hook, err error) {
	var ver version
	if err := json.Unmarshal(content, &ver); err != nil {
		return nil, fmt.Errorf("version check: %w", err)
	}
	reader, ok := Readers[ver.Version]
	if !ok {
		return nil, fmt.Errorf("unrecognized hook version: %q", ver.Version)
	}

	hook, err = reader(content)
	if err != nil {
		return hook, fmt.Errorf("%s: %v", ver.Version, err)
	}
	return hook, err
}

// ReadDir reads hook JSON files from a directory into the given map,
// clobbering any previous entries with the same filenames.
func ReadDir(path string, extensionStages []string, hooks map[string]*current.Hook) error {
	logrus.Debugf("reading hooks from %s", path)
	files, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	res := err
	for _, file := range files {
		filePath := filepath.Join(path, file.Name())
		hook, err := Read(filePath, extensionStages)
		if err != nil {
			if err == ErrNoJSONSuffix {
				continue
			}
			if errors.Is(err, os.ErrNotExist) {
				if err2, ok := err.(*os.PathError); ok && err2.Path == filePath {
					continue
				}
			}
			if res == nil {
				res = err
			} else {
				res = fmt.Errorf("%v: %w", err, res)
			}
			continue
		}
		hooks[file.Name()] = hook
		logrus.Debugf("added hook %s", filePath)
	}
	return res
}

func init() {
	Readers[current.Version] = current.Read
	Readers[old.Version] = old.Read
	Readers[""] = old.Read
}
