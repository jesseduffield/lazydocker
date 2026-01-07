//go:build !windows

package commands

import (
	"fmt"
	"os"
)

// detectSocketPath determines the Podman socket path to use.
// It checks in the following order:
// 1. CONTAINER_HOST environment variable (Podman standard)
// 2. Rootless socket: /run/user/{uid}/podman/podman.sock
// 3. Rootful socket: /run/podman/podman.sock
// 4. Legacy Docker socket: /var/run/docker.sock (for compatibility)
func detectSocketPath() string {
	// 1. Check CONTAINER_HOST environment variable (Podman standard)
	if host := os.Getenv("CONTAINER_HOST"); host != "" {
		return host
	}

	// Also check DOCKER_HOST for backward compatibility
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		return host
	}

	// 2. Check for rootless socket
	uid := os.Getuid()
	rootlessPath := fmt.Sprintf("/run/user/%d/podman/podman.sock", uid)
	if _, err := os.Stat(rootlessPath); err == nil {
		return "unix://" + rootlessPath
	}

	// 3. Check for rootful socket
	rootfulPath := "/run/podman/podman.sock"
	if _, err := os.Stat(rootfulPath); err == nil {
		return "unix://" + rootfulPath
	}

	// 4. Legacy Docker socket path (for compatibility)
	dockerPath := "/var/run/docker.sock"
	if _, err := os.Stat(dockerPath); err == nil {
		return "unix://" + dockerPath
	}

	// Return empty string - will fall back to libpod if available
	return ""
}
