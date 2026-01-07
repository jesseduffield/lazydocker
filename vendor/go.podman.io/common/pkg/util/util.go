package util

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/fileutils"
)

// StringMatchRegexSlice determines if a given string matches one of the given regexes, returns bool.
func StringMatchRegexSlice(s string, re []string) bool {
	for _, r := range re {
		m, err := regexp.MatchString(r, s)
		if err == nil && m {
			return true
		}
	}
	return false
}

// WaitForFile waits until a file has been created or the given timeout has occurred.
func WaitForFile(path string, chWait chan error, timeout time.Duration) (bool, error) {
	var inotifyEvents chan fsnotify.Event
	watcher, err := fsnotify.NewWatcher()
	if err == nil {
		if err := watcher.Add(filepath.Dir(path)); err == nil {
			inotifyEvents = watcher.Events
		}
		defer func() {
			if err := watcher.Close(); err != nil {
				logrus.Errorf("Failed to close fsnotify watcher: %v", err)
			}
		}()
	}

	var timeoutChan <-chan time.Time

	if timeout != 0 {
		timeoutChan = time.After(timeout)
	}

	for {
		select {
		case e := <-chWait:
			return true, e
		case <-inotifyEvents:
			err := fileutils.Exists(path)
			if err == nil {
				return false, nil
			}
			if !os.IsNotExist(err) {
				return false, err
			}
		case <-time.After(25 * time.Millisecond):
			// Check periodically for the file existence.  It is needed
			// if the inotify watcher could not have been created.  It is
			// also useful when using inotify as if for any reasons we missed
			// a notification, we won't hang the process.
			err := fileutils.Exists(path)
			if err == nil {
				return false, nil
			}
			if !os.IsNotExist(err) {
				return false, err
			}
		case <-timeoutChan:
			return false, fmt.Errorf("timed out waiting for file %s", path)
		}
	}
}
