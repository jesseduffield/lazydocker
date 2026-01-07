//go:build !linux && !netbsd && !freebsd && !darwin

package copier

const (
	xattrsSupported = false
)

func Lgetxattrs(path string) (map[string]string, error) {
	return nil, nil
}

func Lsetxattrs(path string, xattrs map[string]string) error {
	return nil
}
