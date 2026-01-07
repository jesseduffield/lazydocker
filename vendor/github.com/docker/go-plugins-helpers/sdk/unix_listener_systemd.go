//go:build (linux || freebsd) && !nosystemd

package sdk

import (
	"fmt"
	"net"
	"os"

	"github.com/coreos/go-systemd/activation"
)

// isRunningSystemd checks whether the host was booted with systemd as its init
// system. This functions similarly to systemd's `sd_booted(3)`: internally, it
// checks whether /run/systemd/system/ exists and is a directory.
// http://www.freedesktop.org/software/systemd/man/sd_booted.html
//
// Copied from github.com/coreos/go-systemd/util.IsRunningSystemd
func isRunningSystemd() bool {
	fi, err := os.Lstat("/run/systemd/system")
	if err != nil {
		return false
	}
	return fi.IsDir()
}

// FIXME(thaJeztah): this code was added in https://github.com/docker/go-plugins-helpers/commit/008703b825c10311af1840deeaf5f4769df7b59e, but is not used anywhere
func setupSocketActivation() (net.Listener, error) {
	if !isRunningSystemd() {
		return nil, nil
	}
	listenFds := activation.Files(false)
	if len(listenFds) > 1 {
		return nil, fmt.Errorf("expected only one socket from systemd, got %d", len(listenFds))
	}
	var listener net.Listener
	if len(listenFds) == 1 {
		l, err := net.FileListener(listenFds[0])
		if err != nil {
			return nil, err
		}
		listener = l
	}
	return listener, nil
}
