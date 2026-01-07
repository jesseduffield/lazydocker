// SPDX-License-Identifier: Apache-2.0
//
// pasta.go - Start pasta(1) for user-mode connectivity
//
// Copyright (c) 2022 Red Hat GmbH
// Author: Stefano Brivio <sbrivio@redhat.com>

// This file has been imported from the podman repository
// (libpod/networking_pasta_linux.go), for the full history see there.

package pasta

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"slices"
	"strings"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/libnetwork/util"
	"go.podman.io/common/pkg/config"
)

const (
	dnsForwardOpt   = "--dns-forward"
	mapGuestAddrOpt = "--map-guest-addr"

	// dnsForwardIpv4 static ip used as nameserver address inside the netns,
	// given this is a "link local" ip it should be very unlikely that it causes conflicts.
	dnsForwardIpv4 = "169.254.1.1"

	// mapGuestAddrIpv4 static ip used as forwarder address inside the netns to reach the host,
	// given this is a "link local" ip it should be very unlikely that it causes conflicts.
	mapGuestAddrIpv4 = "169.254.1.2"
)

type SetupOptions struct {
	// Config used to get pasta options and binary path via HelperBinariesDir
	Config *config.Config
	// Netns is the path to the container Netns
	Netns string
	// Ports that should be forwarded in the container
	Ports []types.PortMapping
	// ExtraOptions are pasta(1) cli options, these will be appended after the
	// pasta options from containers.conf to allow some form of overwrite.
	ExtraOptions []string
}

