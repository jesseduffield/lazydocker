//go:build linux || freebsd

package netavark

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/internal/util"
	"go.podman.io/common/libnetwork/types"
)

type netavarkOptions struct {
	types.NetworkOptions
	Networks map[string]*types.Network `json:"network_info"`
}

func (n *netavarkNetwork) execUpdate(networkName string, networkDNSServers []string) error {
	retErr := n.execNetavark([]string{"update", networkName, "--network-dns-servers", strings.Join(networkDNSServers, ",")}, false, nil, nil)
	return retErr
}

// Setup will setup the container network namespace. It returns
// a map of StatusBlocks, the key is the network name.
func (n *netavarkNetwork) Setup(namespacePath string, options types.SetupOptions) (_ map[string]types.StatusBlock, retErr error) {
	n.lock.Lock()
	defer n.lock.Unlock()
	err := n.loadNetworks()
	if err != nil {
		return nil, err
	}

	err = util.ValidateSetupOptions(n, namespacePath, options)
	if err != nil {
		return nil, err
	}

	// allocate IPs in the IPAM db
	err = n.allocIPs(&options.NetworkOptions)
	if err != nil {
		return nil, err
	}
	defer func() {
		// In case the setup failed for whatever reason podman will not start the
		// container so we must free the allocated ips again to not leak them.
		if retErr != nil {
			if err := n.deallocIPs(&options.NetworkOptions); err != nil {
				logrus.Error(err)
			}
		}
	}()

	netavarkOpts, needPlugin, err := n.convertNetOpts(options.NetworkOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to convert net opts: %w", err)
	}

	// Warn users if one or more networks have dns enabled
	// but aardvark-dns binary is not configured
	for _, network := range netavarkOpts.Networks {
		if network != nil && network.DNSEnabled && n.aardvarkBinary == "" {
			// this is not a fatal error we can still use container without dns
			logrus.Warnf("aardvark-dns binary not found, container dns will not be enabled")
			break
		}
	}

	// trace output to get the json
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		b, err := json.Marshal(&netavarkOpts)
		if err != nil {
			return nil, err
		}
		// show the full netavark command so we can easily reproduce errors from the cli
		logrus.Tracef("netavark command: printf '%s' | %s setup %s", string(b), n.netavarkBinary, namespacePath)
	}

	result := map[string]types.StatusBlock{}
	setup := func() error {
		return n.execNetavark([]string{"setup", namespacePath}, needPlugin, netavarkOpts, &result)
	}

	if n.rootlessNetns != nil {
		err = n.rootlessNetns.Setup(len(options.Networks), setup)
	} else {
		err = setup()
	}
	if err != nil {
		return nil, err
	}

	// make sure that the result makes sense
	if len(result) != len(options.Networks) {
		logrus.Errorf("unexpected netavark result: %v", result)
		return nil, fmt.Errorf("unexpected netavark result length, want (%d), got (%d) networks", len(options.Networks), len(result))
	}

	return result, err
}

// Teardown will teardown the container network namespace.
func (n *netavarkNetwork) Teardown(namespacePath string, options types.TeardownOptions) error {
	n.lock.Lock()
	defer n.lock.Unlock()
	err := n.loadNetworks()
	if err != nil {
		return err
	}

	// get IPs from the IPAM db
	err = n.getAssignedIPs(&options.NetworkOptions)
	if err != nil {
		// when there is an error getting the ips we should still continue
		// to call teardown for netavark to prevent leaking network interfaces
		logrus.Error(err)
	}

	netavarkOpts, needPlugin, err := n.convertNetOpts(options.NetworkOptions)
	if err != nil {
		return fmt.Errorf("failed to convert net opts: %w", err)
	}

	var retErr error
	teardown := func() error {
		return n.execNetavark([]string{"teardown", namespacePath}, needPlugin, netavarkOpts, nil)
	}

	if n.rootlessNetns != nil {
		retErr = n.rootlessNetns.Teardown(len(options.Networks), teardown)
	} else {
		retErr = teardown()
	}

	// when netavark returned an error we still free the used ips
	// otherwise we could end up in a state where block the ips forever
	err = n.deallocIPs(&netavarkOpts.NetworkOptions)
	if err != nil {
		if retErr != nil {
			logrus.Error(err)
		} else {
			retErr = err
		}
	}

	return retErr
}

func (n *netavarkNetwork) getCommonNetavarkOptions(needPlugin bool) []string {
	opts := []string{"--config", n.networkRunDir, "--rootless=" + strconv.FormatBool(n.networkRootless), "--aardvark-binary=" + n.aardvarkBinary}
	// to allow better backwards compat we only add the new netavark option when really needed
	if needPlugin {
		// Note this will require a netavark with https://github.com/containers/netavark/pull/509
		for _, dir := range n.pluginDirs {
			opts = append(opts, "--plugin-directory", dir)
		}
	}
	return opts
}

func (n *netavarkNetwork) convertNetOpts(opts types.NetworkOptions) (*netavarkOptions, bool, error) {
	netavarkOptions := netavarkOptions{
		NetworkOptions: opts,
		Networks:       make(map[string]*types.Network, len(opts.Networks)),
	}

	needsPlugin := false

	for network := range opts.Networks {
		net, err := n.getNetwork(network)
		if err != nil {
			return nil, false, err
		}
		netavarkOptions.Networks[network] = net
		if !slices.Contains(builtinDrivers, net.Driver) {
			needsPlugin = true
		}
	}
	return &netavarkOptions, needsPlugin, nil
}

func (n *netavarkNetwork) RunInRootlessNetns(toRun func() error) error {
	if n.rootlessNetns == nil {
		return types.ErrNotRootlessNetns
	}
	return n.rootlessNetns.Run(n.lock, toRun)
}

func (n *netavarkNetwork) RootlessNetnsInfo() (*types.RootlessNetnsInfo, error) {
	if n.rootlessNetns == nil {
		return nil, types.ErrNotRootlessNetns
	}
	return n.rootlessNetns.Info(), nil
}
