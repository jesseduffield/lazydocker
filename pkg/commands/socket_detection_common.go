package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	cliconfig "github.com/docker/cli/cli/config"
	ddocker "github.com/docker/cli/cli/context/docker"
	ctxstore "github.com/docker/cli/cli/context/store"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

var (
	ErrNoDockerSocket = fmt.Errorf("no working Docker/Podman socket found")
)

// Timeout for validating socket connectivity
const socketValidationTimeout = 3 * time.Second

var (
	validateSocketFunc           = validateSocket
	inferRuntimeFromHostFunc     = inferRuntimeFromHost
	getHostFromContextFunc       = getHostFromContext
	detectPlatformCandidatesFunc = detectPlatformCandidates

	// For testing getHostFromContext
	cliconfigLoadFunc = cliconfig.Load
	ctxstoreNewFunc   = ctxstore.New
)

// Runtime type detection
type ContainerRuntime string

const (
	RuntimeDocker  ContainerRuntime = "docker"
	RuntimePodman  ContainerRuntime = "podman"
	RuntimeUnknown ContainerRuntime = "unknown"
)

// Cache for socket detection results
var (
	cachedDockerHost string
	cachedRuntime    ContainerRuntime
	dockerHostMu     sync.Mutex
)

// DetectDockerHost finds a working Docker/Podman socket
// Results are cached after first successful detection
func DetectDockerHost(log *logrus.Entry) (string, ContainerRuntime, error) {
	dockerHostMu.Lock()
	defer dockerHostMu.Unlock()

	if cachedDockerHost != "" {
		return cachedDockerHost, cachedRuntime, nil
	}

	host, runtime, err := detectDockerHostInternal(log)
	if err != nil {
		return "", RuntimeUnknown, err
	}

	cachedDockerHost = host
	cachedRuntime = runtime
	return host, runtime, nil
}

// ResetDockerHostCache resets the cached docker host.
//
// This is serialized with DetectDockerHost via dockerHostMu.
// Primarily used for testing.
func ResetDockerHostCache() {
	dockerHostMu.Lock()
	defer dockerHostMu.Unlock()
	cachedDockerHost = ""
	cachedRuntime = RuntimeUnknown
}

func inferRuntimeFromHostHeuristic(host string) ContainerRuntime {
	lowerHost := strings.ToLower(host)
	if strings.Contains(lowerHost, "podman") {
		return RuntimePodman
	}
	return RuntimeDocker
}

func detectDockerHostInternal(log *logrus.Entry) (string, ContainerRuntime, error) {
	// Priority 1: Explicit DOCKER_HOST environment variable
	if dockerHost := os.Getenv("DOCKER_HOST"); dockerHost != "" {
		log.Debugf("Using DOCKER_HOST from environment: %s", dockerHost)

		// Handle plain paths without schema
		if !strings.Contains(dockerHost, "://") {
			if _, err := os.Stat(dockerHost); err == nil {
				log.Debugf("DOCKER_HOST is a plain path, assuming %s", DockerSocketSchema)
				dockerHost = DockerSocketSchema + dockerHost
			}
		}

		if !strings.HasPrefix(dockerHost, "ssh://") {
			ctx, cancel := context.WithTimeout(context.Background(), socketValidationTimeout)
			defer cancel()
			if err := validateSocketFunc(ctx, dockerHost, true); err != nil {
				return "", RuntimeUnknown, fmt.Errorf("DOCKER_HOST=%s is set but not accessible: %w", dockerHost, err)
			}

			runtime, err := inferRuntimeFromHostFunc(ctx, dockerHost, true)
			if err != nil {
				log.Debugf("Failed to infer runtime for DOCKER_HOST=%s: %v", dockerHost, err)
				runtime = inferRuntimeFromHostHeuristic(dockerHost)
			}
			return dockerHost, runtime, nil
		}

		// SSH hosts can point to either Docker or Podman; we don't attempt runtime inference here.
		return dockerHost, RuntimeUnknown, nil
	}

	// Priority 2: Docker Context
	contextHost, err := getHostFromContextFunc()
	if err != nil {
		// If DOCKER_CONTEXT was explicitly set, we should fail
		if os.Getenv("DOCKER_CONTEXT") != "" {
			return "", RuntimeUnknown, fmt.Errorf("failed to use DOCKER_CONTEXT: %w", err)
		}
		log.Debugf("Failed to get host from default context: %v", err)
	} else if contextHost != "" {
		log.Debugf("Using host from Docker context: %s", contextHost)
		isValid := true
		if !strings.HasPrefix(contextHost, "ssh://") {
			ctx, cancel := context.WithTimeout(context.Background(), socketValidationTimeout)
			defer cancel()
			if err := validateSocketFunc(ctx, contextHost, false); err != nil {
				if os.Getenv("DOCKER_CONTEXT") != "" {
					return "", RuntimeUnknown, fmt.Errorf("DOCKER_CONTEXT host %s is not accessible: %w", contextHost, err)
				}
				log.Warnf("Context host %s is not accessible: %v", contextHost, err)
				isValid = false
			}
		}

		if isValid {
			ctx, cancel := context.WithTimeout(context.Background(), socketValidationTimeout)
			defer cancel()
			runtime, err := inferRuntimeFromHostFunc(ctx, contextHost, false)
			if err != nil {
				log.Debugf("Failed to infer runtime for Docker context host %s: %v", contextHost, err)
				runtime = inferRuntimeFromHostHeuristic(contextHost)
			}
			return contextHost, runtime, nil
		}
	}

	// Priority 3: Platform-specific candidates
	return detectPlatformCandidatesFunc(log)
}

