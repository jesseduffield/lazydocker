package sdk

import (
	"crypto/tls"
	"net"
	"runtime"

	"github.com/docker/go-connections/sockets"
)

func newTCPListener(address, pluginName, daemonDir string, tlsConfig *tls.Config) (net.Listener, string, error) {
	listener, err := sockets.NewTCPSocket(address, tlsConfig)
	if err != nil {
		return nil, "", err
	}

	addr := listener.Addr().String()

	var specDir string
	if runtime.GOOS == "windows" {
		specDir, err = createPluginSpecDirWindows(pluginName, addr, daemonDir)
	} else {
		specDir, err = createPluginSpecDirUnix(pluginName, addr)
	}
	if err != nil {
		return nil, "", err
	}

	specFile, err := writeSpecFile(pluginName, addr, specDir, protoTCP)
	if err != nil {
		return nil, "", err
	}
	return listener, specFile, nil
}
