//go:build windows

package commands

import (
	"context"

	"github.com/sirupsen/logrus"
)

const (
	DockerSocketSchema = "npipe://"
	DockerSocketPath   = "//./pipe/docker_engine"

	defaultDockerHost = DockerSocketSchema + DockerSocketPath
)

func detectPlatformCandidates(log *logrus.Entry) (string, ContainerRuntime, error) {
	// Try Docker Desktop first
	dockerHost := defaultDockerHost
	ctx, cancel := context.WithTimeout(context.Background(), socketValidationTimeout)
	err := validateSocket(ctx, dockerHost, false)
	cancel()

	if err == nil {
		return dockerHost, RuntimeDocker, nil
	}

	// Try Podman on Windows
	podmanHost := "npipe:////./pipe/podman-machine-default"
	ctx, cancel = context.WithTimeout(context.Background(), socketValidationTimeout)
	err = validateSocket(ctx, podmanHost, false)
	cancel()

	if err == nil {
		return podmanHost, RuntimePodman, nil
	}

	// Fallback to default Docker host
	return dockerHost, RuntimeDocker, nil
}
