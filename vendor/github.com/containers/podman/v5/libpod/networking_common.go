//go:build !remote && (linux || freebsd)

package libpod

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	"github.com/containers/podman/v5/pkg/namespaces"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/etchosts"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/machine"
	"go.podman.io/storage/pkg/lockfile"
)

// bindPorts ports to keep them open via conmon so no other process can use them and we can check if they are in use.
// Note in all cases it is important that we bind before setting up the network to avoid issues were we add firewall
// rules before we even "own" the port.
func (c *Container) bindPorts() ([]*os.File, error) {
	if !c.runtime.config.Engine.EnablePortReservation || rootless.IsRootless() || !c.config.NetMode.IsBridge() {
		return nil, nil
	}
	return bindPorts(c.convertPortMappings())
}

// convertPortMappings will remove the HostIP part from the ports when running inside podman machine.
// This is needed because a HostIP of 127.0.0.1 would now allow the gvproxy forwarder to reach to open ports.
// For machine the HostIP must only be used by gvproxy and never in the VM.
func (c *Container) convertPortMappings() []types.PortMapping {
	if !machine.IsGvProxyBased() || len(c.config.PortMappings) == 0 {
		return c.config.PortMappings
	}
	// if we run in a machine VM we have to ignore the host IP part
	newPorts := make([]types.PortMapping, 0, len(c.config.PortMappings))
	for _, port := range c.config.PortMappings {
		port.HostIP = ""
		newPorts = append(newPorts, port)
	}
	return newPorts
}

func (c *Container) getNetworkOptions(networkOpts map[string]types.PerNetworkOptions) types.NetworkOptions {
	nameservers := make([]string, 0, len(c.runtime.config.Containers.DNSServers.Get())+len(c.config.DNSServer))
	nameservers = append(nameservers, c.runtime.config.Containers.DNSServers.Get()...)
	for _, ip := range c.config.DNSServer {
		nameservers = append(nameservers, ip.String())
	}
	opts := types.NetworkOptions{
		ContainerID:       c.config.ID,
		ContainerName:     getNetworkPodName(c),
		DNSServers:        nameservers,
		ContainerHostname: c.NetworkHostname(),
	}
	opts.PortMappings = c.convertPortMappings()

	// If the container requested special network options use this instead of the config.
	// This is the case for container restore or network reload.
	if c.perNetworkOpts != nil {
		opts.Networks = c.perNetworkOpts
	} else {
		opts.Networks = networkOpts
	}
	return opts
}

// setUpNetwork will set up the networks, on error it will also tear down the cni
// networks. If rootless it will join/create the rootless network namespace.
func (r *Runtime) setUpNetwork(ns string, opts types.NetworkOptions) (map[string]types.StatusBlock, error) {
	return r.network.Setup(ns, types.SetupOptions{NetworkOptions: opts})
}

// getNetworkPodName return the pod name (hostname) used by dns backend.
// If we are in the pod network namespace use the pod name otherwise the container name
func getNetworkPodName(c *Container) string {
	if c.config.NetMode.IsPod() || c.IsInfra() {
		pod, err := c.runtime.state.Pod(c.PodID())
		if err == nil {
			return pod.Name()
		}
	}
	return c.Name()
}

// Tear down a container's network configuration and joins the
// rootless net ns as rootless user
func (r *Runtime) teardownNetworkBackend(ns string, opts types.NetworkOptions) error {
	return r.network.Teardown(ns, types.TeardownOptions{NetworkOptions: opts})
}

// Tear down a container's network backend configuration, but do not tear down the
// namespace itself.
func (r *Runtime) teardownNetwork(ctr *Container) error {
	if ctr.state.NetNS == "" {
		// The container has no network namespace, we're set
		return nil
	}

	logrus.Debugf("Tearing down network namespace at %s for container %s", ctr.state.NetNS, ctr.ID())

	networks, err := ctr.networks()
	if err != nil {
		return err
	}

	if !ctr.config.NetMode.IsSlirp4netns() &&
		!ctr.config.NetMode.IsPasta() && len(networks) > 0 {
		netOpts := ctr.getNetworkOptions(networks)
		return r.teardownNetworkBackend(ctr.state.NetNS, netOpts)
	}
	return nil
}

