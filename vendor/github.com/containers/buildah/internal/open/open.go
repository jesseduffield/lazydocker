package open

import (
	"errors"
	"fmt"
	"syscall"
)

// InChroot opens the file at `path` after chrooting to `root` and then
// changing its working directory to `wd`.  Both `wd` and `path` are evaluated
// in the chroot.
// Returns a file handle, an Errno value if there was an error and the
// underlying error was a standard library error code, and a non-empty error if
// one was detected.
func InChroot(root, wd, path string, mode int, perm uint32) (fd int, errno syscall.Errno, err error) {
	requests := requests{
		Root: root,
		Wd:   wd,
		Open: []request{
			{
				Path:  path,
				Mode:  mode,
				Perms: perm,
			},
		},
	}
	results := inChroot(requests)
	if len(results.Open) != 1 {
		return -1, 0, fmt.Errorf("got %d results back instead of 1", len(results.Open))
	}
	if results.Open[0].Err != "" {
		if results.Open[0].Errno != 0 {
			err = fmt.Errorf("%s: %w", results.Open[0].Err, results.Open[0].Errno)
		} else {
			err = errors.New(results.Open[0].Err)
		}
	}
	return int(results.Open[0].Fd), results.Open[0].Errno, err
}
