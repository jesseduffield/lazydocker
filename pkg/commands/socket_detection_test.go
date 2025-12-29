//go:build !windows

package commands

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/docker/cli/cli/config/configfile"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestGetSocketCandidates(t *testing.T) {
	// Save and restore
	oldGetuid := getuidFunc
	oldGetenv := getenvFunc
	oldUserHomeDir := userHomeDirFunc
	defer func() {
		getuidFunc = oldGetuid
		getenvFunc = oldGetenv
		userHomeDirFunc = oldUserHomeDir
	}()

	getuidFunc = func() int { return 1000 }
	getenvFunc = func(key string) string {
		if key == "XDG_RUNTIME_DIR" {
			return "/run/user/1000"
		}
		return ""
	}
	userHomeDirFunc = func() (string, error) { return "/home/user", nil }

	candidates := getSocketCandidates()

	expectedPaths := []string{
		"unix:///var/run/docker.sock",
		"unix:///run/user/1000/docker.sock",
		"unix:///home/user/.docker/run/docker.sock",
		"unix:///run/user/1000/podman/podman.sock",
		"unix:///home/user/.colima/default/docker.sock",
		"unix:///home/user/.orbstack/run/docker.sock",
		"unix:///home/user/.lima/default/sock/docker.sock",
		"unix:///home/user/.rd/docker.sock",
	}

	var paths []string
	for _, c := range candidates {
		paths = append(paths, c.Path)
	}

	for _, expected := range expectedPaths {
		assert.Contains(t, paths, expected)
	}
}

func TestDetectDockerHost_DOCKER_HOST_Priority(t *testing.T) {
	// Save env var
	oldDockerHost := os.Getenv("DOCKER_HOST")
	defer os.Setenv("DOCKER_HOST", oldDockerHost)

	expectedHost := "unix:///tmp/custom.sock"
	os.Setenv("DOCKER_HOST", expectedHost)

	// Mock validateSocketFunc to succeed
	oldValidate := validateSocketFunc
	oldInfer := inferRuntimeFromHostFunc
	defer func() { validateSocketFunc = oldValidate }()
	defer func() { inferRuntimeFromHostFunc = oldInfer }()
	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		return nil
	}
	inferRuntimeFromHostFunc = func(ctx context.Context, host string, useEnv bool) (ContainerRuntime, error) {
		return RuntimePodman, nil
	}

	// Reset cache for test
	ResetDockerHostCache()

	log := logrus.NewEntry(logrus.New())
	host, runtime, err := DetectDockerHost(log)
	assert.NoError(t, err)
	assert.Equal(t, expectedHost, host)
	assert.Equal(t, RuntimePodman, runtime)
}

func TestDetectDockerHost_Caching(t *testing.T) {
	// Save env var
	oldDockerHost := os.Getenv("DOCKER_HOST")
	defer os.Setenv("DOCKER_HOST", oldDockerHost)

	os.Setenv("DOCKER_HOST", "unix:///tmp/first.sock")

	// Mock validateSocketFunc to succeed
	oldValidate := validateSocketFunc
	oldInfer := inferRuntimeFromHostFunc
	defer func() { validateSocketFunc = oldValidate }()
	defer func() { inferRuntimeFromHostFunc = oldInfer }()
	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		return nil
	}
	inferRuntimeFromHostFunc = func(ctx context.Context, host string, useEnv bool) (ContainerRuntime, error) {
		return RuntimePodman, nil
	}

	// Reset cache for test
	ResetDockerHostCache()

	log := logrus.NewEntry(logrus.New())
	host1, runtime1, _ := DetectDockerHost(log)

	// Change env var - should still return first one from cache
	os.Setenv("DOCKER_HOST", "unix:///tmp/second.sock")
	host2, runtime2, _ := DetectDockerHost(log)

	assert.Equal(t, host1, host2)
	assert.Equal(t, "unix:///tmp/first.sock", host2)
	assert.Equal(t, runtime1, runtime2)
	assert.Equal(t, RuntimePodman, runtime2)
}

