//go:build (linux || freebsd) && cni

package cni

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/containernetworking/cni/libcni"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	types040 "github.com/containernetworking/cni/pkg/types/040"
	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/internal/util"
	"go.podman.io/common/libnetwork/types"
)

// Setup will setup the container network namespace. It returns
// a map of StatusBlocks, the key is the network name.
func (n *cniNetwork) Setup(namespacePath string, options types.SetupOptions) (map[string]types.StatusBlock, error) {
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

	err = setupLoopback(namespacePath)
	if err != nil {
		return nil, fmt.Errorf("failed to set the loopback adapter up: %w", err)
	}

	results := make(map[string]types.StatusBlock, len(options.Networks))

	setup := func() error {
		var retErr error
		teardownOpts := options
		teardownOpts.Networks = map[string]types.PerNetworkOptions{}
		// make sure to teardown the already connected networks on error
		defer func() {
			if retErr != nil {
				if len(teardownOpts.Networks) > 0 {
					err := n.teardown(namespacePath, types.TeardownOptions(teardownOpts))
					if err != nil {
						logrus.Warn(err)
					}
				}
			}
		}()

		ports, err := convertSpecgenPortsToCNIPorts(options.PortMappings)
		if err != nil {
			return err
		}

		for name, netOpts := range options.Networks {
			network := n.networks[name]
			rt := getRuntimeConfig(namespacePath, options.ContainerName, options.ContainerID, name, ports, &netOpts)

			// If we have more than one static ip we need parse the ips via runtime config,
			// make sure to add the ips capability to the first plugin otherwise it doesn't get the ips
			if len(netOpts.StaticIPs) > 0 && !network.cniNet.Plugins[0].Network.Capabilities["ips"] {
				caps := map[string]any{
					"capabilities": map[string]bool{"ips": true},
				}
				network.cniNet.Plugins[0], retErr = libcni.InjectConf(network.cniNet.Plugins[0], caps)
				if retErr != nil {
					return retErr
				}
			}

			var res cnitypes.Result
			res, retErr = n.cniConf.AddNetworkList(context.Background(), network.cniNet, rt)
			// Add this network to teardown opts since it is now connected.
			// Also add this if an errors was returned since we want to call teardown on this regardless.
			teardownOpts.Networks[name] = netOpts
			if retErr != nil {
				return retErr
			}

			logrus.Debugf("cni result for container %s network %s: %v", options.ContainerID, name, res)
			var status types.StatusBlock
			status, retErr = CNIResultToStatus(res)
			if retErr != nil {
				return retErr
			}
			results[name] = status
		}
		return nil
	}

	if n.rootlessNetns != nil {
		err = n.rootlessNetns.Setup(len(options.Networks), setup)
	} else {
		err = setup()
	}
	return results, err
}

// CNIResultToStatus convert the cni result to status block
// nolint:golint,revive
func CNIResultToStatus(res cnitypes.Result) (types.StatusBlock, error) {
	result := types.StatusBlock{}
	cniResult, err := types040.GetResult(res)
	if err != nil {
		return result, err
	}
	nameservers := make([]net.IP, 0, len(cniResult.DNS.Nameservers))
	for _, nameserver := range cniResult.DNS.Nameservers {
		ip := net.ParseIP(nameserver)
		if ip == nil {
			return result, fmt.Errorf("failed to parse cni nameserver ip %s", nameserver)
		}
		nameservers = append(nameservers, ip)
	}
	result.DNSServerIPs = nameservers
	result.DNSSearchDomains = cniResult.DNS.Search

	interfaces := make(map[string]types.NetInterface)
	for intIndex, netInterface := range cniResult.Interfaces {
		// we are only interested about interfaces in the container namespace
		if netInterface.Sandbox == "" {
			continue
		}

		mac, err := net.ParseMAC(netInterface.Mac)
		if err != nil {
			return result, err
		}
		subnets := make([]types.NetAddress, 0, len(cniResult.IPs))
		for _, ip := range cniResult.IPs {
			if ip.Interface == nil {
				// we do no expect ips without an interface
				continue
			}
			if len(cniResult.Interfaces) <= *ip.Interface {
				return result, fmt.Errorf("invalid cni result, interface index %d out of range", *ip.Interface)
			}

			// when we have a ip for this interface add it to the subnets
			if *ip.Interface == intIndex {
				subnets = append(subnets, types.NetAddress{
					IPNet:   types.IPNet{IPNet: ip.Address},
					Gateway: ip.Gateway,
				})
			}
		}
		interfaces[netInterface.Name] = types.NetInterface{
			MacAddress: types.HardwareAddr(mac),
			Subnets:    subnets,
		}
	}
	result.Interfaces = interfaces
	return result, nil
}

