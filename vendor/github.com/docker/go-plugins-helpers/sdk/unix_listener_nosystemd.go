//go:build (linux || freebsd) && nosystemd

package sdk

import "net"

// FIXME(thaJeztah): this code was added in https://github.com/docker/go-plugins-helpers/commit/008703b825c10311af1840deeaf5f4769df7b59e, but is not used anywhere
func setupSocketActivation() (net.Listener, error) {
	return nil, nil
}
