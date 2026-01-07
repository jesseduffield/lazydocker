package machine

import (
	"os"
	"strings"
	"sync"
)

type Marker struct {
	Enabled bool
	Type    string
}

const (
	markerFile = "/etc/containers/podman-machine"
	Wsl        = "wsl"
	Qemu       = "qemu"
	AppleHV    = "applehv"
	HyperV     = "hyperv"
)

var (
	markerSync sync.Once
	marker     *Marker
)

func loadMachineMarker(file string) {
	var kind string
	enabled := false

	if content, err := os.ReadFile(file); err == nil {
		enabled = true
		kind = strings.TrimSpace(string(content))
	}

	marker = &Marker{enabled, kind}
}

func IsPodmanMachine() bool {
	return GetMachineMarker().Enabled
}

func HostType() string {
	return GetMachineMarker().Type
}

func IsGvProxyBased() bool {
	return IsPodmanMachine() && HostType() != Wsl
}

func GetMachineMarker() *Marker {
	markerSync.Do(func() {
		loadMachineMarker(markerFile)
	})
	return marker
}
