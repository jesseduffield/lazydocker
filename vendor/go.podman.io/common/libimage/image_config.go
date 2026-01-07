//go:build !remote

package libimage

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	ociv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/common/pkg/signal"
)

// ImageConfig is a wrapper around the OCIv1 Image Configuration struct exported
// by containers/image, but containing additional fields that are not supported
// by OCIv1 (but are by Docker v2) - notably OnBuild.
type ImageConfig struct {
	ociv1.ImageConfig
	OnBuild []string
}

// ImageConfigFromChanges produces a v1.ImageConfig from the --change flag that
// is accepted by several Podman commands. It accepts a (limited subset) of
// Dockerfile instructions.
// Valid changes are:
// * USER
// * EXPOSE
// * ENV
// * ENTRYPOINT
// * CMD
// * VOLUME
// * WORKDIR
// * LABEL
// * STOPSIGNAL
// * ONBUILD.
func ImageConfigFromChanges(changes []string) (*ImageConfig, error) { // nolint:gocyclo
	config := &ImageConfig{}

	for _, change := range changes {
		// First, let's assume proper Dockerfile format - space
		// separator between instruction and value
		outerKey, value, ok := strings.Cut(change, " ")

		if !ok {
			outerKey, value, ok = strings.Cut(change, "=")
			if !ok {
				return nil, fmt.Errorf("invalid change %q - must be formatted as KEY VALUE", change)
			}
		}

		outerKey = strings.ToUpper(strings.TrimSpace(outerKey))
		value = strings.TrimSpace(value)
		switch outerKey {
		case "USER":
			// Assume literal contents are the user.
			if value == "" {
				return nil, fmt.Errorf("invalid change %q - must provide a value to USER", change)
			}
			config.User = value
		case "EXPOSE":
			// EXPOSE is either [portnum] or
			// [portnum]/[proto]
			// Protocol must be "tcp" or "udp"
			splitPort := strings.Split(value, "/")
			if len(splitPort) > 2 {
				return nil, fmt.Errorf("invalid change %q - EXPOSE port must be formatted as PORT[/PROTO]", change)
			}
			portNum, err := strconv.Atoi(splitPort[0])
			if err != nil {
				return nil, fmt.Errorf("invalid change %q - EXPOSE port must be an integer: %w", change, err)
			}
			if portNum > 65535 || portNum <= 0 {
				return nil, fmt.Errorf("invalid change %q - EXPOSE port must be a valid port number", change)
			}
			proto := "tcp"
			if len(splitPort) > 1 {
				testProto := strings.ToLower(splitPort[1])
				switch testProto {
				case "tcp", "udp":
					proto = testProto
				default:
					return nil, fmt.Errorf("invalid change %q - EXPOSE protocol must be TCP or UDP", change)
				}
			}
			if config.ExposedPorts == nil {
				config.ExposedPorts = make(map[string]struct{})
			}
			config.ExposedPorts[fmt.Sprintf("%d/%s", portNum, proto)] = struct{}{}
		case "ENV":
			// Format is either:
			// ENV key=value
			// ENV key-1=value key-2=value ...
			// ENV key value
			// Both keys and values can be surrounded by quotes to group them.
			// For now: we only support key=value
			// We will attempt to strip quotation marks if present.

			key, val, _ := strings.Cut(value, "=") // val is "" if there is no "="
			// We do need a key
			if key == "" {
				return nil, fmt.Errorf("invalid change %q - ENV must have at least one argument", change)
			}

			if strings.HasPrefix(key, `"`) && strings.HasSuffix(key, `"`) {
				key = strings.TrimPrefix(strings.TrimSuffix(key, `"`), `"`)
			}
			if strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`) {
				val = strings.TrimPrefix(strings.TrimSuffix(val, `"`), `"`)
			}
			config.Env = append(config.Env, fmt.Sprintf("%s=%s", key, val))
		case "ENTRYPOINT":
			// Two valid forms.
			// First, JSON array.
			// Second, not a JSON array - we interpret this as an
			// argument to `sh -c`, unless empty, in which case we
			// just use a blank entrypoint.
			testUnmarshal := []string{}
			if err := json.Unmarshal([]byte(value), &testUnmarshal); err != nil {
				// It ain't valid JSON, so assume it's an
				// argument to sh -c if not empty.
				if value != "" {
					config.Entrypoint = []string{"/bin/sh", "-c", value}
				} else {
					config.Entrypoint = []string{}
				}
			} else {
				// Valid JSON
				config.Entrypoint = testUnmarshal
			}
		case "CMD":
			// Same valid forms as entrypoint.
			// However, where ENTRYPOINT assumes that 'ENTRYPOINT '
			// means no entrypoint, CMD assumes it is 'sh -c' with
			// no third argument.
			testUnmarshal := []string{}
			if err := json.Unmarshal([]byte(value), &testUnmarshal); err != nil {
				// It ain't valid JSON, so assume it's an
				// argument to sh -c.
				// Only include volume if it's not ""
				config.Cmd = []string{"/bin/sh", "-c"}
				if value != "" {
					config.Cmd = append(config.Cmd, value)
				}
			} else {
				// Valid JSON
				config.Cmd = testUnmarshal
			}
		case "VOLUME":
			// Either a JSON array or a set of space-separated
			// paths.
			// Acts rather similar to ENTRYPOINT and CMD, but always
			// appends rather than replacing, and no sh -c prepend.
			testUnmarshal := []string{}
			if err := json.Unmarshal([]byte(value), &testUnmarshal); err != nil {
				// Not valid JSON, so split on spaces
				testUnmarshal = strings.Split(value, " ")
			}
			if len(testUnmarshal) == 0 {
				return nil, fmt.Errorf("invalid change %q - must provide at least one argument to VOLUME", change)
			}
			for _, vol := range testUnmarshal {
				if vol == "" {
					return nil, fmt.Errorf("invalid change %q - VOLUME paths must not be empty", change)
				}
				if config.Volumes == nil {
					config.Volumes = make(map[string]struct{})
				}
				config.Volumes[vol] = struct{}{}
			}
		case "WORKDIR":
			// This can be passed multiple times.
			// Each successive invocation is treated as relative to
			// the previous one - so WORKDIR /A, WORKDIR b,
			// WORKDIR c results in /A/b/c
			// Just need to check it's not empty...
			if value == "" {
				return nil, fmt.Errorf("invalid change %q - must provide a non-empty WORKDIR", change)
			}
			config.WorkingDir = filepath.Join(config.WorkingDir, value)
		case "LABEL":
			// Same general idea as ENV, but we no longer allow " "
			// as a separator.
			// We didn't do that for ENV either, so nice and easy.
			// Potentially problematic: LABEL might theoretically
			// allow an = in the key? If people really do this, we
			// may need to investigate more advanced parsing.
			key, val, ok := strings.Cut(value, "=")
			// Unlike ENV, LABEL must have a value
			if !ok {
				return nil, fmt.Errorf("invalid change %q - LABEL must be formatted key=value", change)
			}

			if strings.HasPrefix(key, `"`) && strings.HasSuffix(key, `"`) {
				key = strings.TrimPrefix(strings.TrimSuffix(key, `"`), `"`)
			}
			if strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`) {
				val = strings.TrimPrefix(strings.TrimSuffix(val, `"`), `"`)
			}
			// Check key after we strip quotations
			if key == "" {
				return nil, fmt.Errorf("invalid change %q - LABEL must have a non-empty key", change)
			}
			if config.Labels == nil {
				config.Labels = make(map[string]string)
			}
			config.Labels[key] = val
		case "STOPSIGNAL":
			// Check the provided signal for validity.
			killSignal, err := signal.ParseSignal(value)
			if err != nil {
				return nil, fmt.Errorf("invalid change %q - KILLSIGNAL must be given a valid signal: %w", change, err)
			}
			config.StopSignal = fmt.Sprintf("%d", killSignal)
		case "ONBUILD":
			// Onbuild always appends.
			if value == "" {
				return nil, fmt.Errorf("invalid change %q - ONBUILD must be given an argument", change)
			}
			config.OnBuild = append(config.OnBuild, value)
		default:
			return nil, fmt.Errorf("invalid change %q - invalid instruction %s", change, outerKey)
		}
	}

	return config, nil
}
