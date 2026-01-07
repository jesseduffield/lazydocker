// Package hook is the 0.1.0 hook configuration structure.
package hook

import (
	"encoding/json"
	"errors"
	"strings"

	rspec "github.com/opencontainers/runtime-spec/specs-go"
	current "go.podman.io/common/pkg/hooks/1.0.0"
)

// Version is the hook configuration version defined in this package.
const Version = "0.1.0"

// Hook is the hook configuration structure.
type Hook struct {
	Hook      *string  `json:"hook"`
	Arguments []string `json:"arguments,omitempty"`

	// https://github.com/cri-o/cri-o/pull/1235
	Stages []string `json:"stages"`
	Stage  []string `json:"stage"`

	Cmds []string `json:"cmds,omitempty"`
	Cmd  []string `json:"cmd,omitempty"`

	Annotations []string `json:"annotations,omitempty"`
	Annotation  []string `json:"annotation,omitempty"`

	HasBindMounts *bool `json:"hasbindmounts,omitempty"`
}

func Read(content []byte) (hook *current.Hook, err error) {
	var raw Hook

	if err = json.Unmarshal(content, &raw); err != nil {
		return nil, err
	}

	if raw.Hook == nil {
		return nil, errors.New("missing required property: hook")
	}

	if raw.Stages == nil {
		raw.Stages = raw.Stage
	} else if raw.Stage != nil {
		return nil, errors.New("cannot set both 'stage' and 'stages'")
	}
	if raw.Stages == nil {
		return nil, errors.New("missing required property: stages")
	}

	if raw.Cmds == nil {
		raw.Cmds = raw.Cmd
	} else if raw.Cmd != nil {
		return nil, errors.New("cannot set both 'cmd' and 'cmds'")
	}

	if raw.Annotations == nil {
		raw.Annotations = raw.Annotation
	} else if raw.Annotation != nil {
		return nil, errors.New("cannot set both 'annotation' and 'annotations'")
	}

	hook = &current.Hook{
		Version: current.Version,
		Hook: rspec.Hook{
			Path: *raw.Hook,
		},
		When: current.When{
			Commands:      raw.Cmds,
			HasBindMounts: raw.HasBindMounts,
			Or:            true,
		},
		Stages: raw.Stages,
	}
	if raw.Arguments != nil {
		hook.Hook.Args = append([]string{*raw.Hook}, raw.Arguments...)
	}
	if raw.Annotations != nil {
		hook.When.Annotations = map[string]string{
			".*": strings.Join(raw.Annotations, "|"),
		}
	}

	return hook, nil
}
