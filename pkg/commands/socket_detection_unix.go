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

var (
	statFunc        = os.Stat
	getuidFunc      = os.Getuid
	getenvFunc      = os.Getenv
	userHomeDirFunc = os.UserHomeDir
)

const (
	DockerSocketSchema = "unix://"
	DockerSocketPath   = "/var/run/docker.sock"

	defaultDockerHost = DockerSocketSchema + DockerSocketPath
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

	xdgRuntime := getenvFunc("XDG_RUNTIME_DIR")
	home, _ := userHomeDirFunc()
	uid := getuidFunc()

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

	// 15. Podman on macOS (default machine)
	if home != "" {
		addCandidate(filepath.Join(home, ".local", "share", "containers", "podman", "machine", "podman-machine-default", "podman.sock"), RuntimePodman)
		addCandidate(filepath.Join(home, ".local", "share", "containers", "podman", "machine", "qemu", "podman.sock"), RuntimePodman)
	}

	return candidates
}

func detectPlatformCandidates(log *logrus.Entry) (string, ContainerRuntime, error) {
	var lastErr error
	candidates := getSocketCandidates()

	for _, candidate := range candidates {
		socketPath := strings.TrimPrefix(candidate.Path, DockerSocketSchema)

		// Fast path: check if socket file exists
		if _, err := statFunc(socketPath); err != nil {
			continue
		}

		// Validate by actually connecting
		err := func() error {
			ctx, cancel := context.WithTimeout(context.Background(), socketValidationTimeout)
			defer cancel()
			return validateSocketFunc(ctx, candidate.Path, false)
		}()

		if err != nil {
			log.Debugf("Socket %s exists but validation failed: %v", candidate.Path, err)
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "permission denied") || strings.Contains(errStr, "eacces") {
				lastErr = fmt.Errorf("%s: permission denied (check your user permissions for this socket)", candidate.Path)
			} else {
				lastErr = fmt.Errorf("%s: %v", candidate.Path, err)
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
