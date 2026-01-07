//go:build darwin

package buildah

import (
	"errors"

	"github.com/containers/buildah/define"
	"github.com/opencontainers/runtime-spec/specs-go"
	nettypes "go.podman.io/common/libnetwork/types"
	"go.podman.io/storage"
)

// ContainerDevices is an alias for a slice of github.com/opencontainers/runc/libcontainer/configs.Device structures.
type ContainerDevices define.ContainerDevices

func setChildProcess() error {
	return errors.New("function not supported on non-linux systems")
}

func runUsingRuntimeMain() {}

func (b *Builder) Run(command []string, options RunOptions) error {
	return errors.New("function not supported on non-linux systems")
}

func DefaultNamespaceOptions() (NamespaceOptions, error) {
	options := NamespaceOptions{
		{Name: string(specs.CgroupNamespace), Host: false},
		{Name: string(specs.IPCNamespace), Host: false},
		{Name: string(specs.MountNamespace), Host: false},
		{Name: string(specs.NetworkNamespace), Host: false},
		{Name: string(specs.PIDNamespace), Host: false},
		{Name: string(specs.UserNamespace), Host: false},
		{Name: string(specs.UTSNamespace), Host: false},
	}
	return options, nil
}

// getNetworkInterface creates the network interface
func getNetworkInterface(store storage.Store, cniConfDir, cniPluginPath string) (nettypes.ContainerNetwork, error) {
	return nil, nil
}
