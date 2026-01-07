package chown

import (
	"os"
	"os/user"
	"path/filepath"

	"go.podman.io/storage/pkg/homedir"
)

// DangerousHostPath validates if a host path is dangerous and should not be modified.
func DangerousHostPath(path string) (bool, error) {
	excludePaths := map[string]bool{
		"/":           true,
		"/bin":        true,
		"/boot":       true,
		"/dev":        true,
		"/etc":        true,
		"/etc/passwd": true,
		"/etc/pki":    true,
		"/etc/shadow": true,
		"/home":       true,
		"/lib":        true,
		"/lib64":      true,
		"/media":      true,
		"/opt":        true,
		"/proc":       true,
		"/root":       true,
		"/run":        true,
		"/sbin":       true,
		"/srv":        true,
		"/sys":        true,
		"/tmp":        true,
		"/usr":        true,
		"/var":        true,
		"/var/lib":    true,
		"/var/log":    true,
	}

	if home := homedir.Get(); home != "" {
		excludePaths[home] = true
	}

	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if usr, err := user.Lookup(sudoUser); err == nil {
			excludePaths[usr.HomeDir] = true
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return true, err
	}

	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return true, err
	}

	if excludePaths[realPath] {
		return true, nil
	}

	return false, nil
}
