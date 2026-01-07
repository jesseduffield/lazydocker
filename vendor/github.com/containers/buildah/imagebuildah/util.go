package imagebuildah

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containers/buildah"
	digest "github.com/opencontainers/go-digest"
)

type mountInfo struct {
	Type   string
	Source string
	From   string
}

// Consumes mount flag in format of `--mount=type=bind,src=/path,from=image` and
// return mountInfo with values, otherwise values are empty if keys are not present in the option.
func getFromAndSourceKeysFromMountFlag(mount string) mountInfo {
	tokens := strings.Split(strings.TrimPrefix(mount, "--mount="), ",")
	source := ""
	from := ""
	mountType := ""
	for _, option := range tokens {
		if optionSplit := strings.Split(option, "="); len(optionSplit) == 2 {
			if optionSplit[0] == "src" || optionSplit[0] == "source" {
				source = optionSplit[1]
			}
			if optionSplit[0] == "from" {
				from = optionSplit[1]
			}
			if optionSplit[0] == "type" {
				mountType = optionSplit[1]
			}
		}
	}
	return mountInfo{Source: source, From: from, Type: mountType}
}

// generatePathChecksum generates the SHA-256 checksum for a file or a directory.
func generatePathChecksum(sourcePath string) (string, error) {
	digester := digest.SHA256.Digester()
	tarWriter := tar.NewWriter(digester.Hash())

	err := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		var linkTarget string
		if info.Mode()&os.ModeSymlink != 0 {
			// If the file is a symlink, get the target
			linkTarget, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}

		header, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)

		// Zero out timestamp fields to ignore modification time in checksum calculation
		header.ModTime = time.Time{}
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(tarWriter, file)
		return err
	})
	tarWriter.Close()
	if err != nil {
		return "", err
	}
	return digester.Digest().String(), nil
}

// InitReexec is a wrapper for buildah.InitReexec().  It should be called at
// the start of main(), and if it returns true, main() should return
// successfully immediately.
func InitReexec() bool {
	return buildah.InitReexec()
}

// argsMapToSlice returns the contents of a map[string]string as a slice of keys
// and values joined with "=".
func argsMapToSlice(m map[string]string) []string {
	s := make([]string, 0, len(m))
	for k, v := range m {
		s = append(s, k+"="+v)
	}
	return s
}
