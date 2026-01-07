//go:build linux || freebsd

package netavark

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/internal/rootlessnetns"
	"go.podman.io/common/libnetwork/internal/util"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/version"
	"go.podman.io/storage/pkg/lockfile"
	"go.podman.io/storage/pkg/unshare"
)

type netavarkNetwork struct {
	// networkConfigDir is directory where the network config files are stored.
	networkConfigDir string

	// networkRunDir is where temporary files are stored, i.e.the ipam db, aardvark config etc
	networkRunDir string

	// tells netavark whether this is rootless mode or rootful, "true" or "false"
	networkRootless bool

	// netavarkBinary is the path to the netavark binary.
	netavarkBinary string
	// aardvarkBinary is the path to the aardvark binary.
	aardvarkBinary string

	// firewallDriver sets the firewall driver to use
	firewallDriver string

	// defaultNetwork is the name for the default network.
	defaultNetwork string
	// defaultSubnet is the default subnet for the default network.
	defaultSubnet types.IPNet

	// defaultsubnetPools contains the subnets which must be used to allocate a free subnet by network create
	defaultsubnetPools []config.SubnetPool

	// dnsBindPort is set the port to pass to netavark for aardvark
	dnsBindPort uint16

	// pluginDirs list of directories were netavark plugins are located
	pluginDirs []string

	// ipamDBPath is the path to the ip allocation bolt db
	ipamDBPath string

	// syslog describes whenever the netavark debug output should be log to the syslog as well.
	// This will use logrus to do so, make sure logrus is set up to log to the syslog.
	syslog bool

	// lock is a internal lock for critical operations
	lock *lockfile.LockFile

	// modTime is the timestamp when the config dir was modified
	modTime time.Time

	// networks is a map with loaded networks, the key is the network name
	networks map[string]*types.Network

	// rootlessNetns is used for the rootless network setup/teardown
	rootlessNetns *rootlessnetns.Netns
}

type InitConfig struct {
	// NetworkConfigDir is directory where the network config files are stored.
	NetworkConfigDir string

	// NetavarkBinary is the path to the netavark binary.
	NetavarkBinary string
	// AardvarkBinary is the path to the aardvark binary.
	AardvarkBinary string

	// NetworkRunDir is where temporary files are stored, i.e.the ipam db, aardvark config
	NetworkRunDir string

	// Syslog describes whenever the netavark debug output should be log to the syslog as well.
	// This will use logrus to do so, make sure logrus is set up to log to the syslog.
	Syslog bool

	// Config containers.conf options
	Config *config.Config
}

