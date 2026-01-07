package specgen

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/namespaces"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/podman/v5/pkg/util"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/opencontainers/runtime-tools/generate"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/common/pkg/cgroups"
	"go.podman.io/common/pkg/config"
	"go.podman.io/storage/pkg/fileutils"
	"go.podman.io/storage/pkg/unshare"
	storageTypes "go.podman.io/storage/types"
)

type NamespaceMode string

const (
	// Default indicates the spec generator should determine
	// a sane default
	Default NamespaceMode = "default"
	// Host means the namespace is derived from the host
	Host NamespaceMode = "host"
	// Path is the path to a namespace
	Path NamespaceMode = "path"
	// FromContainer means namespace is derived from a
	// different container
	FromContainer NamespaceMode = "container"
	// FromPod indicates the namespace is derived from a pod
	FromPod NamespaceMode = "pod"
	// Private indicates the namespace is private
	Private NamespaceMode = "private"
	// Shareable indicates the namespace is shareable
	Shareable NamespaceMode = "shareable"
	// None indicates the IPC namespace is created without mounting /dev/shm
	None NamespaceMode = "none"
	// NoNetwork indicates no network namespace should
	// be joined.  loopback should still exist.
	// Only used with the network namespace, invalid otherwise.
	NoNetwork NamespaceMode = "none"
	// Bridge indicates that the network backend (CNI/netavark)
	// should be used.
	// Only used with the network namespace, invalid otherwise.
	Bridge NamespaceMode = "bridge"
	// Slirp indicates that a slirp4netns network stack should
	// be used.
	// Only used with the network namespace, invalid otherwise.
	Slirp NamespaceMode = "slirp4netns"
	// Pasta indicates that a pasta network stack should be used.
	// Only used with the network namespace, invalid otherwise.
	Pasta NamespaceMode = "pasta"
	// KeepID indicates a user namespace to keep the owner uid inside
	// of the namespace itself.
	// Only used with the user namespace, invalid otherwise.
	KeepID NamespaceMode = "keep-id"
	// NoMap indicates a user namespace to keep the owner uid out
	// of the namespace itself.
	// Only used with the user namespace, invalid otherwise.
	NoMap NamespaceMode = "no-map"
	// Auto indicates to automatically create a user namespace.
	// Only used with the user namespace, invalid otherwise.
	Auto NamespaceMode = "auto"

	// DefaultKernelNamespaces is a comma-separated list of default kernel
	// namespaces.
	DefaultKernelNamespaces = "ipc,net,uts"
)

// Namespace describes the namespace
type Namespace struct {
	NSMode NamespaceMode `json:"nsmode,omitempty"`
	Value  string        `json:"value,omitempty"`
}

// IsDefault returns whether the namespace is set to the default setting (which
// also includes the empty string).
func (n *Namespace) IsDefault() bool {
	return n.NSMode == Default || n.NSMode == ""
}

// IsHost returns a bool if the namespace is host based
func (n *Namespace) IsHost() bool {
	return n.NSMode == Host
}

// IsNone returns a bool if the namespace is set to none
func (n *Namespace) IsNone() bool {
	return n.NSMode == None
}

// IsBridge returns a bool if the namespace is a Bridge
func (n *Namespace) IsBridge() bool {
	return n.NSMode == Bridge
}

// IsPath indicates via bool if the namespace is based on a path
func (n *Namespace) IsPath() bool {
	return n.NSMode == Path
}

// IsContainer indicates via bool if the namespace is based on a container
func (n *Namespace) IsContainer() bool {
	return n.NSMode == FromContainer
}

// IsPod indicates via bool if the namespace is based on a pod
func (n *Namespace) IsPod() bool {
	return n.NSMode == FromPod
}

// IsPrivate indicates the namespace is private
func (n *Namespace) IsPrivate() bool {
	return n.NSMode == Private
}

// IsAuto indicates the namespace is auto
func (n *Namespace) IsAuto() bool {
	return n.NSMode == Auto
}

// IsKeepID indicates the namespace is KeepID
func (n *Namespace) IsKeepID() bool {
	return n.NSMode == KeepID
}

// IsNoMap indicates the namespace is NoMap
func (n *Namespace) IsNoMap() bool {
	return n.NSMode == NoMap
}

func (n *Namespace) String() string {
	if n.Value != "" {
		return fmt.Sprintf("%s:%s", n.NSMode, n.Value)
	}
	return string(n.NSMode)
}

func validateUserNS(n *Namespace) error {
	if n == nil {
		return nil
	}
	switch n.NSMode {
	case Auto, KeepID, NoMap:
		return nil
	}
	return n.validate()
}

