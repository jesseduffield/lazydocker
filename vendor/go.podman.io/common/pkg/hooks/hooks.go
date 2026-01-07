// Package hooks implements hook configuration and handling for CRI-O and libpod.
package hooks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	current "go.podman.io/common/pkg/hooks/1.0.0"
)

// Version is the current hook configuration version.
const Version = current.Version

const (
	// DefaultDir is the default directory containing system hook configuration files.
	DefaultDir = "/usr/share/containers/oci/hooks.d"

	// OverrideDir is the directory for hook configuration files overriding the default entries.
	OverrideDir = "/etc/containers/oci/hooks.d"
)

// Manager provides an opaque interface for managing CRI-O hooks.
type Manager struct {
	hooks           map[string]*current.Hook
	directories     []string
	extensionStages []string
	lock            sync.Mutex
}

type namedHook struct {
	name string
	hook *current.Hook
}

// New creates a new hook manager.  Directories are ordered by
// increasing preference (hook configurations in later directories
// override configurations with the same filename from earlier
// directories).
//
// extensionStages allows callers to add additional stages beyond
// those specified in the OCI Runtime Specification and to control
// OCI-defined stages instead of delegating to the OCI runtime.  See
// Hooks() for more information.
func New(_ context.Context, directories []string, extensionStages []string) (manager *Manager, err error) {
	manager = &Manager{
		hooks:           map[string]*current.Hook{},
		directories:     directories,
		extensionStages: extensionStages,
	}

	for _, dir := range directories {
		err = ReadDir(dir, manager.extensionStages, manager.hooks)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}

	return manager, nil
}

// filenames returns sorted hook entries.
func (m *Manager) namedHooks() (hooks []*namedHook) {
	m.lock.Lock()
	defer m.lock.Unlock()

	hooks = make([]*namedHook, len(m.hooks))
	i := 0
	for name, hook := range m.hooks {
		hooks[i] = &namedHook{
			name: name,
			hook: hook,
		}
		i++
	}

	return hooks
}

// Hooks injects OCI runtime hooks for a given container configuration.
//
// If extensionStages was set when initializing the Manager,
// matching hooks requesting those stages will be returned in
// extensionStageHooks.  This takes precedence over their inclusion in
// the OCI configuration.  For example:
//
//	manager, err := New(ctx, []string{DefaultDir}, []string{"poststop"})
//	extensionStageHooks, err := manager.Hooks(config, annotations, hasBindMounts)
//
// will have any matching post-stop hooks in extensionStageHooks and
// will not insert them into config.Hooks.Poststop.
func (m *Manager) Hooks(config *rspec.Spec, annotations map[string]string, hasBindMounts bool) (extensionStageHooks map[string][]rspec.Hook, err error) {
	hooks := m.namedHooks()
	sort.Slice(hooks, func(i, j int) bool { return strings.ToLower(hooks[i].name) < strings.ToLower(hooks[j].name) })
	localStages := map[string]bool{} // stages destined for extensionStageHooks
	for _, stage := range m.extensionStages {
		localStages[stage] = true
	}
	for _, namedHook := range hooks {
		match, err := namedHook.hook.When.Match(config, annotations, hasBindMounts)
		if err != nil {
			return extensionStageHooks, fmt.Errorf("matching hook %q: %w", namedHook.name, err)
		}
		if match {
			logrus.Debugf("hook %s matched; adding to stages %v", namedHook.name, namedHook.hook.Stages)
			if config.Hooks == nil {
				config.Hooks = &rspec.Hooks{}
			}
			for _, stage := range namedHook.hook.Stages {
				if _, ok := localStages[stage]; ok {
					if extensionStageHooks == nil {
						extensionStageHooks = map[string][]rspec.Hook{}
					}
					extensionStageHooks[stage] = append(extensionStageHooks[stage], namedHook.hook.Hook)
				} else {
					switch stage {
					case "createContainer":
						config.Hooks.CreateContainer = append(config.Hooks.CreateContainer, namedHook.hook.Hook)
					case "createRuntime", "prestart":
						config.Hooks.CreateRuntime = append(config.Hooks.CreateRuntime, namedHook.hook.Hook)
					case "poststart":
						config.Hooks.Poststart = append(config.Hooks.Poststart, namedHook.hook.Hook)
					case "poststop":
						config.Hooks.Poststop = append(config.Hooks.Poststop, namedHook.hook.Hook)
					case "startContainer":
						config.Hooks.StartContainer = append(config.Hooks.StartContainer, namedHook.hook.Hook)
					default:
						return extensionStageHooks, fmt.Errorf("hook %q: unknown stage %q", namedHook.name, stage)
					}
				}
			}
		} else {
			logrus.Debugf("hook %s did not match", namedHook.name)
		}
	}

	return extensionStageHooks, nil
}
