//go:build remote

package config

// isDirectory tests whether the given path exists and is a directory. It
// follows symlinks.
func isDirectory(path string) error {
	return nil
}

func isRemote() bool {
	return true
}

func (c *EngineConfig) validatePaths() error {
	return nil
}

func (c *EngineConfig) validateRuntimeNames() error {
	return nil
}

func (c *ContainersConfig) validateDevices() error {
	return nil
}

func (c *ContainersConfig) validateInterfaceName() error {
	return nil
}

func (c *ContainersConfig) validateUlimits() error {
	return nil
}

func (c *ContainersConfig) validateTZ() error {
	return nil
}

func (c *ContainersConfig) validateUmask() error {
	return nil
}

func (c *ContainersConfig) validateLogPath() error {
	return nil
}