func validateNetNS(n *Namespace) error {
	if n == nil {
		return nil
	}
	switch n.NSMode {
	case Slirp:
		break
	case Pasta:
		// Check if we run rootless/in a userns. Do not use rootless.IsRootless() here.
		// Pasta switches to nobody when running as root which causes it to fail while
		// opening the netns owned by root. However when pasta is already in a userns
		// it doesn't switch to nobody so it works there.
		// https://github.com/containers/podman/issues/17840
		if unshare.IsRootless() {
			break
		}
		return fmt.Errorf("pasta networking is only supported for rootless mode or when inside a nested userns")
	case "", Default, Host, Path, FromContainer, FromPod, Private, NoNetwork, Bridge:
		break
	default:
		return fmt.Errorf("invalid network %q", n.NSMode)
	}

	// Path and From Container MUST have a string value set
	if n.NSMode == Path || n.NSMode == FromContainer {
		if len(n.Value) < 1 {
			return fmt.Errorf("namespace mode %s requires a value", n.NSMode)
		}
	} else if n.NSMode != Slirp {
		// All others except must NOT set a string value
		if len(n.Value) > 0 {
			return fmt.Errorf("namespace value %s cannot be provided with namespace mode %s", n.Value, n.NSMode)
		}
	}

	return nil
}

func validateIPCNS(n *Namespace) error {
	if n == nil {
		return nil
	}
	switch n.NSMode {
	case Shareable, None:
		return nil
	}
	return n.validate()
}

// Validate perform simple validation on the namespace to make sure it is not
// invalid from the get-go
func (n *Namespace) validate() error {
	if n == nil {
		return nil
	}
	switch n.NSMode {
	case "", Default, Host, Path, FromContainer, FromPod, Private:
		// Valid, do nothing
	case NoNetwork, Bridge, Slirp, Pasta:
		return errors.New("cannot use network modes with non-network namespace")
	default:
		return fmt.Errorf("invalid namespace type %s specified", n.NSMode)
	}

	// Path and From Container MUST have a string value set
	if n.NSMode == Path || n.NSMode == FromContainer {
		if len(n.Value) < 1 {
			return fmt.Errorf("namespace mode %s requires a value", n.NSMode)
		}
	} else {
		// All others must NOT set a string value
		if len(n.Value) > 0 {
			return fmt.Errorf("namespace value %s cannot be provided with namespace mode %s", n.Value, n.NSMode)
		}
	}
	return nil
}

// ParseNamespace parses a namespace in string form.
// This is not intended for the network namespace, which has a separate
// function.
func ParseNamespace(ns string) (Namespace, error) {
	toReturn := Namespace{}
	switch ns {
	case "pod":
		toReturn.NSMode = FromPod
	case "host":
		toReturn.NSMode = Host
	case "private", "":
		toReturn.NSMode = Private
	default:
		if value, ok := strings.CutPrefix(ns, "ns:"); ok {
			toReturn.NSMode = Path
			toReturn.Value = value
		} else if value, ok := strings.CutPrefix(ns, "container:"); ok {
			toReturn.NSMode = FromContainer
			toReturn.Value = value
		} else {
			return toReturn, fmt.Errorf("unrecognized namespace mode %s passed", ns)
		}
	}

	return toReturn, nil
}

// ParseCgroupNamespace parses a cgroup namespace specification in string
// form.
func ParseCgroupNamespace(ns string) (Namespace, error) {
	toReturn := Namespace{}
	// Cgroup is host for v1, private for v2.
	// We can't trust c/common for this, as it only assumes private.
	cgroupsv2, err := cgroups.IsCgroup2UnifiedMode()
	if err != nil {
		return toReturn, err
	}
	if cgroupsv2 {
		switch ns {
		case "host":
			toReturn.NSMode = Host
		case "private", "":
			toReturn.NSMode = Private
		default:
			return toReturn, fmt.Errorf("unrecognized cgroup namespace mode %s passed", ns)
		}
	} else {
		toReturn.NSMode = Host
	}
	return toReturn, nil
}

// ParseIPCNamespace parses an ipc namespace specification in string
// form.
func ParseIPCNamespace(ns string) (Namespace, error) {
	toReturn := Namespace{}
	switch ns {
	case "shareable", "":
		toReturn.NSMode = Shareable
		return toReturn, nil
	case "none":
		toReturn.NSMode = None
		return toReturn, nil
	}
	return ParseNamespace(ns)
}