func getRuntimeConfig(netns, conName, conID, networkName string, ports []cniPortMapEntry, opts *types.PerNetworkOptions) *libcni.RuntimeConf {
	rt := &libcni.RuntimeConf{
		ContainerID: conID,
		NetNS:       netns,
		IfName:      opts.InterfaceName,
		Args: [][2]string{
			{"IgnoreUnknown", "1"},
			// Do not set the K8S env vars, see https://github.com/containers/podman/issues/12083.
			// Only K8S_POD_NAME is used by dnsname to get the container name.
			{"K8S_POD_NAME", conName},
		},
		CapabilityArgs: map[string]any{},
	}

	// Propagate environment CNI_ARGS
	for kvpairs := range strings.SplitSeq(os.Getenv("CNI_ARGS"), ";") {
		if key, val, ok := strings.Cut(kvpairs, "="); ok {
			rt.Args = append(rt.Args, [2]string{key, val})
		}
	}

	// Add mac address to cni args
	if len(opts.StaticMAC) > 0 {
		rt.Args = append(rt.Args, [2]string{"MAC", opts.StaticMAC.String()})
	}

	if len(opts.StaticIPs) == 1 {
		// Add a single IP to the args field. CNI plugins < 1.0.0
		// do not support multiple ips via capability args.
		rt.Args = append(rt.Args, [2]string{"IP", opts.StaticIPs[0].String()})
	} else if len(opts.StaticIPs) > 1 {
		// Set the static ips in the capability args
		// to support more than one static ip per network.
		rt.CapabilityArgs["ips"] = opts.StaticIPs
	}

	// Set network aliases for the dnsname plugin.
	if len(opts.Aliases) > 0 {
		rt.CapabilityArgs["aliases"] = map[string][]string{
			networkName: opts.Aliases,
		}
	}

	// Set PortMappings in Capabilities
	if len(ports) > 0 {
		rt.CapabilityArgs["portMappings"] = ports
	}

	return rt
}

// Teardown will teardown the container network namespace.
func (n *cniNetwork) Teardown(namespacePath string, options types.TeardownOptions) error {
	n.lock.Lock()
	defer n.lock.Unlock()
	err := n.loadNetworks()
	if err != nil {
		return err
	}
	return n.teardown(namespacePath, options)
}

func (n *cniNetwork) teardown(namespacePath string, options types.TeardownOptions) error {
	// Note: An empty namespacePath is allowed because some plugins
	// still need teardown, for example ipam should remove used ip allocations.

	ports, err := convertSpecgenPortsToCNIPorts(options.PortMappings)
	if err != nil {
		return err
	}

	var multiErr *multierror.Error
	teardown := func() error {
		for name, netOpts := range options.Networks {
			rt := getRuntimeConfig(namespacePath, options.ContainerName, options.ContainerID, name, ports, &netOpts)

			cniConfList, newRt, err := getCachedNetworkConfig(n.cniConf, name, rt)
			if err == nil {
				rt = newRt
			} else {
				logrus.Warnf("Failed to load cached network config: %v, falling back to loading network %s from disk", err, name)
				network := n.networks[name]
				if network == nil {
					multiErr = multierror.Append(multiErr, fmt.Errorf("network %s: %w", name, types.ErrNoSuchNetwork))
					continue
				}
				cniConfList = network.cniNet
			}

			err = n.cniConf.DelNetworkList(context.Background(), cniConfList, rt)
			if err != nil {
				multiErr = multierror.Append(multiErr, err)
			}
		}
		return nil
	}

	if n.rootlessNetns != nil {
		err = n.rootlessNetns.Teardown(len(options.Networks), teardown)
	} else {
		err = teardown()
	}
	multiErr = multierror.Append(multiErr, err)

	return multiErr.ErrorOrNil()
}

func getCachedNetworkConfig(cniConf *libcni.CNIConfig, name string, rt *libcni.RuntimeConf) (*libcni.NetworkConfigList, *libcni.RuntimeConf, error) {
	cniConfList := &libcni.NetworkConfigList{
		Name: name,
	}
	confBytes, rt, err := cniConf.GetNetworkListCachedConfig(cniConfList, rt)
	if err != nil {
		return nil, nil, err
	} else if confBytes == nil {
		return nil, nil, fmt.Errorf("network %s not found in CNI cache", name)
	}

	cniConfList, err = libcni.ConfListFromBytes(confBytes)
	if err != nil {
		return nil, nil, err
	}
	return cniConfList, rt, nil
}

func (n *cniNetwork) RunInRootlessNetns(toRun func() error) error {
	if n.rootlessNetns == nil {
		return types.ErrNotRootlessNetns
	}
	return n.rootlessNetns.Run(n.lock, toRun)
}

func (n *cniNetwork) RootlessNetnsInfo() (*types.RootlessNetnsInfo, error) {
	if n.rootlessNetns == nil {
		return nil, types.ErrNotRootlessNetns
	}
	return n.rootlessNetns.Info(), nil
}
