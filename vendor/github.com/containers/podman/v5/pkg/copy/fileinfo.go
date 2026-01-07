package copy

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
)

// XDockerContainerPathStatHeader is the *key* in http headers pointing to the
// base64 encoded JSON payload of stating a path in a container.
const XDockerContainerPathStatHeader = "X-Docker-Container-Path-Stat"

// ErrENOENT mimics the stdlib's ErrENOENT and can be used to implement custom logic
// while preserving the user-visible error message.
var ErrENOENT = errors.New("no such file or directory")

// FileInfo describes a file or directory and is returned by
// (*CopyItem).Stat().
type FileInfo = define.FileInfo

// EncodeFileInfo serializes the specified FileInfo as a base64 encoded JSON
// payload.  Intended for Docker compat.
func EncodeFileInfo(info *FileInfo) (string, error) {
	buf, err := json.Marshal(&info)
	if err != nil {
		return "", fmt.Errorf("failed to serialize file stats: %w", err)
	}
	return base64.URLEncoding.EncodeToString(buf), nil
}

// ExtractFileInfoFromHeader extracts a base64 encoded JSON payload of a
// FileInfo in the http header.  If no such header entry is found, nil is
// returned.  Intended for Docker compat.
func ExtractFileInfoFromHeader(header *http.Header) (*FileInfo, error) {
	rawData := header.Get(XDockerContainerPathStatHeader)
	if len(rawData) == 0 {
		return nil, nil
	}

	info := FileInfo{}
	base64Decoder := base64.NewDecoder(base64.URLEncoding, strings.NewReader(rawData))
	if err := json.NewDecoder(base64Decoder).Decode(&info); err != nil {
		return nil, err
	}

	return &info, nil
}

// ResolveHostPath resolves the specified, possibly relative, path on the host.
func ResolveHostPath(path string) (*FileInfo, error) {
	resolvedHostPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	resolvedHostPath = PreserveBasePath(path, resolvedHostPath)

	statInfo, err := os.Stat(resolvedHostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrENOENT
		}
		return nil, err
	}

	return &FileInfo{
		Name:       statInfo.Name(),
		Size:       statInfo.Size(),
		Mode:       statInfo.Mode(),
		ModTime:    statInfo.ModTime(),
		IsDir:      statInfo.IsDir(),
		LinkTarget: resolvedHostPath,
	}, nil
}

// PreserveBasePath makes sure that the original base path (e.g., "/" or "./")
// is preserved.  The filepath API among tends to clean up a bit too much but
// we *must* preserve this data by all means.
func PreserveBasePath(original, resolved string) string {
	// Ensure paths are in platform semantics (replace / with \ on Windows)
	resolved = filepath.FromSlash(resolved)
	original = filepath.FromSlash(original)

	if filepath.Base(resolved) != "." && filepath.Base(original) == "." {
		if !hasTrailingPathSeparator(resolved) {
			// Add a separator if it doesn't already end with one (a cleaned
			// path would only end in a separator if it is the root).
			resolved += string(filepath.Separator)
		}
		resolved += "."
	}

	if !hasTrailingPathSeparator(resolved) && hasTrailingPathSeparator(original) {
		resolved += string(filepath.Separator)
	}

	return resolved
}

// hasTrailingPathSeparator returns whether the given
// path ends with the system's path separator character.
func hasTrailingPathSeparator(path string) bool {
	return len(path) > 0 && os.IsPathSeparator(path[len(path)-1])
}