// ParseUserNamespace parses a user namespace specification in string
// form.
func ParseUserNamespace(ns string) (Namespace, error) {
	toReturn := Namespace{}
	switch ns {
	case "auto":
		toReturn.NSMode = Auto
		return toReturn, nil
	case "keep-id":
		toReturn.NSMode = KeepID
		return toReturn, nil
	case "nomap":
		toReturn.NSMode = NoMap
		return toReturn, nil
	case "":
		toReturn.NSMode = Host
		return toReturn, nil
	default:
		if value, ok := strings.CutPrefix(ns, "auto:"); ok {
			toReturn.NSMode = Auto
			toReturn.Value = value
			return toReturn, nil
		} else if value, ok := strings.CutPrefix(ns, "keep-id:"); ok {
			toReturn.NSMode = KeepID
			toReturn.Value = value
			return toReturn, nil
		} else {
			return ParseNamespace(ns)
		}
	}
}

// ParseNetworkFlag parses a network string slice into the network options
// If the input is nil or empty it will use the default setting from containers.conf
func ParseNetworkFlag(networks []string) (Namespace, map[string]types.PerNetworkOptions, map[string][]string, error) {
	var networkOptions map[string][]string
	toReturn := Namespace{}
	// by default we try to use the containers.conf setting
	// if we get at least one value use this instead
	cfg, err := config.Default()
	if err != nil {
		return toReturn, nil, nil, err
	}
	ns := cfg.Containers.NetNS
	if len(networks) > 0 {
		ns = networks[0]
	}

	podmanNetworks := make(map[string]types.PerNetworkOptions)

	switch {
	case ns == string(Slirp), strings.HasPrefix(ns, string(Slirp)+":"):
		key, options, hasOptions := strings.Cut(ns, ":")
		if hasOptions {
			networkOptions = make(map[string][]string)
			networkOptions[key] = strings.Split(options, ",")
		}
		toReturn.NSMode = Slirp
	case ns == string(FromPod):
		toReturn.NSMode = FromPod
	case ns == "" || ns == string(Default) || ns == string(Private):
		toReturn.NSMode = Private
	case ns == string(Bridge), strings.HasPrefix(ns, string(Bridge)+":"):
		toReturn.NSMode = Bridge
		_, options, hasOptions := strings.Cut(ns, ":")
		netOpts := types.PerNetworkOptions{}
		if hasOptions {
			var err error
			netOpts, err = parseBridgeNetworkOptions(options)
			if err != nil {
				return toReturn, nil, nil, err
			}
		}
		// we have to set the special default network name here
		podmanNetworks["default"] = netOpts

	case ns == string(NoNetwork):
		toReturn.NSMode = NoNetwork
	case ns == string(Host):
		toReturn.NSMode = Host
	case strings.HasPrefix(ns, "ns:"):
		_, value, _ := strings.Cut(ns, ":")
		toReturn.NSMode = Path
		toReturn.Value = value
	case strings.HasPrefix(ns, string(FromContainer)+":"):
		_, value, _ := strings.Cut(ns, ":")
		toReturn.NSMode = FromContainer
		toReturn.Value = value
	case ns == string(Pasta), strings.HasPrefix(ns, string(Pasta)+":"):
		key, options, hasOptions := strings.Cut(ns, ":")
		if hasOptions {
			networkOptions = make(map[string][]string)
			networkOptions[key] = strings.Split(options, ",")
		}
		toReturn.NSMode = Pasta
	default:
		// we should have a normal network
		name, options, hasOptions := strings.Cut(ns, ":")
		if hasOptions {
			if name == "" {
				return toReturn, nil, nil, errors.New("network name cannot be empty")
			}
			netOpts, err := parseBridgeNetworkOptions(options)
			if err != nil {
				return toReturn, nil, nil, fmt.Errorf("invalid option for network %s: %w", name, err)
			}
			podmanNetworks[name] = netOpts
		} else {
			// Assume we have been given a comma separated list of networks for backwards compat.
			networkList := strings.SplitSeq(ns, ",")
			for net := range networkList {
				podmanNetworks[net] = types.PerNetworkOptions{}
			}
		}

		// networks need bridge mode
		toReturn.NSMode = Bridge
	}

	if len(networks) > 1 {
		if !toReturn.IsBridge() {
			return toReturn, nil, nil, fmt.Errorf("cannot set multiple networks without bridge network mode, selected mode %s: %w", toReturn.NSMode, define.ErrInvalidArg)
		}

		for _, network := range networks[1:] {
			name, options, hasOptions := strings.Cut(network, ":")
			if name == "" {
				return toReturn, nil, nil, fmt.Errorf("network name cannot be empty: %w", define.ErrInvalidArg)
			}
			if slices.Contains([]string{string(Bridge), string(Slirp), string(Pasta), string(FromPod), string(NoNetwork),
				string(Default), string(Private), string(Path), string(FromContainer), string(Host)}, name) {
				return toReturn, nil, nil, fmt.Errorf("can only set extra network names, selected mode %s conflicts with bridge: %w", name, define.ErrInvalidArg)
			}
			netOpts := types.PerNetworkOptions{}
			if hasOptions {
				var err error
				netOpts, err = parseBridgeNetworkOptions(options)
				if err != nil {
					return toReturn, nil, nil, fmt.Errorf("invalid option for network %s: %w", name, err)
				}
			}
			podmanNetworks[name] = netOpts
		}
	}

	return toReturn, podmanNetworks, networkOptions, nil
}

