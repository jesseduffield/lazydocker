//go:build !remote

package libpod

import (
	"net"
	"os"
	"path/filepath"
)

func openUnixSocket(path string) (*net.UnixConn, error) {
	// socket paths can be too long to fit into a sockaddr_un so we create a shorter symlink.
	tmpdir, err := os.MkdirTemp("", "podman")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpdir)
	tmpsockpath := filepath.Join(tmpdir, "sock")
	if err := os.Symlink(path, tmpsockpath); err != nil {
		return nil, err
	}
	return net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: tmpsockpath, Net: "unixpacket"})
}