// isBridgeNetMode checks if the given network mode is bridge.
// It returns nil when it is set to bridge and an error otherwise.
func isBridgeNetMode(n namespaces.NetworkMode) error {
	if !n.IsBridge() {
		return fmt.Errorf("%q is not supported: %w", n, define.ErrNetworkModeInvalid)
	}
	return nil
}

// Reload only works with containers with a configured network.
// It will tear down, and then reconfigure, the network of the container.
// This is mainly used when a reload of firewall rules wipes out existing
// firewall configuration.
// Efforts will be made to preserve MAC and IP addresses.
// Only works on containers with bridge networking at present, though in the future we could
// extend this to stop + restart slirp4netns
func (r *Runtime) reloadContainerNetwork(ctr *Container) (map[string]types.StatusBlock, error) {
	if ctr.state.NetNS == "" {
		return nil, fmt.Errorf("container %s network is not configured, refusing to reload: %w", ctr.ID(), define.ErrCtrStateInvalid)
	}
	if err := isBridgeNetMode(ctr.config.NetMode); err != nil {
		return nil, err
	}
	logrus.Infof("Going to reload container %s network", ctr.ID())

	err := r.teardownNetwork(ctr)
	if err != nil {
		// teardownNetwork will error if the iptables rules do not exist and this is the case after
		// a firewall reload. The purpose of network reload is to recreate the rules if they do
		// not exists so we should not log this specific error as error. This would confuse users otherwise.
		// iptables-legacy and iptables-nft will create different errors. Make sure to match both.
		b, rerr := regexp.MatchString("Couldn't load target `CNI-[a-f0-9]{24}':No such file or directory|Chain 'CNI-[a-f0-9]{24}' does not exist", err.Error())
		if rerr == nil && !b {
			logrus.Error(err)
		} else {
			logrus.Info(err)
		}
	}

	networkOpts, err := ctr.networks()
	if err != nil {
		return nil, err
	}

	// Set the same network settings as before..
	netStatus := ctr.getNetworkStatus()
	for network, perNetOpts := range networkOpts {
		for name, netInt := range netStatus[network].Interfaces {
			perNetOpts.InterfaceName = name
			perNetOpts.StaticMAC = netInt.MacAddress
			for _, netAddress := range netInt.Subnets {
				perNetOpts.StaticIPs = append(perNetOpts.StaticIPs, netAddress.IPNet.IP)
			}
			// Normally interfaces have a length of 1, only for some special cni configs we could get more.
			// For now just use the first interface to get the ips this should be good enough for most cases.
			break
		}
		networkOpts[network] = perNetOpts
	}
	ctr.perNetworkOpts = networkOpts

	return r.configureNetNS(ctr, ctr.state.NetNS)
}