func parseBridgeNetworkOptions(opts string) (types.PerNetworkOptions, error) {
	netOpts := types.PerNetworkOptions{}
	if len(opts) == 0 {
		return netOpts, nil
	}
	allopts := strings.SplitSeq(opts, ",")
	for opt := range allopts {
		name, value, _ := strings.Cut(opt, "=")
		switch name {
		case "ip", "ip6":
			ip := net.ParseIP(value)
			if ip == nil {
				return netOpts, fmt.Errorf("invalid ip address %q", value)
			}
			netOpts.StaticIPs = append(netOpts.StaticIPs, ip)

		case "mac":
			mac, err := net.ParseMAC(value)
			if err != nil {
				return netOpts, err
			}
			netOpts.StaticMAC = types.HardwareAddr(mac)

		case "alias":
			if value == "" {
				return netOpts, errors.New("alias cannot be empty")
			}
			netOpts.Aliases = append(netOpts.Aliases, value)

		case "interface_name":
			if value == "" {
				return netOpts, errors.New("interface_name cannot be empty")
			}
			netOpts.InterfaceName = value

		default:
			if netOpts.Options == nil {
				netOpts.Options = make(map[string]string)
			}
			netOpts.Options[name] = value
		}
	}
	return netOpts, nil
}

func SetupUserNS(idmappings *storageTypes.IDMappingOptions, userns Namespace, g *generate.Generator) (string, error) {
	// User
	var user string
	switch userns.NSMode {
	case Path:
		if err := fileutils.Exists(userns.Value); err != nil {
			return user, fmt.Errorf("cannot find specified user namespace path: %w", err)
		}
		if err := g.AddOrReplaceLinuxNamespace(string(spec.UserNamespace), userns.Value); err != nil {
			return user, err
		}
	case Host:
		if err := g.RemoveLinuxNamespace(string(spec.UserNamespace)); err != nil {
			return user, err
		}
	case KeepID:
		opts, err := namespaces.UsernsMode(userns.String()).GetKeepIDOptions()
		if err != nil {
			return user, err
		}
		if opts.MaxSize != nil && !rootless.IsRootless() {
			return user, fmt.Errorf("cannot set max size for user namespace when not running rootless")
		}
		mappings, uid, gid, err := util.GetKeepIDMapping(opts)
		if err != nil {
			return user, err
		}
		idmappings = mappings
		g.SetProcessUID(uint32(uid))
		g.SetProcessGID(uint32(gid))
		g.AddProcessAdditionalGid(uint32(gid))
		user = fmt.Sprintf("%d:%d", uid, gid)
		if err := privateUserNamespace(idmappings, g); err != nil {
			return user, err
		}
	case NoMap:
		mappings, uid, gid, err := util.GetNoMapMapping()
		if err != nil {
			return user, err
		}
		idmappings = mappings
		g.SetProcessUID(uint32(uid))
		g.SetProcessGID(uint32(gid))
		g.AddProcessAdditionalGid(uint32(gid))
		user = fmt.Sprintf("%d:%d", uid, gid)
		if err := privateUserNamespace(idmappings, g); err != nil {
			return user, err
		}
	case Private:
		if err := privateUserNamespace(idmappings, g); err != nil {
			return user, err
		}
	}
	return user, nil
}

func privateUserNamespace(idmappings *storageTypes.IDMappingOptions, g *generate.Generator) error {
	if err := g.AddOrReplaceLinuxNamespace(string(spec.UserNamespace), ""); err != nil {
		return err
	}
	if idmappings == nil || (len(idmappings.UIDMap) == 0 && len(idmappings.GIDMap) == 0) {
		return errors.New("must provide at least one UID or GID mapping to configure a user namespace")
	}
	for _, uidmap := range idmappings.UIDMap {
		g.AddLinuxUIDMapping(uint32(uidmap.HostID), uint32(uidmap.ContainerID), uint32(uidmap.Size))
	}
	for _, gidmap := range idmappings.GIDMap {
		g.AddLinuxGIDMapping(uint32(gidmap.HostID), uint32(gidmap.ContainerID), uint32(gidmap.Size))
	}
	return nil
}
