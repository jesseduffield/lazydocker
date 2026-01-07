//go:build linux

// Rootlessport Config type for use in podman/cmd/rootlessport.
package rootlessport

import (
	"go.podman.io/common/libnetwork/types"
)

const (
	// BinaryName is the binary name for the parent process.
	BinaryName = "rootlessport"
)

// Config needs to be provided to the process via stdin as a JSON string.
// stdin needs to be closed after the message has been written.
type Config struct {
	Mappings    []types.PortMapping
	NetNSPath   string
	ExitFD      int
	ReadyFD     int
	TmpDir      string
	ChildIP     string
	ContainerID string
	RootlessCNI bool
}