// getHostFromContext retrieves the host from the current Docker context
func getHostFromContext() (string, error) {
	currentContext := os.Getenv("DOCKER_CONTEXT")
	if currentContext == "" {
		cf, err := cliconfigLoadFunc(cliconfig.Dir())
		if err != nil {
			return "", err
		}
		currentContext = cf.CurrentContext
	}

	if currentContext == "" || currentContext == "default" {
		return "", nil
	}

	storeConfig := ctxstore.NewConfig(
		func() interface{} { return &ddocker.EndpointMeta{} },
		ctxstore.EndpointTypeGetter(ddocker.DockerEndpoint, func() interface{} { return &ddocker.EndpointMeta{} }),
	)

	st := ctxstoreNewFunc(cliconfig.ContextStoreDir(), storeConfig)
	md, err := st.GetMetadata(currentContext)
	if err != nil {
		return "", err
	}
	dockerEP, ok := md.Endpoints[ddocker.DockerEndpoint]
	if !ok {
		return "", nil
	}
	dockerEPMeta, ok := dockerEP.(ddocker.EndpointMeta)
	if !ok {
		return "", fmt.Errorf("expected docker.EndpointMeta, got %T", dockerEP)
	}

	return dockerEPMeta.Host, nil
}

// validateSocket attempts to connect to the Docker API at the given host
func validateSocket(ctx context.Context, host string, useEnv bool) error {
	var opts []client.Opt
	if useEnv {
		// If we're validating the host from the environment, use FromEnv to pick up TLS settings
		opts = append(opts, client.FromEnv)
	}
	opts = append(opts, client.WithHost(host), client.WithAPIVersionNegotiation())

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	defer cli.Close()

	_, err = cli.Ping(ctx)
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	return nil
}

// inferRuntimeFromHost inspects the engine behind the host to distinguish Docker vs Podman.
//
// It uses the Docker-compatible API. Podman supports this and typically reports itself
// via version metadata.
func inferRuntimeFromHost(ctx context.Context, host string, useEnv bool) (ContainerRuntime, error) {
	var opts []client.Opt
	if useEnv {
		// If we're validating/inferencing the host from the environment, use FromEnv to pick up TLS settings
		opts = append(opts, client.FromEnv)
	}
	opts = append(opts, client.WithHost(host), client.WithAPIVersionNegotiation())

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return RuntimeUnknown, fmt.Errorf("create client: %w", err)
	}
	defer cli.Close()

	v, err := cli.ServerVersion(ctx)
	if err != nil {
		return RuntimeUnknown, fmt.Errorf("server version: %w", err)
	}

	// Heuristics: Podman typically identifies itself in platform name or components.
	needle := "podman"
	if strings.Contains(strings.ToLower(v.Platform.Name), needle) {
		return RuntimePodman, nil
	}
	for _, c := range v.Components {
		if strings.Contains(strings.ToLower(c.Name), needle) {
			return RuntimePodman, nil
		}
	}

	return RuntimeDocker, nil
}
