package hook

import (
	"errors"
	"fmt"
	"regexp"

	rspec "github.com/opencontainers/runtime-spec/specs-go"
)

// When holds hook-injection conditions.
type When struct {
	Always        *bool             `json:"always,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
	Commands      []string          `json:"commands,omitempty"`
	HasBindMounts *bool             `json:"hasBindMounts,omitempty"`

	// Or enables any-of matching.
	//
	// Deprecated: this property is for is backwards-compatibility with
	// 0.1.0 hooks.  It will be removed when we drop support for them.
	Or bool `json:"-"`
}

// Match returns true if the given conditions match the configuration.
func (when *When) Match(config *rspec.Spec, annotations map[string]string, hasBindMounts bool) (match bool, err error) {
	matches := 0

	if when.Always != nil {
		if *when.Always {
			if when.Or {
				return true, nil
			}
			matches++
		} else if !when.Or {
			return false, nil
		}
	}

	if when.HasBindMounts != nil {
		if *when.HasBindMounts && hasBindMounts {
			if when.Or {
				return true, nil
			}
			matches++
		} else if !when.Or {
			return false, nil
		}
	}

	for keyPattern, valuePattern := range when.Annotations {
		match := false
		for key, value := range annotations {
			match, err = regexp.MatchString(keyPattern, key)
			if err != nil {
				return false, fmt.Errorf("annotation key: %w", err)
			}
			if match {
				match, err = regexp.MatchString(valuePattern, value)
				if err != nil {
					return false, fmt.Errorf("annotation value: %w", err)
				}
				if match {
					break
				}
			}
		}
		if match {
			if when.Or {
				return true, nil
			}
			matches++
		} else if !when.Or {
			return false, nil
		}
	}

	if config.Process != nil && len(when.Commands) > 0 {
		if len(config.Process.Args) == 0 {
			return false, errors.New("process.args must have at least one entry")
		}
		command := config.Process.Args[0]
		for _, cmdPattern := range when.Commands {
			match, err := regexp.MatchString(cmdPattern, command)
			if err != nil {
				return false, fmt.Errorf("command: %w", err)
			}
			if match {
				return true, nil
			}
		}
		return false, nil
	}

	return matches > 0, nil
}
