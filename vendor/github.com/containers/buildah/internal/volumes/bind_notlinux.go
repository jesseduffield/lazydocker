//go:build !linux

package volumes

import "errors"

// bindFromChroot would open "path" inside of "root" using a chrooted
// subprocess that returns a descriptor, then would create a uniquely-named
// temporary directory or file under "tmp" and bind-mount the opened descriptor
// to it, returning the path of the temporary file or directory.  The caller
// would be responsible for unmounting and removing the temporary.  For now,
// this just returns an error because it is not implemented for this platform.
func bindFromChroot(root, path, tmp string) (string, error) {
	return "", errors.New("not available on this system")
}