// Produce an InspectNetworkSettings containing information on the container
// network.
func (c *Container) getContainerNetworkInfo() (*define.InspectNetworkSettings, error) {
	if c.config.NetNsCtr != "" {
		netNsCtr, err := c.runtime.GetContainer(c.config.NetNsCtr)
		if err != nil {
			return nil, err
		}
		// see https://github.com/containers/podman/issues/10090
		// the container has to be locked for syncContainer()
		netNsCtr.lock.Lock()
		defer netNsCtr.lock.Unlock()
		// Have to sync to ensure that state is populated
		if err := netNsCtr.syncContainer(); err != nil {
			return nil, err
		}
		logrus.Debugf("Container %s shares network namespace, retrieving network info of container %s", c.ID(), c.config.NetNsCtr)

		return netNsCtr.getContainerNetworkInfo()
	}

	settings := new(define.InspectNetworkSettings)
	settings.Ports = makeInspectPortBindings(c.config.PortMappings)

	networks, err := c.networks()
	if err != nil {
		return nil, err
	}

	getNetworkID := func(nameOrID string) string {
		network, err := c.runtime.network.NetworkInspect(nameOrID)
		if err == nil && network.ID != "" {
			return network.ID
		}
		return nameOrID
	}

	setDefaultNetworks := func() {
		settings.Networks = make(map[string]*define.InspectAdditionalNetwork, 1)
		name := c.NetworkMode()
		addedNet := new(define.InspectAdditionalNetwork)
		addedNet.NetworkID = getNetworkID(name)
		settings.Networks[name] = addedNet
	}

	if c.state.NetNS == "" {
		if networkNSPath, set := c.joinedNetworkNSPath(); networkNSPath != "" {
			if result, err := c.inspectJoinedNetworkNS(networkNSPath); err == nil {
				// fallback to dummy configuration
				settings.InspectBasicNetworkConfig = resultToBasicNetworkConfig(result)
			} else {
				// do not propagate error inspecting a joined network ns
				logrus.Errorf("Inspecting network namespace: %s of container %s: %v", networkNSPath, c.ID(), err)
			}
			return settings, nil
		} else if set {
			// network none case, if running allow user to join netns via sandbox key
			// https://github.com/containers/podman/issues/16716
			if c.state.PID > 0 {
				settings.SandboxKey = fmt.Sprintf("/proc/%d/ns/net", c.state.PID)
			}
		}
		// We can't do more if the network is down.
		// We still want to make dummy configurations for each network
		// the container joined.
		if len(networks) > 0 {
			settings.Networks = make(map[string]*define.InspectAdditionalNetwork, len(networks))
			for net, opts := range networks {
				cniNet := new(define.InspectAdditionalNetwork)
				cniNet.NetworkID = getNetworkID(net)
				cniNet.Aliases = opts.Aliases
				settings.Networks[net] = cniNet
			}
		} else {
			setDefaultNetworks()
		}

		return settings, nil
	}

	// Set network namespace path
	settings.SandboxKey = c.state.NetNS

	netStatus := c.getNetworkStatus()
	// If this is empty, we're probably slirp4netns
	if len(netStatus) == 0 {
		return settings, nil
	}

	// If we have networks - handle that here
	if len(networks) > 0 {
		if len(networks) != len(netStatus) {
			return nil, fmt.Errorf("network inspection mismatch: asked to join %d network(s) %v, but have information on %d network(s): %w", len(networks), networks, len(netStatus), define.ErrInternal)
		}

		settings.Networks = make(map[string]*define.InspectAdditionalNetwork, len(networks))

		for name, opts := range networks {
			result := netStatus[name]
			addedNet := new(define.InspectAdditionalNetwork)
			addedNet.NetworkID = getNetworkID(name)
			addedNet.Aliases = opts.Aliases
			addedNet.InspectBasicNetworkConfig = resultToBasicNetworkConfig(result)

			settings.Networks[name] = addedNet
		}

		// if not only the default network is connected we can return here
		// otherwise we have to populate the InspectBasicNetworkConfig settings
		_, isDefaultNet := networks[c.runtime.config.Network.DefaultNetwork]
		if len(networks) != 1 || !isDefaultNet {
			return settings, nil
		}
	} else {
		setDefaultNetworks()
	}

	// If not joining networks, we should have at most 1 result
	if len(netStatus) > 1 {
		return nil, fmt.Errorf("should have at most 1 network status result if not joining networks, instead got %d: %w", len(netStatus), define.ErrInternal)
	}

	if len(netStatus) == 1 {
		for _, status := range netStatus {
			settings.InspectBasicNetworkConfig = resultToBasicNetworkConfig(status)
		}
	}
	return settings, nil
}