// NewNetworkInterface creates the ContainerNetwork interface for the netavark backend.
// Note: The networks are not loaded from disk until a method is called.
func NewNetworkInterface(conf *InitConfig) (types.ContainerNetwork, error) {
	var netns *rootlessnetns.Netns
	var err error
	// Do not use unshare.IsRootless() here. We only care if we are running re-exec in the userns,
	// IsRootless() also returns true if we are root in a userns which is not what we care about and
	// causes issues as this slower more complicated rootless-netns logic should not be used as root.
	val, ok := os.LookupEnv(unshare.UsernsEnvName)
	useRootlessNetns := ok && val == "done"
	if useRootlessNetns {
		netns, err = rootlessnetns.New(conf.NetworkRunDir, rootlessnetns.Netavark, conf.Config)
		if err != nil {
			return nil, err
		}
	}

	// root needs to use a globally unique lock because there is only one host netns
	lockPath := defaultRootLockPath
	if useRootlessNetns {
		lockPath = filepath.Join(conf.NetworkConfigDir, "netavark.lock")
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

	if err := os.MkdirAll(conf.NetworkRunDir, 0o755); err != nil {
		return nil, err
	}

	defaultSubnetPools := conf.Config.Network.DefaultSubnetPools
	if defaultSubnetPools == nil {
		defaultSubnetPools = config.DefaultSubnetPools
	}

	n := &netavarkNetwork{
		networkConfigDir:   conf.NetworkConfigDir,
		networkRunDir:      conf.NetworkRunDir,
		netavarkBinary:     conf.NetavarkBinary,
		aardvarkBinary:     conf.AardvarkBinary,
		networkRootless:    useRootlessNetns,
		ipamDBPath:         filepath.Join(conf.NetworkRunDir, "ipam.db"),
		firewallDriver:     conf.Config.Network.FirewallDriver,
		defaultNetwork:     defaultNetworkName,
		defaultSubnet:      defaultNet,
		defaultsubnetPools: defaultSubnetPools,
		dnsBindPort:        conf.Config.Network.DNSBindPort,
		pluginDirs:         conf.Config.Network.NetavarkPluginDirs.Get(),
		lock:               lock,
		syslog:             conf.Syslog,
		rootlessNetns:      netns,
	}

	return n, nil
}

var builtinDrivers = []string{types.BridgeNetworkDriver, types.MacVLANNetworkDriver, types.IPVLANNetworkDriver}

// Drivers will return the list of supported network drivers
// for this interface.
func (n *netavarkNetwork) Drivers() []string {
	paths := getAllPlugins(n.pluginDirs)
	return append(builtinDrivers, paths...)
}

// DefaultNetworkName will return the default netavark network name.
func (n *netavarkNetwork) DefaultNetworkName() string {
	return n.defaultNetwork
}

func (n *netavarkNetwork) loadNetworks() error {
	// check the mod time of the config dir
	f, err := os.Stat(n.networkConfigDir)
	if err != nil {
		// the directory may not exists which is fine. It will be created on the first network create
		if errors.Is(err, os.ErrNotExist) {
			// networks are already loaded
			if n.networks != nil {
				return nil
			}
			networks := make(map[string]*types.Network, 1)
			networkInfo, err := n.createDefaultNetwork()
			if err != nil {
				return fmt.Errorf("failed to create default network %s: %w", n.defaultNetwork, err)
			}
			networks[n.defaultNetwork] = networkInfo
			n.networks = networks
			return nil
		}
		return err
	}
	modTime := f.ModTime()

	// skip loading networks if they are already loaded and
	// if the config dir was not modified since the last call
	if n.networks != nil && modTime.Equal(n.modTime) {
		return nil
	}
	// make sure the remove all networks before we reload them
	n.networks = nil
	n.modTime = modTime

	files, err := os.ReadDir(n.networkConfigDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	networks := make(map[string]*types.Network, len(files))
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if filepath.Ext(f.Name()) != ".json" {
			continue
		}

		path := filepath.Join(n.networkConfigDir, f.Name())
		file, err := os.Open(path)
		if err != nil {
			// do not log ENOENT errors
			if !errors.Is(err, os.ErrNotExist) {
				logrus.Warnf("Error loading network config file %q: %v", path, err)
			}
			continue
		}
		network := new(types.Network)
		err = json.NewDecoder(file).Decode(network)
		if err != nil {
			logrus.Warnf("Error reading network config file %q: %v", path, err)
			continue
		}

		// check that the filename matches the network name
		if network.Name+".json" != f.Name() {
			logrus.Warnf("Network config name %q does not match file name %q, skipping", network.Name, f.Name())
			continue
		}

		if !types.NameRegex.MatchString(network.Name) {
			logrus.Warnf("Network config %q has invalid name: %q, skipping: %v", path, network.Name, types.ErrInvalidName)
			continue
		}

		err = parseNetwork(network)
		if err != nil {
			logrus.Warnf("Network config %q could not be parsed, skipping: %v", path, err)
			continue
		}

		logrus.Debugf("Successfully loaded network %s: %v", network.Name, network)
		networks[network.Name] = network
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

func parseNetwork(network *types.Network) error {
	if network.Labels == nil {
		network.Labels = map[string]string{}
	}
	if network.Options == nil {
		network.Options = map[string]string{}
	}
	if network.IPAMOptions == nil {
		network.IPAMOptions = map[string]string{}
	}

	if len(network.ID) != 64 {
		return fmt.Errorf("invalid network ID %q", network.ID)
	}

	// add gateway when not internal or dns enabled
	addGateway := !network.Internal || network.DNSEnabled
	return util.ValidateSubnets(network, addGateway, nil)
}

func (n *netavarkNetwork) createDefaultNetwork() (*types.Network, error) {
	net := types.Network{
		Name:             n.defaultNetwork,
		NetworkInterface: defaultBridgeName + "0",
		// Important do not change this ID
		ID:     "2f259bab93aaaaa2542ba43ef33eb990d0999ee1b9924b557b7be53c0b7a1bb9",
		Driver: types.BridgeNetworkDriver,
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
func (n *netavarkNetwork) getNetwork(nameOrID string) (*types.Network, error) {
	// fast path check the map key, this will only work for names
	if val, ok := n.networks[nameOrID]; ok {
		return val, nil
	}
	// If there was no match we might got a full or partial ID.
	var net *types.Network
	for _, val := range n.networks {
		// This should not happen because we already looked up the map by name but check anyway.
		if val.Name == nameOrID {
			return val, nil
		}

		if strings.HasPrefix(val.ID, nameOrID) {
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

// Implement the NetUtil interface for easy code sharing with other network interfaces.

// ForEach call the given function for each network.
func (n *netavarkNetwork) ForEach(run func(types.Network)) {
	for _, val := range n.networks {
		run(*val)
	}
}

// Len return the number of networks.
func (n *netavarkNetwork) Len() int {
	return len(n.networks)
}

// DefaultInterfaceName return the default cni bridge name, must be suffixed with a number.
func (n *netavarkNetwork) DefaultInterfaceName() string {
	return defaultBridgeName
}

// NetworkInfo return the network information about binary path,
// package version and program version.
func (n *netavarkNetwork) NetworkInfo() types.NetworkInfo {
	path := n.netavarkBinary
	packageVersion := version.Package(path)
	programVersion, err := version.Program(path)
	if err != nil {
		logrus.Infof("Failed to get the netavark version: %v", err)
	}
	info := types.NetworkInfo{
		Backend: types.Netavark,
		Version: programVersion,
		Package: packageVersion,
		Path:    path,
	}

	dnsPath := n.aardvarkBinary
	dnsPackage := version.Package(dnsPath)
	dnsProgram, err := version.Program(dnsPath)
	if err != nil {
		logrus.Infof("Failed to get the aardvark version: %v", err)
	}
	info.DNS = types.DNSNetworkInfo{
		Package: dnsPackage,
		Path:    dnsPath,
		Version: dnsProgram,
	}

	return info
}

func (n *netavarkNetwork) Network(nameOrID string) (*types.Network, error) {
	network, err := n.getNetwork(nameOrID)
	if err != nil {
		return nil, err
	}
	return network, nil
}
