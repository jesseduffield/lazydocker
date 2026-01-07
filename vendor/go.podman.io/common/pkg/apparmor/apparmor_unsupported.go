//go:build !linux || !apparmor

package apparmor

// IsEnabled dummy.
func IsEnabled() bool {
	return false
}

// InstallDefault dummy.
func InstallDefault(name string) error {
	return ErrApparmorUnsupported
}

// IsLoaded dummy.
func IsLoaded(name string) (bool, error) {
	return false, ErrApparmorUnsupported
}

// CheckProfileAndLoadDefault dummy.
func CheckProfileAndLoadDefault(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	return "", ErrApparmorUnsupported
}

// DefaultContent dummy.
func DefaultContent(name string) ([]byte, error) {
	return nil, nil
}
