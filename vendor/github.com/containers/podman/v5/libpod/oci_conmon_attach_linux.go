//go:build !remote

package libpod

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func openUnixSocket(path string) (*net.UnixConn, error) {
	fd, err := unix.Open(path, unix.O_PATH, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	return net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: fmt.Sprintf("/proc/self/fd/%d", fd), Net: "unixpacket"})
}
