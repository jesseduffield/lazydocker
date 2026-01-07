//go:build !linux && !freebsd && !solaris && !darwin

package graphdriver

// Slice of drivers that should be used in an order
var Priority = []string{
	"unsupported",
}

// GetFSMagic returns the filesystem id given the path.
func GetFSMagic(rootpath string) (FsMagic, error) {
	return FsMagicUnsupported, nil
}
