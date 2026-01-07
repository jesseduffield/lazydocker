package explicitfilepath

import (
	"fmt"
	"os"
	"path/filepath"

	"go.podman.io/storage/pkg/fileutils"
)

// ResolvePathToFullyExplicit returns the input path converted to an absolute, no-symlinks, cleaned up path.
// To do so, all elements of the input path must exist; as a special case, the final component may be
// a non-existent name (but not a symlink pointing to a non-existent name)
// This is intended as a helper for implementations of types.ImageReference.PolicyConfigurationIdentity etc.
func ResolvePathToFullyExplicit(path string) (string, error) {
	switch err := fileutils.Lexists(path); {
	case err == nil:
		return resolveExistingPathToFullyExplicit(path)
	case os.IsNotExist(err):
		parent, file := filepath.Split(path)
		resolvedParent, err := resolveExistingPathToFullyExplicit(parent)
		if err != nil {
			return "", err
		}
		if file == "." || file == ".." {
			// Coverage: This can happen, but very rarely: if we have successfully resolved the parent, both "." and ".." in it should have been resolved as well.
			// This can still happen if there is a filesystem race condition, causing the Lstat() above to fail but the later resolution to succeed.
			// We do not care to promise anything if such filesystem race conditions can happen, but we definitely don't want to return "."/".." components
			// in the resulting path, and especially not at the end.
			return "", fmt.Errorf("Unexpectedly missing special filename component in %s", path)
		}
		resolvedPath := filepath.Join(resolvedParent, file)
		// As a sanity check, ensure that there are no "." or ".." components.
		cleanedResolvedPath := filepath.Clean(resolvedPath)
		if cleanedResolvedPath != resolvedPath {
			// Coverage: This should never happen.
			return "", fmt.Errorf("Internal inconsistency: Path %s resolved to %s still cleaned up to %s", path, resolvedPath, cleanedResolvedPath)
		}
		return resolvedPath, nil
	default: // err != nil, unrecognized
		return "", err
	}
}

// resolveExistingPathToFullyExplicit is the same as ResolvePathToFullyExplicit,
// but without the special case for missing final component.
func resolveExistingPathToFullyExplicit(path string) (string, error) {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", err // Coverage: This can fail only if os.Getwd() fails.
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}
