//go:build windows

package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	DockerSocketSchema = "npipe://"
	DockerSocketPath   = "//./pipe/docker_engine"

	defaultDockerHost = DockerSocketSchema + DockerSocketPath
)

func getPodmanPipes(log *logrus.Entry) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Debugf("Failed to get user home directory: %v", err)
		return []string{"npipe:////./pipe/podman-machine-default"}
	}

	configDir := filepath.Join(home, ".config", "containers", "podman", "machine", "wsl")
	files, err := os.ReadDir(configDir)
	if err != nil {
		log.Debugf("Failed to read Podman machine config directory %s: %v", configDir, err)
		return []string{"npipe:////./pipe/podman-machine-default"}
	}

	var pipes []string
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".json" {
			name := strings.TrimSuffix(f.Name(), ".json")
			pipes = append(pipes, "npipe:////./pipe/"+name)
		}
	}

	if len(pipes) == 0 {
		log.Debug("No Podman machine config files found, falling back to default")
		return []string{"npipe:////./pipe/podman-machine-default"}
	}
	return pipes
}

func detectPlatformCandidates(log *logrus.Entry) (string, ContainerRuntime, error) {
	// Try Docker Desktop first
	dockerHost := defaultDockerHost
	err := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), socketValidationTimeout)
		defer cancel()
		return validateSocketFunc(ctx, dockerHost, false)
	}()

	if err == nil {
		return dockerHost, RuntimeDocker, nil
	}

	// Try Podman machines on Windows
	for _, podmanHost := range getPodmanPipes(log) {
		err = func() error {
			ctx, cancel := context.WithTimeout(context.Background(), socketValidationTimeout)
			defer cancel()
			return validateSocketFunc(ctx, podmanHost, false)
		}()

		if err == nil {
			return podmanHost, RuntimePodman, nil
		}
	}

	return "", RuntimeUnknown, ErrNoDockerSocket
}
