package config

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/containers/buildah/docker"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/openshift/imagebuilder"
	"go.podman.io/image/v5/manifest"
)

// firstStringElseSecondString takes two strings, and returns the first
// string if it isn't empty, else the second string
func firstStringElseSecondString(first, second string) string {
	if first != "" {
		return first
	}
	return second
}

// firstSliceElseSecondSlice takes two string slices, and returns the first
// slice of strings if it has contents, else the second slice
func firstSliceElseSecondSlice(first, second []string) []string {
	if len(first) > 0 {
		return slices.Clone(first)
	}
	return slices.Clone(second)
}

// firstSlicePairElseSecondSlicePair takes two pairs of string slices, and
// returns the first pair of slices if either has contents, else the second
// pair
func firstSlicePairElseSecondSlicePair(firstA, firstB, secondA, secondB []string) ([]string, []string) {
	if len(firstA) > 0 || len(firstB) > 0 {
		return slices.Clone(firstA), slices.Clone(firstB)
	}
	return slices.Clone(secondA), slices.Clone(secondB)
}

// mergeEnv combines variables from a and b into a single environment slice. if
// a and b both provide values for the same variable, the value from b is
// preferred
func mergeEnv(a, b []string) []string {
	index := make(map[string]int)
	results := make([]string, 0, len(a)+len(b))
	for _, kv := range slices.Concat(a, b) {
		k, _, specifiesValue := strings.Cut(kv, "=")
		if !specifiesValue {
			if value, ok := os.LookupEnv(kv); ok {
				kv = kv + "=" + value
			} else {
				kv = kv + "="
			}
		}
		if i, seen := index[k]; seen {
			results[i] = kv
		} else {
			index[k] = len(results)
			results = append(results, kv)
		}
	}
	return results
}

func parseOverrideChanges(overrideChanges []string, overrideConfig *manifest.Schema2Config) (*manifest.Schema2Config, error) {
	if len(overrideChanges) == 0 {
		return overrideConfig, nil
	}
	if overrideConfig == nil {
		overrideConfig = &manifest.Schema2Config{}
	}
	// Parse the set of changes as we would a Dockerfile.
	changes := strings.Join(overrideChanges, "\n")
	parsed, err := imagebuilder.ParseDockerfile(strings.NewReader(changes))
	if err != nil {
		return overrideConfig, fmt.Errorf("parsing change set %+v: %w", changes, err)
	}
	// Create a dummy builder object to process configuration-related
	// instructions.
	subBuilder := imagebuilder.NewBuilder(nil)
	// Convert the incoming data into an initial RunConfig.
	subBuilder.RunConfig = *GoDockerclientConfigFromSchema2Config(overrideConfig)
	// Process the change instructions one by one.
	for _, node := range parsed.Children {
		var step imagebuilder.Step
		if err := step.Resolve(node); err != nil {
			return overrideConfig, fmt.Errorf("resolving change %q: %w", node.Original, err)
		}
		if err := subBuilder.Run(&step, &configOnlyExecutor{}, true); err != nil {
			return overrideConfig, fmt.Errorf("processing change %q: %w", node.Original, err)
		}
	}
	// Pull settings out of the dummy builder's RunConfig.
	return Schema2ConfigFromGoDockerclientConfig(&subBuilder.RunConfig), nil
}

// OverrideOCI takes a buildah docker config and an OCI ImageConfig, and applies a
// mixture of a slice of Dockerfile-style instructions and fields from a config
// blob to them both
func OverrideOCI(oconfig *v1.ImageConfig, overrideChanges []string, overrideConfig *manifest.Schema2Config) error {
	overrideConfig, err := parseOverrideChanges(overrideChanges, overrideConfig)
	if err != nil {
		return err
	}

	if overrideConfig != nil {
		// Apply changes from a possibly-provided possibly-changed config struct.
		oconfig.User = firstStringElseSecondString(overrideConfig.User, oconfig.User)
		if len(overrideConfig.ExposedPorts) > 0 {
			oexposedPorts := make(map[string]struct{})
			for port := range oconfig.ExposedPorts {
				oexposedPorts[port] = struct{}{}
			}
			for port := range overrideConfig.ExposedPorts {
				oexposedPorts[string(port)] = struct{}{}
			}
			oconfig.ExposedPorts = oexposedPorts
		}
		if len(overrideConfig.Env) > 0 {
			oconfig.Env = mergeEnv(oconfig.Env, overrideConfig.Env)
		}
		oconfig.Entrypoint, oconfig.Cmd = firstSlicePairElseSecondSlicePair(overrideConfig.Entrypoint, overrideConfig.Cmd, oconfig.Entrypoint, oconfig.Cmd)
		if len(overrideConfig.Volumes) > 0 {
			if oconfig.Volumes == nil {
				oconfig.Volumes = make(map[string]struct{})
			}
			for volume := range overrideConfig.Volumes {
				oconfig.Volumes[volume] = struct{}{}
			}
		}
		oconfig.WorkingDir = firstStringElseSecondString(overrideConfig.WorkingDir, oconfig.WorkingDir)
		if len(overrideConfig.Labels) > 0 {
			if oconfig.Labels == nil {
				oconfig.Labels = make(map[string]string)
			}
			maps.Copy(oconfig.Labels, overrideConfig.Labels)
		}
		oconfig.StopSignal = overrideConfig.StopSignal
	}
	return nil
}

