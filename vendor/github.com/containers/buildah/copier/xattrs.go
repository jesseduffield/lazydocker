//go:build linux || netbsd || freebsd || darwin

package copier

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/unshare"
	"golang.org/x/sys/unix"
)

const (
	xattrsSupported = true
	imaXattr        = "security.ima"
)

var (
	relevantAttributes    = []string{"security.capability", imaXattr, "user.*"} // the attributes that we preserve - we discard others
	irrelevantAttributes  = []string{"user.overlay.*"}                          // the attributes that we discard, even from the relevantAttributes list
	initialXattrListSize  = 64 * 1024
	initialXattrValueSize = 64 * 1024
)

// isRelevantXattr checks if "attribute" matches one of the attribute patterns
// listed in the "relevantAttributes" list.
func isRelevantXattr(attribute string) bool {
	for _, relevant := range relevantAttributes {
		matched, err := filepath.Match(relevant, attribute)
		if err != nil || !matched {
			continue
		}
		for _, irrelevant := range irrelevantAttributes {
			matched, err := filepath.Match(irrelevant, attribute)
			if err != nil || !matched {
				continue
			}
			return false
		}
		return true
	}
	return false
}

// Lgetxattrs returns a map of the relevant extended attributes set on the given file.
func Lgetxattrs(path string) (map[string]string, error) {
	maxSize := 64 * 1024 * 1024
	listSize := initialXattrListSize
	var list []byte
	for listSize < maxSize {
		list = make([]byte, listSize)
		size, err := unix.Llistxattr(path, list)
		if err != nil {
			if errors.Is(err, syscall.ERANGE) {
				listSize *= 2
				continue
			}
			if errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.ENOSYS) {
				// treat these errors listing xattrs as equivalent to "no xattrs"
				list = list[:0]
				break
			}
			return nil, fmt.Errorf("listing extended attributes of %q: %w", path, err)
		}
		list = list[:size]
		break
	}
	if listSize >= maxSize {
		return nil, fmt.Errorf("unable to read list of attributes for %q: size would have been too big", path)
	}
	m := make(map[string]string)
	for attribute := range strings.SplitSeq(string(list), string('\000')) {
		if isRelevantXattr(attribute) {
			attributeSize := initialXattrValueSize
			var attributeValue []byte
			for attributeSize < maxSize {
				attributeValue = make([]byte, attributeSize)
				size, err := unix.Lgetxattr(path, attribute, attributeValue)
				if err != nil {
					if errors.Is(err, syscall.ERANGE) {
						attributeSize *= 2
						continue
					}
					return nil, fmt.Errorf("getting value of extended attribute %q on %q: %w", attribute, path, err)
				}
				m[attribute] = string(attributeValue[:size])
				break
			}
			if attributeSize >= maxSize {
				return nil, fmt.Errorf("unable to read attribute %q of %q: size would have been too big", attribute, path)
			}
		}
	}
	return m, nil
}

// Lsetxattrs sets the relevant members of the specified extended attributes on the given file.
func Lsetxattrs(path string, xattrs map[string]string) error {
	for attribute, value := range xattrs {
		if isRelevantXattr(attribute) {
			if err := unix.Lsetxattr(path, attribute, []byte(value), 0); err != nil {
				if unshare.IsRootless() && attribute == imaXattr {
					logrus.Warnf("Unable to set %q xattr on %q: %v", attribute, path, err)
				} else {
					return fmt.Errorf("setting value of extended attribute %q on %q: %w", attribute, path, err)
				}
			}
		}
	}
	return nil
}