func TestDetectDockerHost_DOCKER_HOST_Invalid(t *testing.T) {
	oldDockerHost := os.Getenv("DOCKER_HOST")
	os.Setenv("DOCKER_HOST", "unix:///tmp/invalid.sock")
	defer os.Setenv("DOCKER_HOST", oldDockerHost)

	ResetDockerHostCache()

	oldValidate := validateSocketFunc
	defer func() { validateSocketFunc = oldValidate }()
	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		return errors.New("invalid")
	}

	log := logrus.NewEntry(logrus.New())
	_, _, err := DetectDockerHost(log)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "DOCKER_HOST")
	}
}

func TestDetectDockerHost_Context_Success(t *testing.T) {
	oldDockerHost := os.Getenv("DOCKER_HOST")
	os.Setenv("DOCKER_HOST", "")
	defer os.Setenv("DOCKER_HOST", oldDockerHost)

	oldGetHost := getHostFromContextFunc
	oldValidate := validateSocketFunc
	oldInfer := inferRuntimeFromHostFunc
	defer func() {
		getHostFromContextFunc = oldGetHost
		validateSocketFunc = oldValidate
		inferRuntimeFromHostFunc = oldInfer
	}()

	getHostFromContextFunc = func() (string, error) {
		return "unix:///tmp/context.sock", nil
	}
	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		if host == "unix:///tmp/context.sock" {
			return nil
		}
		return errors.New("invalid")
	}
	inferRuntimeFromHostFunc = func(ctx context.Context, host string, useEnv bool) (ContainerRuntime, error) {
		return RuntimePodman, nil
	}

	ResetDockerHostCache()

	log := logrus.NewEntry(logrus.New())
	host, runtime, err := DetectDockerHost(log)
	assert.NoError(t, err)
	assert.Equal(t, "unix:///tmp/context.sock", host)
	assert.Equal(t, RuntimePodman, runtime)
}

func TestDetectDockerHost_Context_Invalid_Fallback(t *testing.T) {
	oldDockerHost := os.Getenv("DOCKER_HOST")
	os.Setenv("DOCKER_HOST", "")
	defer os.Setenv("DOCKER_HOST", oldDockerHost)

	oldGetHost := getHostFromContextFunc
	oldValidate := validateSocketFunc
	oldDetectPlatform := detectPlatformCandidatesFunc
	defer func() {
		getHostFromContextFunc = oldGetHost
		validateSocketFunc = oldValidate
		detectPlatformCandidatesFunc = oldDetectPlatform
	}()

	getHostFromContextFunc = func() (string, error) {
		return "unix:///tmp/invalid-context.sock", nil
	}
	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		return errors.New("invalid")
	}
	detectPlatformCandidatesFunc = func(log *logrus.Entry) (string, ContainerRuntime, error) {
		return "unix:///tmp/fallback.sock", RuntimeDocker, nil
	}

	ResetDockerHostCache()

	log := logrus.NewEntry(logrus.New())
	host, _, err := DetectDockerHost(log)
	assert.NoError(t, err)
	assert.Equal(t, "unix:///tmp/fallback.sock", host)
}

func TestDetectDockerHost_Context_Invalid(t *testing.T) {
	// Save env vars
	oldDockerHost := os.Getenv("DOCKER_HOST")
	oldDockerContext := os.Getenv("DOCKER_CONTEXT")
	defer func() {
		os.Setenv("DOCKER_HOST", oldDockerHost)
		os.Setenv("DOCKER_CONTEXT", oldDockerContext)
	}()

	os.Setenv("DOCKER_HOST", "")
	os.Setenv("DOCKER_CONTEXT", "nonexistent-context-12345")

	// Reset cache for test
	ResetDockerHostCache()

	log := logrus.NewEntry(logrus.New())
	_, _, err := DetectDockerHost(log)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to use DOCKER_CONTEXT")
}

