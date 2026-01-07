//go:build !linux && !freebsd

package sdk

import (
	"errors"
	"net"
)

func newUnixListener(pluginName string, gid int) (net.Listener, string, error) {
	return nil, "", errors.New("unix socket creation is only supported on Linux and FreeBSD")
}
