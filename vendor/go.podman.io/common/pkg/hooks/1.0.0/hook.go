// Package hook is the 1.0.0 hook configuration structure.
package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"go.podman.io/storage/pkg/fileutils"
)

// Version is the hook configuration version defined in this package.
const Version = "1.0.0"

// Hook is the hook configuration structure.
type Hook struct {
	Version string     `json:"version"`
	Hook    rspec.Hook `json:"hook"`
	When    When       `json:"when"`
	Stages  []string   `json:"stages"`
}

// Read reads hook JSON bytes, verifies them, and returns the hook configuration.
func Read(content []byte) (hook *Hook, err error) {
	if err = json.Unmarshal(content, &hook); err != nil {
		return nil, err
	}
	return hook, nil
}

// Validate performs load-time hook validation.
func (hook *Hook) Validate(extensionStages []string) (err error) {
	if hook == nil {
		return errors.New("nil hook")
	}

	if hook.Version != Version {
		return fmt.Errorf("unexpected hook version %q (expecting %v)", hook.Version, Version)
	}

	if hook.Hook.Path == "" {
		return errors.New("missing required property: hook.path")
	}

	if err := fileutils.Exists(hook.Hook.Path); err != nil {
		return err
	}

	for key, value := range hook.When.Annotations {
		if _, err = regexp.Compile(key); err != nil {
			return fmt.Errorf("invalid annotation key %q: %w", key, err)
		}
		if _, err = regexp.Compile(value); err != nil {
			return fmt.Errorf("invalid annotation value %q: %w", value, err)
		}
	}

	for _, command := range hook.When.Commands {
		if _, err = regexp.Compile(command); err != nil {
			return fmt.Errorf("invalid command %q: %w", command, err)
		}
	}

	if hook.Stages == nil {
		return errors.New("missing required property: stages")
	}

	validStages := map[string]bool{
		"createContainer": true,
		"createRuntime":   true,
		"prestart":        true,
		"poststart":       true,
		"poststop":        true,
		"startContainer":  true,
	}
	for _, stage := range extensionStages {
		validStages[stage] = true
	}

	for _, stage := range hook.Stages {
		if !validStages[stage] {
			return fmt.Errorf("unknown stage %q", stage)
		}
	}

	return nil
}