// Setup start the pasta process for the given netns.
// The pasta binary is looked up in the HelperBinariesDir and $PATH.
// Note that there is no need for any special cleanup logic, the pasta
// process will automatically exit when the netns path is deleted.
func Setup(opts *SetupOptions) (*SetupResult, error) {
	path, err := opts.Config.FindHelperBinary(BinaryName, true)
	if err != nil {
		return nil, fmt.Errorf("could not find pasta, the network namespace can't be configured: %w", err)
	}

	cmdArgs, dnsForwardIPs, mapGuestAddrIPs, err := createPastaArgs(opts)
	if err != nil {
		return nil, err
	}

	logrus.Debugf("pasta arguments: %s", strings.Join(cmdArgs, " "))

	for {
		// pasta forks once ready, and quits once we delete the target namespace
		out, err := exec.Command(path, cmdArgs...).CombinedOutput()
		if err != nil {
			exitErr := &exec.ExitError{}
			if errors.As(err, &exitErr) {
				// special backwards compat check, --map-guest-addr was added in pasta version 20240814 so we
				// cannot hard require it yet. Once we are confident that the update is most distros we can remove it.
				if exitErr.ExitCode() == 1 &&
					strings.Contains(string(out), "unrecognized option '"+mapGuestAddrOpt) &&
					len(mapGuestAddrIPs) == 1 && mapGuestAddrIPs[0] == mapGuestAddrIpv4 {
					// we did add the default --map-guest-addr option, if users set something different we want
					// to get to the error below. We have to unset mapGuestAddrIPs here to avoid a infinite loop.
					mapGuestAddrIPs = nil
					// Trim off last two args which are --map-guest-addr 169.254.1.2.
					cmdArgs = cmdArgs[:len(cmdArgs)-2]
					continue
				}
				return nil, fmt.Errorf("pasta failed with exit code %d:\n%s",
					exitErr.ExitCode(), string(out))
			}
			return nil, fmt.Errorf("failed to start pasta: %w", err)
		}

		if len(out) > 0 {
			// TODO: This should be warning but as of August 2024 pasta still prints
			// things with --quiet that we do not care about. In podman CI I still see
			// "Couldn't get any nameserver address" so until this is fixed we cannot
			// enable it. For now info is fine and we can bump it up later, it is only a
			// nice to have.
			logrus.Infof("pasta logged warnings: %q", strings.TrimSpace(string(out)))
		}
		break
	}

	var ipv4, ipv6 bool
	result := &SetupResult{}
	err = ns.WithNetNSPath(opts.Netns, func(_ ns.NetNS) error {
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return err
		}
		for _, addr := range addrs {
			// make sure to skip loopback and multicast addresses
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && !ipnet.IP.IsMulticast() {
				if util.IsIPv4(ipnet.IP) {
					result.IPAddresses = append(result.IPAddresses, ipnet.IP)
					ipv4 = true
				} else if !ipnet.IP.IsLinkLocalUnicast() {
					// Else must be ipv6.
					// We shouldn't resolve hosts.containers.internal to IPv6
					// link-local addresses, for two reasons:
					// 1. even if IPv6 is disabled in pasta (--ipv4-only), the
					//    kernel will configure an IPv6 link-local address in the
					//    container, but that doesn't mean that IPv6 connectivity
					//    is actually working
					// 2. link-local addresses need to be suffixed by the zone
					//    (interface) to be of any use, but we can't do it here
					//
					// Thus, don't include IPv6 link-local addresses in
					// IPAddresses: Podman uses them for /etc/hosts entries, and
					// those need to be functional.
					result.IPAddresses = append(result.IPAddresses, ipnet.IP)
					ipv6 = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	result.IPv6 = ipv6
	result.DNSForwardIPs = filterIPFamily(dnsForwardIPs, ipv4, ipv6)
	result.MapGuestAddrIPs = filterIPFamily(mapGuestAddrIPs, ipv4, ipv6)

	return result, nil
}

func filterIPFamily(ips []string, ipv4, ipv6 bool) []string {
	var result []string
	for _, ip := range ips {
		ipp := net.ParseIP(ip)
		// add the ip only if the address family matches
		if ipv4 && util.IsIPv4(ipp) || ipv6 && util.IsIPv6(ipp) {
			result = append(result, ip)
		}
	}
	return result
}

// createPastaArgs creates the pasta arguments, it returns the args to be passed to pasta(1)
// and as second arg the dns forward ips used. As third arg the map guest addr ips used.
func createPastaArgs(opts *SetupOptions) ([]string, []string, []string, error) {
	noTCPInitPorts := true
	noUDPInitPorts := true
	noTCPNamespacePorts := true
	noUDPNamespacePorts := true
	noMapGW := true
	quiet := true

	cmdArgs := []string{"--config-net"}
	// first append options set in the config
	cmdArgs = append(cmdArgs, opts.Config.Network.PastaOptions.Get()...)
	// then append the ones that were set on the cli
	cmdArgs = append(cmdArgs, opts.ExtraOptions...)

	cmdArgs = slices.DeleteFunc(cmdArgs, func(s string) bool {
		// --map-gw is not a real pasta(1) option so we must remove it
		// and not add --no-map-gw below
		if s == "--map-gw" {
			noMapGW = false
			return true
		}
		return false
	})

	var dnsForwardIPs []string
	var mapGuestAddrIPs []string
	for i, opt := range cmdArgs {
		switch opt {
		case "-t", "--tcp-ports":
			noTCPInitPorts = false
		case "-u", "--udp-ports":
			noUDPInitPorts = false
		case "-T", "--tcp-ns":
			noTCPNamespacePorts = false
		case "-U", "--udp-ns":
			noUDPNamespacePorts = false
		case "-d", "--debug", "--trace":
			quiet = false
		case dnsForwardOpt:
			// if there is no arg after it pasta will likely error out anyway due invalid cli args
			if len(cmdArgs) > i+1 {
				dnsForwardIPs = append(dnsForwardIPs, cmdArgs[i+1])
			}
		case mapGuestAddrOpt:
			if len(cmdArgs) > i+1 {
				mapGuestAddrIPs = append(mapGuestAddrIPs, cmdArgs[i+1])
			}
		}
	}

	for _, i := range opts.Ports {
		for protocol := range strings.SplitSeq(i.Protocol, ",") {
			var addr string

			if i.HostIP != "" {
				addr = i.HostIP + "/"
			}

			switch protocol {
			case "tcp":
				noTCPInitPorts = false
				cmdArgs = append(cmdArgs, "-t")
			case "udp":
				noUDPInitPorts = false
				cmdArgs = append(cmdArgs, "-u")
			default:
				return nil, nil, nil, fmt.Errorf("can't forward protocol: %s", protocol)
			}

			arg := fmt.Sprintf("%s%d-%d:%d-%d", addr,
				i.HostPort,
				i.HostPort+i.Range-1,
				i.ContainerPort,
				i.ContainerPort+i.Range-1)
			cmdArgs = append(cmdArgs, arg)
		}
	}

	if len(dnsForwardIPs) == 0 {
		// the user did not request custom --dns-forward so add our own.
		cmdArgs = append(cmdArgs, dnsForwardOpt, dnsForwardIpv4)
		dnsForwardIPs = append(dnsForwardIPs, dnsForwardIpv4)
	}

	if noTCPInitPorts {
		cmdArgs = append(cmdArgs, "-t", "none")
	}
	if noUDPInitPorts {
		cmdArgs = append(cmdArgs, "-u", "none")
	}
	if noTCPNamespacePorts {
		cmdArgs = append(cmdArgs, "-T", "none")
	}
	if noUDPNamespacePorts {
		cmdArgs = append(cmdArgs, "-U", "none")
	}
	if noMapGW {
		cmdArgs = append(cmdArgs, "--no-map-gw")
	}
	if quiet {
		// pass --quiet to silence the info output from pasta if verbose/trace pasta is not required
		cmdArgs = append(cmdArgs, "--quiet")
	}

	cmdArgs = append(cmdArgs, "--netns", opts.Netns)

	// do this as last arg so we can easily trim them off in the error case when we have an older version
	if len(mapGuestAddrIPs) == 0 {
		// the user did not request custom --map-guest-addr so add our own so that we can use this
		// for our own host.containers.internal host entry.
		cmdArgs = append(cmdArgs, mapGuestAddrOpt, mapGuestAddrIpv4)
		mapGuestAddrIPs = append(mapGuestAddrIPs, mapGuestAddrIpv4)
	}

	return cmdArgs, dnsForwardIPs, mapGuestAddrIPs, nil
}
