package systemd

import (
	"os"
	"strconv"
)

// SocketActivated determine if podman is running under the socket activation protocol
// Criteria is based on the expectations of "github.com/coreos/go-systemd/v22/activation"
func SocketActivated() bool {
	pid, found := os.LookupEnv("LISTEN_PID")
	if !found {
		return false
	}
	p, err := strconv.Atoi(pid)
	if err != nil || p != os.Getpid() {
		return false
	}

	fds, found := os.LookupEnv("LISTEN_FDS")
	if !found {
		return false
	}
	nfds, err := strconv.Atoi(fds)
	if err != nil || nfds == 0 {
		return false
	}
	return true
}
