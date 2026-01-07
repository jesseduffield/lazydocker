//go:build !freebsd

package system

const (
	EXTATTR_NAMESPACE_EMPTY  = 0
	EXTATTR_NAMESPACE_USER   = 0
	EXTATTR_NAMESPACE_SYSTEM = 0
)

// ExtattrGetLink is not supported on platforms other than FreeBSD.
func ExtattrGetLink(path string, attrnamespace int, attrname string) ([]byte, error) {
	return nil, ErrNotSupportedPlatform
}

// ExtattrSetLink is not supported on platforms other than FreeBSD.
func ExtattrSetLink(path string, attrnamespace int, attrname string, data []byte) error {
	return ErrNotSupportedPlatform
}

// ExtattrListLink is not supported on platforms other than FreeBSD.
func ExtattrListLink(path string, attrnamespace int) ([]string, error) {
	return nil, ErrNotSupportedPlatform
}