// resultToBasicNetworkConfig produces an InspectBasicNetworkConfig from a CNI
// result
func resultToBasicNetworkConfig(result types.StatusBlock) define.InspectBasicNetworkConfig {
	config := define.InspectBasicNetworkConfig{}
	interfaceNames := make([]string, 0, len(result.Interfaces))
	for interfaceName := range result.Interfaces {
		interfaceNames = append(interfaceNames, interfaceName)
	}
	// ensure consistent inspect results by sorting
	sort.Strings(interfaceNames)
	for _, interfaceName := range interfaceNames {
		netInt := result.Interfaces[interfaceName]
		for _, netAddress := range netInt.Subnets {
			size, _ := netAddress.IPNet.Mask.Size()
			if netAddress.IPNet.IP.To4() != nil {
				// ipv4
				if config.IPAddress == "" {
					config.IPAddress = netAddress.IPNet.IP.String()
					config.IPPrefixLen = size
					config.Gateway = netAddress.Gateway.String()
				} else {
					config.SecondaryIPAddresses = append(config.SecondaryIPAddresses, define.Address{Addr: netAddress.IPNet.IP.String(), PrefixLength: size})
				}
			} else {
				// ipv6
				if config.GlobalIPv6Address == "" {
					config.GlobalIPv6Address = netAddress.IPNet.IP.String()
					config.GlobalIPv6PrefixLen = size
					config.IPv6Gateway = netAddress.Gateway.String()
				} else {
					config.SecondaryIPv6Addresses = append(config.SecondaryIPv6Addresses, define.Address{Addr: netAddress.IPNet.IP.String(), PrefixLength: size})
				}
			}
		}
		if config.MacAddress == "" {
			config.MacAddress = netInt.MacAddress.String()
		} else {
			config.AdditionalMacAddresses = append(config.AdditionalMacAddresses, netInt.MacAddress.String())
		}
	}
	return config
}

