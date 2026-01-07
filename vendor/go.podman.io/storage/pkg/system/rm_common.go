//go:build !freebsd

package system

// Reset file flags in a directory tree. This allows EnsureRemoveAll
// to delete trees which have the immutable flag set.
func resetFileFlags(dir string) error {
	return nil
}