// OverrideDocker takes a buildah docker config and an Docker Config, and applies a
// mixture of a slice of Dockerfile-style instructions and fields from a config
// blob to them both
func OverrideDocker(dconfig *docker.Config, overrideChanges []string, overrideConfig *manifest.Schema2Config) error {
	overrideConfig, err := parseOverrideChanges(overrideChanges, overrideConfig)
	if err != nil {
		return err
	}

	if overrideConfig != nil {
		// Apply changes from a possibly-provided possibly-changed config struct.
		dconfig.Hostname = firstStringElseSecondString(overrideConfig.Hostname, dconfig.Hostname)
		dconfig.Domainname = firstStringElseSecondString(overrideConfig.Domainname, dconfig.Domainname)
		dconfig.User = firstStringElseSecondString(overrideConfig.User, dconfig.User)
		dconfig.AttachStdin = overrideConfig.AttachStdin
		dconfig.AttachStdout = overrideConfig.AttachStdout
		dconfig.AttachStderr = overrideConfig.AttachStderr
		if len(overrideConfig.ExposedPorts) > 0 {
			dexposedPorts := make(map[docker.Port]struct{})
			for port := range dconfig.ExposedPorts {
				dexposedPorts[port] = struct{}{}
			}
			for port := range overrideConfig.ExposedPorts {
				dexposedPorts[docker.Port(port)] = struct{}{}
			}
			dconfig.ExposedPorts = dexposedPorts
		}
		dconfig.Tty = overrideConfig.Tty
		dconfig.OpenStdin = overrideConfig.OpenStdin
		dconfig.StdinOnce = overrideConfig.StdinOnce
		if len(overrideConfig.Env) > 0 {
			dconfig.Env = mergeEnv(dconfig.Env, overrideConfig.Env)
		}
		dconfig.Entrypoint, dconfig.Cmd = firstSlicePairElseSecondSlicePair(overrideConfig.Entrypoint, overrideConfig.Cmd, dconfig.Entrypoint, dconfig.Cmd)
		if overrideConfig.Healthcheck != nil {
			dconfig.Healthcheck = &docker.HealthConfig{
				Test:        slices.Clone(overrideConfig.Healthcheck.Test),
				Interval:    overrideConfig.Healthcheck.Interval,
				Timeout:     overrideConfig.Healthcheck.Timeout,
				StartPeriod: overrideConfig.Healthcheck.StartPeriod,
				Retries:     overrideConfig.Healthcheck.Retries,
			}
		}
		dconfig.ArgsEscaped = overrideConfig.ArgsEscaped
		dconfig.Image = firstStringElseSecondString(overrideConfig.Image, dconfig.Image)
		if len(overrideConfig.Volumes) > 0 {
			if dconfig.Volumes == nil {
				dconfig.Volumes = make(map[string]struct{})
			}
			for volume := range overrideConfig.Volumes {
				dconfig.Volumes[volume] = struct{}{}
			}
		}
		dconfig.WorkingDir = firstStringElseSecondString(overrideConfig.WorkingDir, dconfig.WorkingDir)
		dconfig.NetworkDisabled = overrideConfig.NetworkDisabled
		dconfig.MacAddress = overrideConfig.MacAddress
		dconfig.OnBuild = overrideConfig.OnBuild
		if len(overrideConfig.Labels) > 0 {
			if dconfig.Labels == nil {
				dconfig.Labels = make(map[string]string)
			}
			maps.Copy(dconfig.Labels, overrideConfig.Labels)
		}
		dconfig.StopSignal = overrideConfig.StopSignal
		dconfig.StopTimeout = overrideConfig.StopTimeout
		dconfig.Shell = firstSliceElseSecondSlice(overrideConfig.Shell, dconfig.Shell)
	}
	return nil
}