func TestDetectDockerHost_Context_Error_NoEnv(t *testing.T) {
	// Save and restore
	oldGetHost := getHostFromContextFunc
	oldDetectPlatform := detectPlatformCandidatesFunc
	defer func() {
		getHostFromContextFunc = oldGetHost
		detectPlatformCandidatesFunc = oldDetectPlatform
	}()

	// Mock getHostFromContext to return error
	getHostFromContextFunc = func() (string, error) {
		return "", errors.New("context error")
	}

	// Mock detectPlatformCandidates to return success so we can see it falls through
	detectPlatformCandidatesFunc = func(log *logrus.Entry) (string, ContainerRuntime, error) {
		return "unix:///tmp/fallback.sock", RuntimeDocker, nil
	}

	// Clear DOCKER_HOST
	oldDockerHost := os.Getenv("DOCKER_HOST")
	os.Setenv("DOCKER_HOST", "")
	defer os.Setenv("DOCKER_HOST", oldDockerHost)

	ResetDockerHostCache()

	log := logrus.NewEntry(logrus.New())
	host, runtime, err := DetectDockerHost(log)
	assert.NoError(t, err)
	assert.Equal(t, "unix:///tmp/fallback.sock", host)
	assert.Equal(t, RuntimeDocker, runtime)
}

func TestDetectDockerHost_SSH(t *testing.T) {
	oldDockerHost := os.Getenv("DOCKER_HOST")
	os.Setenv("DOCKER_HOST", "ssh://user@host")
	defer os.Setenv("DOCKER_HOST", oldDockerHost)

	ResetDockerHostCache()

	log := logrus.NewEntry(logrus.New())
	host, _, err := DetectDockerHost(log)
	assert.NoError(t, err)
	assert.Equal(t, "ssh://user@host", host)
}

func TestDetectDockerHost_PlainPath(t *testing.T) {
	oldDockerHost := os.Getenv("DOCKER_HOST")
	defer os.Setenv("DOCKER_HOST", oldDockerHost)

	// Create a temporary file to act as a socket
	tmpFile, err := os.CreateTemp("", "lazydocker-test-*.sock")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	os.Setenv("DOCKER_HOST", tmpFile.Name())

	ResetDockerHostCache()

	// Mock validateSocketFunc to succeed
	oldValidate := validateSocketFunc
	defer func() { validateSocketFunc = oldValidate }()
	validateSocketFunc = func(ctx context.Context, host string, useEnv bool) error {
		return nil
	}

	log := logrus.NewEntry(logrus.New())
	host, _, err := DetectDockerHost(log)
	assert.NoError(t, err)
	assert.Equal(t, "unix://"+tmpFile.Name(), host)
}

func TestValidateSocket_UseEnv(t *testing.T) {
	ctx := context.Background()
	oldDockerHost := os.Getenv("DOCKER_HOST")
	os.Setenv("DOCKER_HOST", "unix:///tmp/test.sock")
	defer os.Setenv("DOCKER_HOST", oldDockerHost)

	// This will fail ping but cover the useEnv branch
	err := validateSocket(ctx, "unix:///tmp/test.sock", true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ping failed")
}

func TestGetHostFromContext_Implementation(t *testing.T) {
	// Save and restore
	oldLoad := cliconfigLoadFunc
	defer func() { cliconfigLoadFunc = oldLoad }()

	t.Run("DOCKER_CONTEXT set", func(t *testing.T) {
		oldCtx := os.Getenv("DOCKER_CONTEXT")
		os.Setenv("DOCKER_CONTEXT", "default")
		defer os.Setenv("DOCKER_CONTEXT", oldCtx)

		host, err := getHostFromContext()
		assert.NoError(t, err)
		assert.Empty(t, host)
	})

	t.Run("Config load success but empty context", func(t *testing.T) {
		cliconfigLoadFunc = func(dir string) (*configfile.ConfigFile, error) {
			return &configfile.ConfigFile{CurrentContext: ""}, nil
		}
		host, err := getHostFromContext()
		assert.NoError(t, err)
		assert.Empty(t, host)
	})

	t.Run("Config load error", func(t *testing.T) {
		cliconfigLoadFunc = func(dir string) (*configfile.ConfigFile, error) {
			return nil, errors.New("load error")
		}
		_, err := getHostFromContext()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "load error")
	})
}

