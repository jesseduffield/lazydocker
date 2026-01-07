//go:build !windows

package sdk

import (
	"errors"
	"net"
)

func newWindowsListener(address, pluginName, daemonRoot string, pipeConfig *WindowsPipeConfig) (net.Listener, string, error) {
	return nil, "", errors.New("named pipe creation is only supported on Windows")
}

func windowsCreateDirectoryWithACL(name string) error {
	return nil
}
