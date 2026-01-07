package specgen

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

func IsHostWinPath(path string) bool {
	return shouldResolveWinPaths() && strings.HasPrefix(path, `\\`) || hasWinDriveScheme(path, 0) || winPathExists(path)
}

func hasWinDriveScheme(path string, start int) bool {
	if len(path) < start+2 || path[start+1] != ':' {
		return false
	}

	drive := rune(path[start])
	return drive < unicode.MaxASCII && unicode.IsLetter(drive)
}

// Converts a Windows path to a WSL guest path if local env is a WSL linux guest or this is a Windows client.
func ConvertWinMountPath(path string) (string, error) {
	if !shouldResolveWinPaths() {
		return path, nil
	}

	if strings.HasPrefix(path, "/") {
		// Handle /[driveletter]/windows/path form (e.g. c:\Users\bar == /c/Users/bar)
		if len(path) > 2 && path[2] == '/' && shouldResolveUnixWinVariant(path) {
			drive := unicode.ToLower(rune(path[1]))
			if unicode.IsLetter(drive) && drive <= unicode.MaxASCII {
				return fmt.Sprintf("/mnt/%c/%s", drive, path[3:]), nil
			}
		}

		// unix path - pass through
		return path, nil
	}

	// Convert remote win client relative paths to absolute
	path = resolveRelativeOnWindows(path)

	// Strip extended marker prefix if present
	path = strings.TrimPrefix(path, `\\?\`)

	// Drive installed via wsl --mount
	switch {
	case strings.HasPrefix(path, `\\.\`):
		path = "/mnt/wsl/" + path[4:]
	case len(path) > 1 && path[1] == ':':
		path = "/mnt/" + strings.ToLower(path[0:1]) + path[2:]
	default:
		return path, errors.New("unsupported UNC path")
	}

	return strings.ReplaceAll(path, `\`, "/"), nil
}
