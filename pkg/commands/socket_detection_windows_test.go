//go:build windows

package commands

import (
	"context"
	"errors"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestDetectPlatformCandidates_Windows_DockerSuccess(t *testing.T) {
	oldValidate := validateSocketFunc
	defer func() { validateSocketFunc = oldValidate }()

	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		if host == "npipe:////./pipe/docker_engine" {
			return nil
		}
		return errors.New("mock failure")
	}

	log := logrus.NewEntry(logrus.New())
	host, runtime, err := detectPlatformCandidates(log)
	assert.NoError(t, err)
	assert.Equal(t, "npipe:////./pipe/docker_engine", host)
	assert.Equal(t, RuntimeDocker, runtime)
}

func TestDetectPlatformCandidates_Windows_PodmanSuccess(t *testing.T) {
	oldValidate := validateSocketFunc
	defer func() { validateSocketFunc = oldValidate }()

	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		if host == "npipe:////./pipe/podman-machine-default" {
			return nil
		}
		return errors.New("mock failure")
	}

	log := logrus.NewEntry(logrus.New())
	host, runtime, err := detectPlatformCandidates(log)
	assert.NoError(t, err)
	assert.Equal(t, "npipe:////./pipe/podman-machine-default", host)
	assert.Equal(t, RuntimePodman, runtime)
}

func TestDetectPlatformCandidates_Windows_Failure(t *testing.T) {
	oldValidate := validateSocketFunc
	defer func() { validateSocketFunc = oldValidate }()

	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		return errors.New("mock failure")
	}

	log := logrus.NewEntry(logrus.New())
	_, _, err := detectPlatformCandidates(log)
	assert.Error(t, err)
	assert.Equal(t, ErrNoDockerSocket, err)
}
