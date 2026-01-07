//go:build windows

package commands

import "os"

// detectSocketPath determines the Podman socket path to use on Windows.
// It checks in the following order:
// 1. CONTAINER_HOST environment variable (Podman standard)
// 2. Default Podman machine named pipe
func detectSocketPath() string {
	// Check CONTAINER_HOST environment variable
	if host := os.Getenv("CONTAINER_HOST"); host != "" {
		return host
	}

	// Also check DOCKER_HOST for backward compatibility
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		return host
	}

	// Default to Podman machine socket
	// Podman machine on Windows uses named pipes
	return "npipe:////./pipe/podman-machine-default"
}
