package copy

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ParseSourceAndDestination parses the source and destination input into a
// possibly specified container and path.  The input format is described in
// podman-cp(1) as "[nameOrID:]path".  Colons in paths are supported as long
// they start with a dot or slash.
//
// It returns, in order, the source container and path, followed by the
// destination container and path, and an error.  Note that exactly one
// container must be specified.
func ParseSourceAndDestination(source, destination string) (string, string, string, string, error) {
	sourceContainer, sourcePath := parseUserInput(source)
	destContainer, destPath := parseUserInput(destination)

	if len(sourcePath) == 0 || len(destPath) == 0 {
		return "", "", "", "", fmt.Errorf("invalid arguments %q, %q: you must specify paths", source, destination)
	}

	return sourceContainer, sourcePath, destContainer, destPath, nil
}

// parseUserInput parses the input string and returns, if specified, the name
// or ID of the container and the path.  The input format is described in
// podman-cp(1) as "[nameOrID:]path".  Colons in paths are supported as long
// they start with a dot or slash.
func parseUserInput(input string) (container string, path string) {
	if len(input) == 0 {
		return
	}
	path = input

	// If the input starts with a dot or slash, it cannot refer to a
	// container.
	if input[0] == '.' || input[0] == '/' {
		return
	}

	// If the input is an absolute path, it cannot refer to a container.
	// This is necessary because absolute paths on Windows will include
	// a colon, which would cause the drive letter to be parsed as a
	// container name.
	if filepath.IsAbs(input) {
		return
	}

	if parsedContainer, parsedPath, ok := strings.Cut(path, ":"); ok {
		container = parsedContainer
		path = parsedPath
	}
	return
}
