package sdk

import (
	"fmt"
	"os"
	"path/filepath"
)

type protocol string

const (
	protoTCP       protocol = "tcp"
	protoNamedPipe protocol = "npipe"
)

// PluginSpecDir returns plugin spec dir in relation to daemon root directory.
func PluginSpecDir(daemonRoot string) string {
	return ([]string{filepath.Join(daemonRoot, "plugins")})[0]
}

// WindowsDefaultDaemonRootDir returns default data directory of docker daemon on Windows.
func WindowsDefaultDaemonRootDir() string {
	return filepath.Join(os.Getenv("programdata"), "docker")
}

func createPluginSpecDirWindows(name, address, daemonRoot string) (string, error) {
	_, err := os.Stat(daemonRoot)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("Deamon root directory must already exist: %s", err)
	}

	pluginSpecDir := PluginSpecDir(daemonRoot)

	if err := windowsCreateDirectoryWithACL(pluginSpecDir); err != nil {
		return "", err
	}
	return pluginSpecDir, nil
}

func createPluginSpecDirUnix(name, address string) (string, error) {
	pluginSpecDir := PluginSpecDir("/etc/docker")
	if err := os.MkdirAll(pluginSpecDir, 0755); err != nil {
		return "", err
	}
	return pluginSpecDir, nil
}

func writeSpecFile(name, address, pluginSpecDir string, proto protocol) (string, error) {
	specFileDir := filepath.Join(pluginSpecDir, name+".spec")

	url := string(proto) + "://" + address
	if err := os.WriteFile(specFileDir, []byte(url), 0644); err != nil {
		return "", err
	}

	return specFileDir, nil
}
