package system

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/mount"
)

// EnsureRemoveAll wraps `os.RemoveAll` to check for specific errors that can
// often be remedied.
// Only use `EnsureRemoveAll` if you really want to make every effort to remove
// a directory.
//
// Because of the way `os.Remove` (and by extension `os.RemoveAll`) works, there
// can be a race between reading directory entries and then actually attempting
// to remove everything in the directory.
// These types of errors do not need to be returned since it's ok for the dir to
// be gone we can just retry the remove operation.
//
// This should not return a `os.ErrNotExist` kind of error under any circumstances
func EnsureRemoveAll(dir string) error {
	notExistErr := make(map[string]bool)

	// track retries
	exitOnErr := make(map[string]int)
	maxRetry := 1000

	// Attempt a simple remove all first, this avoids the more expensive
	// RecursiveUnmount call if not needed.
	if err := os.RemoveAll(dir); err == nil {
		return nil
	}

	// Attempt to unmount anything beneath this dir first
	if err := mount.RecursiveUnmount(dir); err != nil {
		logrus.Debugf("RecursiveUnmount on %s failed: %v", dir, err)
	}

	for {
		err := os.RemoveAll(dir)
		if err == nil {
			return nil
		}

		// If the RemoveAll fails with a permission error, we
		// may have immutable files so try to remove the
		// immutable flag and redo the RemoveAll.
		if errors.Is(err, syscall.EPERM) {
			if err = resetFileFlags(dir); err != nil {
				return fmt.Errorf("resetting file flags: %w", err)
			}
			err = os.RemoveAll(dir)
			if err == nil {
				return nil
			}
		}

		pe, ok := err.(*os.PathError)
		if !ok {
			return err
		}

		if os.IsNotExist(err) {
			if notExistErr[pe.Path] {
				return err
			}
			notExistErr[pe.Path] = true

			// There is a race where some subdir can be removed but after the parent
			//   dir entries have been read.
			// So the path could be from `os.Remove(subdir)`
			// If the reported non-existent path is not the passed in `dir` we
			// should just retry, but otherwise return with no error.
			if pe.Path == dir {
				return nil
			}
			continue
		}

		if !IsEBUSY(pe.Err) {
			return err
		}

		if e := mount.Unmount(pe.Path); e != nil {
			return fmt.Errorf("while removing %s: %w", dir, e)
		}

		if exitOnErr[pe.Path] == maxRetry {
			return err
		}
		exitOnErr[pe.Path]++
		time.Sleep(10 * time.Millisecond)
	}
}
