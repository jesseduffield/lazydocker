//go:build (linux || freebsd) && cni

package network

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.podman.io/common/libnetwork/cni"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/machine"
	"go.podman.io/storage"
	"go.podman.io/storage/pkg/homedir"
	"go.podman.io/storage/pkg/unshare"
)

const (
	// cniConfigDirRootless is the directory in XDG_CONFIG_HOME for cni plugins.
	cniConfigDirRootless = "cni/net.d/"

	cniSupported = true
)

func getCniInterface(conf *config.Config) (types.ContainerNetwork, error) {
	confDir := conf.Network.NetworkConfigDir
	if confDir == "" {
		var err error
		confDir, err = getDefaultCNIConfigDir()
		if err != nil {
			return nil, err
		}
	}
	return cni.NewCNINetworkInterface(&cni.InitConfig{
		Config:       conf,
		CNIConfigDir: confDir,
		RunDir:       conf.Engine.TmpDir,
		IsMachine:    machine.IsGvProxyBased(),
	})
}

func getDefaultCNIConfigDir() (string, error) {
	if !unshare.IsRootless() {
		return cniConfigDir, nil
	}

	configHome, err := homedir.GetConfigHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(configHome, cniConfigDirRootless), nil
}

func networkBackendFromStore(store storage.Store, conf *config.Config) (backend types.NetworkBackend, err error) {
	_, err = conf.FindHelperBinary("netavark", false)
	if err != nil {
		// if we cannot find netavark use CNI
		return types.CNI, nil
	}

	// If there are any containers then return CNI
	cons, err := store.Containers()
	if err != nil {
		return "", err
	}
	if len(cons) != 0 {
		return types.CNI, nil
	}

	// If there are any non ReadOnly images then return CNI
	imgs, err := store.Images()
	if err != nil {
		return "", err
	}
	for _, i := range imgs {
		if !i.ReadOnly {
			return types.CNI, nil
		}
	}

	// If there are CNI Networks then return CNI
	cniInterface, err := getCniInterface(conf)
	if err == nil {
		nets, err := cniInterface.NetworkList()
		// there is always a default network so check > 1
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		if len(nets) > 1 {
			// we do not have a fresh system so use CNI
			return types.CNI, nil
		}
	}
	return types.Netavark, nil
}

func backendFromType(backend types.NetworkBackend, store storage.Store, conf *config.Config, syslog bool) (types.NetworkBackend, types.ContainerNetwork, error) {
	switch backend {
	case types.Netavark:
		netInt, err := netavarkBackendFromConf(store, conf, syslog)
		if err != nil {
			return "", nil, err
		}
		return types.Netavark, netInt, err
	case types.CNI:
		netInt, err := getCniInterface(conf)
		if err != nil {
			return "", nil, err
		}
		return types.CNI, netInt, err

	default:
		return "", nil, fmt.Errorf("unsupported network backend %q, check network_backend in containers.conf", backend)
	}
}
