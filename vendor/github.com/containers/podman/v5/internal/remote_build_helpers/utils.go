package remote_build_helpers

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/sirupsen/logrus"
)

// TempFileManager manages temporary files created during image build.
// It maintains a list of created temporary files and provides cleanup functionality
// to ensure proper resource management.
type TempFileManager struct {
	files []string
}

func NewTempFileManager() *TempFileManager {
	return &TempFileManager{}
}

func (t *TempFileManager) AddFile(filename string) {
	t.files = append(t.files, filename)
}

func (t *TempFileManager) Cleanup() {
	for _, file := range t.files {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			logrus.Errorf("Failed to remove temp file %s: %v", file, err)
		}
	}
	t.files = t.files[:0] // Reset slice
}

// CreateTempFileFromReader creates a temporary file in the specified destination directory
// with the given pattern, and copies content from the provided reader into the file.
// The created temporary file is automatically added to the manager's cleanup list.
//
// Parameters:
//   - dest: The directory where the temporary file should be created
//   - pattern: The pattern for naming the temporary file
//   - reader: The io.Reader from which to read content to write into the temporary file
//
// Returns:
//   - string: The path to the created temporary file
//   - error: Any error encountered during the operation
func (t *TempFileManager) CreateTempFileFromReader(dest string, pattern string, reader io.Reader) (string, error) {
	tmpFile, err := os.CreateTemp(dest, pattern)
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	defer tmpFile.Close()

	t.AddFile(tmpFile.Name())

	if _, err := io.Copy(tmpFile, reader); err != nil {
		return "", fmt.Errorf("copying stdin content: %w", err)
	}
	return tmpFile.Name(), nil
}

// CreateTempSecret creates a temporary copy of a secret file in the specified
// context directory. The original secret file is copied to a new temporary file
// which is automatically added to the manager's cleanup list.
//
// Parameters:
//   - secretPath: The path to the source secret file to copy
//   - contextDir: The directory where the temporary secret file should be created
//
// Returns:
//   - string: The path to the created temporary secret file
//   - error: Any error encountered during the operation
func (t *TempFileManager) CreateTempSecret(secretPath, contextDir string) (string, error) {
	secretFile, err := os.Open(secretPath)
	if err != nil {
		return "", fmt.Errorf("opening secret file %s: %w", secretPath, err)
	}
	defer secretFile.Close()

	return t.CreateTempFileFromReader(contextDir, "podman-build-secret-*", secretFile)
}
