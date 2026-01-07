//go:build (linux || freebsd) && cni

package cni

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containernetworking/cni/libcni"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/internal/rootlessnetns"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/version"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/lockfile"
	"go.podman.io/storage/pkg/unshare"
)

const defaultRootLockPath = "/run/lock/podman-cni.lock"

type cniNetwork struct {
	// cniConfigDir is directory where the cni config files are stored.
	cniConfigDir string
	// cniPluginDirs is a list of directories where cni should look for the plugins.
	cniPluginDirs []string

	cniConf *libcni.CNIConfig

	// defaultNetwork is the name for the default network.
	defaultNetwork string
	// defaultSubnet is the default subnet for the default network.
	defaultSubnet types.IPNet

	// defaultsubnetPools contains the subnets which must be used to allocate a free subnet by network create
	defaultsubnetPools []config.SubnetPool

	// isMachine describes whenever podman runs in a podman machine environment.
	isMachine bool

	// lock is a internal lock for critical operations
	lock *lockfile.LockFile

	// modTime is the timestamp when the config dir was modified
	modTime time.Time

	// networks is a map with loaded networks, the key is the network name
	networks map[string]*network

	// rootlessNetns is used for the rootless network setup/teardown
	rootlessNetns *rootlessnetns.Netns
}

type network struct {
	// filename is the full path to the cni config file on disk
	filename  string
	libpodNet *types.Network
	cniNet    *libcni.NetworkConfigList
}

type InitConfig struct {
	// CNIConfigDir is directory where the cni config files are stored.
	CNIConfigDir string
	// RunDir is a directory where temporary files can be stored.
	RunDir string

	// IsMachine describes whenever podman runs in a podman machine environment.
	IsMachine bool

	// Config containers.conf options
	Config *config.Config
}

// NewCNINetworkInterface creates the ContainerNetwork interface for the CNI backend.
// Note: The networks are not loaded from disk until a method is called.
func NewCNINetworkInterface(conf *InitConfig) (types.ContainerNetwork, error) {
	var netns *rootlessnetns.Netns
	var err error
	// Do not use unshare.IsRootless() here. We only care if we are running re-exec in the userns,
	// IsRootless() also returns true if we are root in a userns which is not what we care about and
	// causes issues as this slower more complicated rootless-netns logic should not be used as root.
	val, ok := os.LookupEnv(unshare.UsernsEnvName)
	useRootlessNetns := ok && val == "done"
	if useRootlessNetns {
		netns, err = rootlessnetns.New(conf.RunDir, rootlessnetns.CNI, conf.Config)
		if err != nil {
			return nil, err
		}
	}

	// root needs to use a globally unique lock because there is only one host netns
	lockPath := defaultRootLockPath
	if useRootlessNetns {
		lockPath = filepath.Join(conf.CNIConfigDir, "cni.lock")
	}

	lock, err := lockfile.GetLockFile(lockPath)
	if err != nil {
		return nil, err
	}

	defaultNetworkName := conf.Config.Network.DefaultNetwork
	if defaultNetworkName == "" {
		defaultNetworkName = types.DefaultNetworkName
	}

	defaultSubnet := conf.Config.Network.DefaultSubnet
	if defaultSubnet == "" {
		defaultSubnet = types.DefaultSubnet
	}
	defaultNet, err := types.ParseCIDR(defaultSubnet)
	if err != nil {
		return nil, fmt.Errorf("failed to parse default subnet: %w", err)
	}

	defaultSubnetPools := conf.Config.Network.DefaultSubnetPools
	if defaultSubnetPools == nil {
		defaultSubnetPools = config.DefaultSubnetPools
	}

	cni := libcni.NewCNIConfig(conf.Config.Network.CNIPluginDirs.Values, &cniExec{})
	n := &cniNetwork{
		cniConfigDir:       conf.CNIConfigDir,
		cniPluginDirs:      conf.Config.Network.CNIPluginDirs.Get(),
		cniConf:            cni,
		defaultNetwork:     defaultNetworkName,
		defaultSubnet:      defaultNet,
		defaultsubnetPools: defaultSubnetPools,
		isMachine:          conf.IsMachine,
		lock:               lock,
		rootlessNetns:      netns,
	}

	return n, nil
}

// Drivers will return the list of supported network drivers
// for this interface.
func (n *cniNetwork) Drivers() []string {
	return []string{types.BridgeNetworkDriver, types.MacVLANNetworkDriver, types.IPVLANNetworkDriver}
}

// DefaultNetworkName will return the default cni network name.
func (n *cniNetwork) DefaultNetworkName() string {
	return n.defaultNetwork
}