// NetworkDisconnect removes a container from the network
func (c *Container) NetworkDisconnect(nameOrID, netName string, _ bool) error {
	// only the bridge mode supports cni networks
	if err := isBridgeNetMode(c.config.NetMode); err != nil {
		return err
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	networks, err := c.networks()
	if err != nil {
		return err
	}

	// check if network exists and if the input is an ID we get the name
	// CNI and netavark and the libpod db only uses names so it is important that we only use the name
	netName, _, err = c.runtime.normalizeNetworkName(netName)
	if err != nil {
		return err
	}

	netOpts, nameExists := networks[netName]
	if !nameExists && len(networks) > 0 {
		return fmt.Errorf("container %s is not connected to network %s", nameOrID, netName)
	}

	if err := c.syncContainer(); err != nil {
		return err
	}
	// get network status before we disconnect
	networkStatus := c.getNetworkStatus()

	if err := c.runtime.state.NetworkDisconnect(c, netName); err != nil {
		return err
	}

	// Since we removed the new network from the container db we must have to add it back during partial setup errors
	addContainerNetworkToDB := func() {
		if err := c.runtime.state.NetworkConnect(c, netName, netOpts); err != nil {
			logrus.Errorf("Failed to add network %s for container %s to DB after failed network disconnect", netName, nameOrID)
		}
	}

	c.newNetworkEvent(events.NetworkDisconnect, netName)
	if !c.ensureState(define.ContainerStateRunning, define.ContainerStateCreated) {
		return nil
	}

	if c.state.NetNS == "" {
		addContainerNetworkToDB()
		return fmt.Errorf("unable to disconnect %s from %s: %w", nameOrID, netName, define.ErrNoNetwork)
	}

	opts := types.NetworkOptions{
		ContainerID:   c.config.ID,
		ContainerName: getNetworkPodName(c),
	}
	opts.PortMappings = c.convertPortMappings()
	opts.Networks = map[string]types.PerNetworkOptions{
		netName: networks[netName],
	}

	if err := c.runtime.teardownNetworkBackend(c.state.NetNS, opts); err != nil {
		addContainerNetworkToDB()
		return err
	}

	// update network status if container is running
	oldStatus, statusExist := networkStatus[netName]
	delete(networkStatus, netName)
	c.state.NetworkStatus = networkStatus
	err = c.save()
	if err != nil {
		return err
	}

	// Reload ports when there are still connected networks, maybe we removed the network interface with the child ip.
	// Reloading without connected networks does not make sense, so we can skip this step.
	if rootless.IsRootless() && len(networkStatus) > 0 {
		if err := c.reloadRootlessRLKPortMapping(); err != nil {
			return err
		}
	}

	// Update resolv.conf if required
	if statusExist {
		stringIPs := make([]string, 0, len(oldStatus.DNSServerIPs))
		for _, ip := range oldStatus.DNSServerIPs {
			stringIPs = append(stringIPs, ip.String())
		}
		if len(stringIPs) > 0 {
			logrus.Debugf("Removing DNS Servers %v from resolv.conf", stringIPs)
			if err := c.removeNameserver(stringIPs); err != nil {
				return err
			}
		}

		// update /etc/hosts file
		if file, ok := c.state.BindMounts[config.DefaultHostsFile]; ok {
			// sync the names with c.getHostsEntries()
			names := []string{c.Hostname(), c.config.Name}
			rm := etchosts.GetNetworkHostEntries(map[string]types.StatusBlock{netName: oldStatus}, names...)
			if len(rm) > 0 {
				// make sure to lock this file to prevent concurrent writes when
				// this is used a net dependency container
				lock, err := lockfile.GetLockFile(file)
				if err != nil {
					return fmt.Errorf("failed to lock hosts file: %w", err)
				}
				logrus.Debugf("Remove /etc/hosts entries %v", rm)
				lock.Lock()
				err = etchosts.Remove(file, rm)
				lock.Unlock()
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ConnectNetwork connects a container to a given network
func (c *Container) NetworkConnect(nameOrID, netName string, netOpts types.PerNetworkOptions) error {
	// only the bridge mode supports networks
	if err := isBridgeNetMode(c.config.NetMode); err != nil {
		return err
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	networks, err := c.networks()
	if err != nil {
		return err
	}

	// check if network exists and if the input is an ID we get the name
	// CNI and netavark and the libpod db only uses names so it is important that we only use the name
	var nicName string
	netName, nicName, err = c.runtime.normalizeNetworkName(netName)
	if err != nil {
		return err
	}

	if err := c.syncContainer(); err != nil {
		return err
	}

	// get network status before we connect
	networkStatus := c.getNetworkStatus()

	netOpts.Aliases = append(netOpts.Aliases, getExtraNetworkAliases(c)...)

	// check whether interface is to be named as the network_interface
	// when name left unspecified
	if netOpts.InterfaceName == "" {
		netOpts.InterfaceName = nicName
	}

	// set default interface name
	if netOpts.InterfaceName == "" {
		netOpts.InterfaceName = getFreeInterfaceName(networks)
		if netOpts.InterfaceName == "" {
			return errors.New("could not find free network interface name")
		}
	}

	if err := c.runtime.state.NetworkConnect(c, netName, netOpts); err != nil {
		// Docker compat: treat requests to attach already attached networks as a no-op, ignoring opts
		if errors.Is(err, define.ErrNetworkConnected) && !c.ensureState(define.ContainerStateRunning, define.ContainerStateCreated) {
			return nil
		}

		return err
	}

	// Since we added the new network to the container db we must have to remove it from that during partial setup errors
	removeContainerNetworkFromDB := func() {
		if err := c.runtime.state.NetworkDisconnect(c, netName); err != nil {
			logrus.Errorf("Failed to remove network %s for container %s from DB after failed network connect", netName, nameOrID)
		}
	}

	c.newNetworkEvent(events.NetworkConnect, netName)
	if !c.ensureState(define.ContainerStateRunning, define.ContainerStateCreated) {
		return nil
	}
	if c.state.NetNS == "" {
		removeContainerNetworkFromDB()
		return fmt.Errorf("unable to connect %s to %s: %w", nameOrID, netName, define.ErrNoNetwork)
	}

	opts := types.NetworkOptions{
		ContainerID:   c.config.ID,
		ContainerName: getNetworkPodName(c),
	}
	opts.PortMappings = c.convertPortMappings()
	opts.Networks = map[string]types.PerNetworkOptions{
		netName: netOpts,
	}

	results, err := c.runtime.setUpNetwork(c.state.NetNS, opts)
	if err != nil {
		removeContainerNetworkFromDB()
		return err
	}
	if len(results) != 1 {
		return errors.New("when adding aliases, results must be of length 1")
	}

	// we need to get the old host entries before we add the new one to the status
	// if we do not add do it here we will get the wrong existing entries which will throw of the logic
	// we could also copy the map but this does not seem worth it
	// sync the hostNames with c.getHostsEntries()
	hostNames := []string{c.Hostname(), c.config.Name}
	oldHostEntries := etchosts.GetNetworkHostEntries(networkStatus, hostNames...)

	// update network status
	if networkStatus == nil {
		networkStatus = make(map[string]types.StatusBlock, 1)
	}
	networkStatus[netName] = results[netName]
	c.state.NetworkStatus = networkStatus

	err = c.save()
	if err != nil {
		return err
	}

	// The first network needs a port reload to set the correct child ip for the rootlessport process.
	// Adding a second network does not require a port reload because the child ip is still valid.
	if rootless.IsRootless() && len(networks) == 0 {
		if err := c.reloadRootlessRLKPortMapping(); err != nil {
			return err
		}
	}

	ipv6 := c.checkForIPv6(networkStatus)

	// Update resolv.conf if required
	stringIPs := make([]string, 0, len(results[netName].DNSServerIPs))
	for _, ip := range results[netName].DNSServerIPs {
		if (ip.To4() == nil) && !ipv6 {
			continue
		}
		stringIPs = append(stringIPs, ip.String())
	}
	if len(stringIPs) > 0 {
		logrus.Debugf("Adding DNS Servers %v to resolv.conf", stringIPs)
		if err := c.addNameserver(stringIPs); err != nil {
			return err
		}
	}

	// update /etc/hosts file
	if file, ok := c.state.BindMounts[config.DefaultHostsFile]; ok {
		// make sure to lock this file to prevent concurrent writes when
		// this is used a net dependency container
		lock, err := lockfile.GetLockFile(file)
		if err != nil {
			return fmt.Errorf("failed to lock hosts file: %w", err)
		}
		new := etchosts.GetNetworkHostEntries(results, hostNames...)
		logrus.Debugf("Add /etc/hosts entries %v", new)
		// use special AddIfExists API to make sure we only add new entries if an old one exists
		// see the AddIfExists() comment for more information
		lock.Lock()
		err = etchosts.AddIfExists(file, oldHostEntries, new)
		lock.Unlock()
		if err != nil {
			return err
		}
	}

	return nil
}

// get a free interface name for a new network
// return an empty string if no free name was found
func getFreeInterfaceName(networks map[string]types.PerNetworkOptions) string {
	ifNames := make([]string, 0, len(networks))
	for _, opts := range networks {
		ifNames = append(ifNames, opts.InterfaceName)
	}
	for i := range 100000 {
		ifName := fmt.Sprintf("eth%d", i)
		if !slices.Contains(ifNames, ifName) {
			return ifName
		}
	}
	return ""
}

func getExtraNetworkAliases(c *Container) []string {
	// always add the short id as alias for docker compat
	alias := []string{c.config.ID[:12]}
	// if an explicit hostname was set add it as well
	if c.config.Spec.Hostname != "" {
		alias = append(alias, c.config.Spec.Hostname)
	}
	return alias
}

// DisconnectContainerFromNetwork removes a container from its network
func (r *Runtime) DisconnectContainerFromNetwork(nameOrID, netName string, force bool) error {
	ctr, err := r.LookupContainer(nameOrID)
	if err != nil {
		return err
	}
	return ctr.NetworkDisconnect(nameOrID, netName, force)
}

// ConnectContainerToNetwork connects a container to a network
func (r *Runtime) ConnectContainerToNetwork(nameOrID, netName string, netOpts types.PerNetworkOptions) error {
	ctr, err := r.LookupContainer(nameOrID)
	if err != nil {
		return err
	}
	return ctr.NetworkConnect(nameOrID, netName, netOpts)
}

// normalizeNetworkName takes a network name, a partial or a full network ID and
// returns: 1) the network name and 2) the network_interface name for macvlan
// and ipvlan drivers if the naming pattern is "device" defined in the
// containers.conf file. Else, "".
// If the network is not found an error is returned.
func (r *Runtime) normalizeNetworkName(nameOrID string) (string, string, error) {
	net, err := r.network.NetworkInspect(nameOrID)
	if err != nil {
		return "", "", err
	}

	netIface := ""
	namingPattern := r.config.Containers.InterfaceName
	if namingPattern == "device" && (net.Driver == types.MacVLANNetworkDriver || net.Driver == types.IPVLANNetworkDriver) {
		netIface = net.NetworkInterface
	}

	return net.Name, netIface, nil
}

// ocicniPortsToNetTypesPorts convert the old port format to the new one
// while deduplicating ports into ranges
func ocicniPortsToNetTypesPorts(ports []types.OCICNIPortMapping) []types.PortMapping {
	if len(ports) == 0 {
		return nil
	}

	newPorts := make([]types.PortMapping, 0, len(ports))

	// first sort the ports
	sort.Slice(ports, func(i, j int) bool {
		return compareOCICNIPorts(ports[i], ports[j])
	})

	// we already check if the slice is empty so we can use the first element
	currentPort := types.PortMapping{
		HostIP:        ports[0].HostIP,
		HostPort:      uint16(ports[0].HostPort),
		ContainerPort: uint16(ports[0].ContainerPort),
		Protocol:      ports[0].Protocol,
		Range:         1,
	}

	for i := 1; i < len(ports); i++ {
		if ports[i].HostIP == currentPort.HostIP &&
			ports[i].Protocol == currentPort.Protocol &&
			ports[i].HostPort-int32(currentPort.Range) == int32(currentPort.HostPort) &&
			ports[i].ContainerPort-int32(currentPort.Range) == int32(currentPort.ContainerPort) {
			currentPort.Range++
		} else {
			newPorts = append(newPorts, currentPort)
			currentPort = types.PortMapping{
				HostIP:        ports[i].HostIP,
				HostPort:      uint16(ports[i].HostPort),
				ContainerPort: uint16(ports[i].ContainerPort),
				Protocol:      ports[i].Protocol,
				Range:         1,
			}
		}
	}
	newPorts = append(newPorts, currentPort)
	return newPorts
}

// compareOCICNIPorts will sort the ocicni ports by
// 1) host ip
// 2) protocol
// 3) hostPort
// 4) container port
func compareOCICNIPorts(i, j types.OCICNIPortMapping) bool {
	if i.HostIP != j.HostIP {
		return i.HostIP < j.HostIP
	}

	if i.Protocol != j.Protocol {
		return i.Protocol < j.Protocol
	}

	if i.HostPort != j.HostPort {
		return i.HostPort < j.HostPort
	}

	return i.ContainerPort < j.ContainerPort
}