func TestValidateSocket_Failures(t *testing.T) {
	ctx := context.Background()

	// Test non-existent path
	err := validateSocket(ctx, "unix:///tmp/nonexistent-12345.sock", false)
	assert.Error(t, err)

	// Test invalid schema
	err = validateSocket(ctx, "invalid:///tmp/test.sock", false)
	assert.Error(t, err)
}

func TestDetectPlatformCandidates_Unix(t *testing.T) {
	// Save and restore
	oldStat := statFunc
	oldValidate := validateSocketFunc
	defer func() {
		statFunc = oldStat
		validateSocketFunc = oldValidate
	}()

	log := logrus.New().WithField("test", "test")

	t.Run("No sockets exist", func(t *testing.T) {
		statFunc = func(name string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		}
		_, _, err := detectPlatformCandidates(log)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ensure Docker or Podman is running")
	})

	t.Run("Docker socket exists and is valid", func(t *testing.T) {
		statFunc = func(name string) (os.FileInfo, error) {
			if name == "/var/run/docker.sock" {
				return nil, nil
			}
			return nil, os.ErrNotExist
		}
		validateSocketFunc = func(ctx context.Context, host string, silent bool) error {
			if host == "unix:///var/run/docker.sock" {
				return nil
			}
			return errors.New("invalid")
		}
		path, runtime, err := detectPlatformCandidates(log)
		assert.NoError(t, err)
		assert.Equal(t, "unix:///var/run/docker.sock", path)
		assert.Equal(t, RuntimeDocker, runtime)
	})

	t.Run("Docker socket exists but permission denied", func(t *testing.T) {
		statFunc = func(name string) (os.FileInfo, error) {
			if name == "/var/run/docker.sock" {
				return nil, nil
			}
			return nil, os.ErrNotExist
		}
		validateSocketFunc = func(ctx context.Context, host string, silent bool) error {
			return errors.New("permission denied")
		}
		_, _, err := detectPlatformCandidates(log)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "permission denied")
	})

	t.Run("Docker socket exists but other error", func(t *testing.T) {
		statFunc = func(name string) (os.FileInfo, error) {
			if name == "/var/run/docker.sock" {
				return nil, nil
			}
			return nil, os.ErrNotExist
		}
		validateSocketFunc = func(ctx context.Context, host string, silent bool) error {
			return errors.New("other error")
		}
		_, _, err := detectPlatformCandidates(log)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "other error")
	})

	t.Run("Podman socket exists and is valid", func(t *testing.T) {
		// Mock getuid to return 1000
		oldGetuid := getuidFunc
		getuidFunc = func() int { return 1000 }
		defer func() { getuidFunc = oldGetuid }()

		statFunc = func(name string) (os.FileInfo, error) {
			if name == "/run/user/1000/podman/podman.sock" {
				return nil, nil
			}
			return nil, os.ErrNotExist
		}
		validateSocketFunc = func(ctx context.Context, host string, silent bool) error {
			if host == "unix:///run/user/1000/podman/podman.sock" {
				return nil
			}
			return errors.New("invalid")
		}
		path, runtime, err := detectPlatformCandidates(log)
		assert.NoError(t, err)
		assert.Equal(t, "unix:///run/user/1000/podman/podman.sock", path)
		assert.Equal(t, RuntimePodman, runtime)
	})
}