func (n *cniNetwork) loadNetworks() error {
	// check the mod time of the config dir
	var modTime time.Time
	f, err := os.Stat(n.cniConfigDir)
	// ignore error if the file does not exists
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		modTime = f.ModTime()
	}

	// skip loading networks if they are already loaded and
	// if the config dir was not modified since the last call
	if n.networks != nil && modTime.Equal(n.modTime) {
		return nil
	}
	// make sure the remove all networks before we reload them
	n.networks = nil
	n.modTime = modTime

	// FIXME: do we have to support other file types as well, e.g. .conf?
	files, err := libcni.ConfFiles(n.cniConfigDir, []string{".conflist"})
	if err != nil {
		return err
	}
	networks := make(map[string]*network, len(files))
	for _, file := range files {
		conf, err := libcni.ConfListFromFile(file)
		if err != nil {
			// do not log ENOENT errors
			if !errors.Is(err, os.ErrNotExist) {
				logrus.Warnf("Error loading CNI config file %s: %v", file, err)
			}
			continue
		}

		if !types.NameRegex.MatchString(conf.Name) {
			logrus.Warnf("CNI config list %s has invalid name, skipping: %v", file, types.ErrInvalidName)
			continue
		}

		// podman < v4.0 used the podman-machine cni plugin for podman machine port forwarding
		// since this is now build into podman we no longer use the plugin
		// old configs may still contain it so we just remove it here
		if n.isMachine {
			conf = removeMachinePlugin(conf)
		}

		if _, err := n.cniConf.ValidateNetworkList(context.Background(), conf); err != nil {
			logrus.Warnf("Error validating CNI config file %s: %v", file, err)
			continue
		}

		if val, ok := networks[conf.Name]; ok {
			logrus.Warnf("CNI config list %s has the same network name as %s, skipping", file, val.filename)
			continue
		}

		net, err := createNetworkFromCNIConfigList(conf, file)
		if err != nil {
			// ignore ENOENT as the config has been removed in the meantime so we can just ignore this case
			if !errors.Is(err, fs.ErrNotExist) {
				logrus.Errorf("CNI config list %s could not be converted to a libpod config, skipping: %v", file, err)
			}
			continue
		}
		logrus.Debugf("Successfully loaded network %s: %v", net.Name, net)
		networkInfo := network{
			filename:  file,
			cniNet:    conf,
			libpodNet: net,
		}
		networks[net.Name] = &networkInfo
	}

	// create the default network in memory if it did not exists on disk
	if networks[n.defaultNetwork] == nil {
		networkInfo, err := n.createDefaultNetwork()
		if err != nil {
			return fmt.Errorf("failed to create default network %s: %w", n.defaultNetwork, err)
		}
		networks[n.defaultNetwork] = networkInfo
	}

	logrus.Debugf("Successfully loaded %d networks", len(networks))
	n.networks = networks
	return nil
}

func (n *cniNetwork) createDefaultNetwork() (*network, error) {
	net := types.Network{
		Name:             n.defaultNetwork,
		NetworkInterface: "cni-podman0",
		Driver:           types.BridgeNetworkDriver,
		Subnets: []types.Subnet{
			{Subnet: n.defaultSubnet},
		},
	}
	return n.networkCreate(&net, true)
}

// getNetwork will lookup a network by name or ID. It returns an
// error when no network was found or when more than one network
// with the given (partial) ID exists.
// getNetwork will read from the networks map, therefore the caller
// must ensure that n.lock is locked before using it.
func (n *cniNetwork) getNetwork(nameOrID string) (*network, error) {
	// fast path check the map key, this will only work for names
	if val, ok := n.networks[nameOrID]; ok {
		return val, nil
	}
	// If there was no match we might got a full or partial ID.
	var net *network
	for _, val := range n.networks {
		// This should not happen because we already looked up the map by name but check anyway.
		if val.libpodNet.Name == nameOrID {
			return val, nil
		}

		if strings.HasPrefix(val.libpodNet.ID, nameOrID) {
			if net != nil {
				return nil, fmt.Errorf("more than one result for network ID %s", nameOrID)
			}
			net = val
		}
	}
	if net != nil {
		return net, nil
	}
	return nil, fmt.Errorf("unable to find network with name or ID %s: %w", nameOrID, types.ErrNoSuchNetwork)
}

// getNetworkIDFromName creates a network ID from the name. It is just the
// sha256 hash so it is not safe but it should be safe enough for our use case.
func getNetworkIDFromName(name string) string {
	hash := sha256.Sum256([]byte(name))
	return hex.EncodeToString(hash[:])
}

// Implement the NetUtil interface for easy code sharing with other network interfaces.

// ForEach call the given function for each network.
func (n *cniNetwork) ForEach(run func(types.Network)) {
	for _, val := range n.networks {
		run(*val.libpodNet)
	}
}

// Len return the number of networks.
func (n *cniNetwork) Len() int {
	return len(n.networks)
}

// DefaultInterfaceName return the default cni bridge name, must be suffixed with a number.
func (n *cniNetwork) DefaultInterfaceName() string {
	return cniDeviceName
}

// NetworkInfo return the network information about binary path,
// package version and program version.
func (n *cniNetwork) NetworkInfo() types.NetworkInfo {
	path := ""
	packageVersion := ""
	for _, p := range n.cniPluginDirs {
		ver := version.Package(p)
		if ver != version.UnknownPackage {
			path = p
			packageVersion = ver
			break
		}
	}

	info := types.NetworkInfo{
		Backend: types.CNI,
		Package: packageVersion,
		Path:    path,
	}

	dnsPath := filepath.Join(path, "dnsname")
	dnsPackage := version.Package(dnsPath)
	dnsProgram, err := version.ProgramDnsname(dnsPath)
	if err != nil {
		logrus.Infof("Failed to get the dnsname plugin version: %v", err)
	}
	if err := fileutils.Exists(dnsPath); err == nil {
		info.DNS = types.DNSNetworkInfo{
			Path:    dnsPath,
			Package: dnsPackage,
			Version: dnsProgram,
		}
	}

	return info
}

func (n *cniNetwork) Network(nameOrID string) (*types.Network, error) {
	network, err := n.getNetwork(nameOrID)
	if err != nil {
		return nil, err
	}
	return network.libpodNet, err
}
