package system

import (
	"strings"

	"golang.org/x/sys/unix"
)

const (
	// Value is larger than the maximum size allowed
	E2BIG unix.Errno = unix.E2BIG

	// Operation not supported
	ENOTSUP unix.Errno = unix.ENOTSUP

	// Value is too small or too large for maximum size allowed
	EOVERFLOW unix.Errno = unix.EOVERFLOW
)

var namespaceMap = map[string]int{
	"user":   EXTATTR_NAMESPACE_USER,
	"system": EXTATTR_NAMESPACE_SYSTEM,
}

func xattrToExtattr(xattr string) (namespace int, extattr string, err error) {
	namespaceName, extattr, found := strings.Cut(xattr, ".")
	if !found {
		return -1, "", ENOTSUP
	}

	namespace, ok := namespaceMap[namespaceName]
	if !ok {
		return -1, "", ENOTSUP
	}
	return namespace, extattr, nil
}

// Lgetxattr retrieves the value of the extended attribute identified by attr
// and associated with the given path in the file system.
// Returns a []byte slice if the xattr is set and nil otherwise.
func Lgetxattr(path string, attr string) ([]byte, error) {
	namespace, extattr, err := xattrToExtattr(attr)
	if err != nil {
		return nil, err
	}
	return ExtattrGetLink(path, namespace, extattr)
}

// Lsetxattr sets the value of the extended attribute identified by attr
// and associated with the given path in the file system.
func Lsetxattr(path string, attr string, value []byte, flags int) error {
	if flags != 0 {
		// FIXME: Flags are not supported on FreeBSD, but we can implement
		// them mimicking the behavior of the Linux implementation.
		// See lsetxattr(2) on Linux for more information.
		return ENOTSUP
	}

	namespace, extattr, err := xattrToExtattr(attr)
	if err != nil {
		return err
	}
	return ExtattrSetLink(path, namespace, extattr, value)
}

// Llistxattr lists extended attributes associated with the given path
// in the file system.
func Llistxattr(path string) ([]string, error) {
	attrs := []string{}

	for namespaceName, namespace := range namespaceMap {
		namespaceAttrs, err := ExtattrListLink(path, namespace)
		if err != nil {
			return nil, err
		}

		for _, attr := range namespaceAttrs {
			attrs = append(attrs, namespaceName+"."+attr)
		}
	}

	return attrs, nil
}
