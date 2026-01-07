package config

import (
	"fmt"
	"path/filepath"

	"github.com/hashicorp/go-multierror"
	"go.podman.io/storage/pkg/fileutils"
)

// LoadedModules returns absolute paths to loaded containers.conf modules.
func (c *Config) LoadedModules() []string {
	// Required for conmon's callback to Podman's cleanup.
	// Absolute paths make loading the modules a bit faster.
	return c.loadedModules
}

// Find the specified modules in the options.  Return an error if a specific
// module cannot be located on the host.
func (o *Options) modules(paths *paths) ([]string, error) {
	if len(o.Modules) == 0 {
		return nil, nil
	}

	dirs := moduleDirectories(paths)

	configs := make([]string, 0, len(o.Modules))
	for _, path := range o.Modules {
		resolved, err := resolveModule(path, dirs)
		if err != nil {
			return nil, fmt.Errorf("could not resolve module %q: %w", path, err)
		}
		configs = append(configs, resolved)
	}

	return configs, nil
}

// ModuleDirectories return the directories to load modules from:
// 1) XDG_CONFIG_HOME/HOME if rootless
// 2) /etc/
// 3) /usr/share.
func ModuleDirectories() ([]string, error) { // Public API for shell completions in Podman
	paths, err := defaultPaths()
	if err != nil {
		return nil, err
	}
	return moduleDirectories(paths), nil
}

func moduleDirectories(paths *paths) []string {
	const moduleSuffix = ".modules"
	modules := make([]string, 0, 3)
	if paths.uid > 0 {
		modules = append(modules, paths.home+moduleSuffix)
	}
	modules = append(modules, paths.etc+moduleSuffix)
	modules = append(modules, paths.usr+moduleSuffix)
	return modules
}

// Resolve the specified path to a module.
func resolveModule(path string, dirs []string) (string, error) {
	if filepath.IsAbs(path) {
		err := fileutils.Exists(path)
		return path, err
	}

	// Collect all errors to avoid suppressing important errors (e.g.,
	// permission errors).
	var multiErr error
	for _, d := range dirs {
		candidate := filepath.Join(d, path)
		err := fileutils.Exists(candidate)
		if err == nil {
			return candidate, nil
		}
		multiErr = multierror.Append(multiErr, err)
	}
	return "", multiErr
}
