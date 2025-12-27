//go:build !windows

package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	DockerSocketSchema = "unix://"
	DockerSocketPath   = "/var/run/docker.sock"

	defaultDockerHost = DockerSocketSchema + DockerSocketPath
)

var (
	ErrNoDockerSocket = errors.New("no working Docker/Podman socket found")
)

// SocketCandidate represents a potential socket to try
type SocketCandidate struct {
	Path    string
	Runtime ContainerRuntime
}

// getSocketCandidates returns all possible socket paths in priority order
func getSocketCandidates() []SocketCandidate {
	var candidates []SocketCandidate

	// Helper to add candidate if path is valid
	addCandidate := func(path string, runtime ContainerRuntime) {
		if path != "" {
			candidates = append(candidates, SocketCandidate{
				Path:    DockerSocketSchema + path,
				Runtime: runtime,
			})
		}
	}

	// 1. Standard Docker daemon socket
	addCandidate(DockerSocketPath, RuntimeDocker)

	xdgRuntime := os.Getenv("XDG_RUNTIME_DIR")
	home, _ := os.UserHomeDir()
	uid := os.Getuid()

	// 2. Rootless Docker: $XDG_RUNTIME_DIR/docker.sock
	if xdgRuntime != "" {
		addCandidate(filepath.Join(xdgRuntime, "docker.sock"), RuntimeDocker)
	}

	// 3. Rootless Docker: ~/.docker/run/docker.sock
	if home != "" {
		addCandidate(filepath.Join(home, ".docker", "run", "docker.sock"), RuntimeDocker)
		// 4. Docker Desktop: ~/.docker/desktop/docker.sock
		addCandidate(filepath.Join(home, ".docker", "desktop", "docker.sock"), RuntimeDocker)
	}

	// 5. Rootless Docker: /run/user/$UID/docker.sock
	addCandidate(filepath.Join("/run", "user", strconv.Itoa(uid), "docker.sock"), RuntimeDocker)

	// 6. Colima
	if home != "" {
		addCandidate(filepath.Join(home, ".colima", "default", "docker.sock"), RuntimeDocker)
		addCandidate(filepath.Join(home, ".colima", "docker.sock"), RuntimeDocker)
	}

	// 7. OrbStack
	if home != "" {
		addCandidate(filepath.Join(home, ".orbstack", "run", "docker.sock"), RuntimeDocker)
	}

	// 8. Lima
	if home != "" {
		addCandidate(filepath.Join(home, ".lima", "default", "sock", "docker.sock"), RuntimeDocker)
	}

	// 9. Rancher Desktop
	if home != "" {
		addCandidate(filepath.Join(home, ".rd", "docker.sock"), RuntimeDocker)
	}

	// 10. Snap Docker
	addCandidate("/var/snap/docker/current/run/docker.sock", RuntimeDocker)

	// 11. Rootless Podman: $XDG_RUNTIME_DIR/podman/podman.sock
	if xdgRuntime != "" {
		addCandidate(filepath.Join(xdgRuntime, "podman", "podman.sock"), RuntimePodman)
	}

	// 12. Rootless Podman: /run/user/$UID/podman/podman.sock
	addCandidate(filepath.Join("/run", "user", strconv.Itoa(uid), "podman", "podman.sock"), RuntimePodman)

	// 13. Rootless Podman: ~/.local/share/containers/podman/podman.sock
	if home != "" {
		addCandidate(filepath.Join(home, ".local", "share", "containers", "podman", "podman.sock"), RuntimePodman)
	}

	// 14. Rootful Podman: /run/podman/podman.sock
	addCandidate("/run/podman/podman.sock", RuntimePodman)

	return candidates
}

func detectPlatformCandidates(log *logrus.Entry) (string, ContainerRuntime, error) {
	var lastErr error
	candidates := getSocketCandidates()

	for _, candidate := range candidates {
		socketPath := strings.TrimPrefix(candidate.Path, DockerSocketSchema)

		// Fast path: check if socket file exists
		if _, err := os.Stat(socketPath); err != nil {
			continue
		}

		// Validate by actually connecting
		ctx, cancel := context.WithTimeout(context.Background(), socketValidationTimeout)
		err := validateSocket(ctx, candidate.Path, false)
		cancel()

		if err != nil {
			log.Debugf("Socket %s exists but validation failed: %v", candidate.Path, err)
			if strings.Contains(err.Error(), "permission denied") {
				lastErr = fmt.Errorf("%s: permission denied (are you in the docker group?)", candidate.Path)
			} else {
				lastErr = fmt.Errorf("%s: %w", candidate.Path, err)
			}
			continue
		}

		log.Infof("Connected to %s runtime via %s", candidate.Runtime, candidate.Path)
		return candidate.Path, candidate.Runtime, nil
	}

	// All candidates failed - provide actionable error
	if lastErr != nil {
		return "", RuntimeUnknown, fmt.Errorf("%w: last error: %v", ErrNoDockerSocket, lastErr)
	}

	msg := fmt.Sprintf("%v: ensure Docker or Podman is running", ErrNoDockerSocket)
	return "", RuntimeUnknown, errors.New(msg)
}
