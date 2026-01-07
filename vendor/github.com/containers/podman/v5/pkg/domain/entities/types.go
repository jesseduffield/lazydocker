package entities

import (
	"net"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/libpod/events"
	entitiesTypes "github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/containers/podman/v5/pkg/specgen"
	"go.podman.io/common/libnetwork/types"
	"go.podman.io/storage/pkg/archive"
)

type Report struct {
	Id  []string
	Err map[string]error
}

type PodDeleteReport struct{ Report }

type (
	VolumeDeleteOptions struct{}
	VolumeDeleteReport  struct{ Report }
)

type NetFlags struct {
	AddHosts     []string `json:"add-host,omitempty"`
	DNS          []string `json:"dns,omitempty"`
	DNSOpt       []string `json:"dns-opt,omitempty"`
	DNDSearch    []string `json:"dns-search,omitempty"`
	MacAddr      string   `json:"mac-address,omitempty"`
	Publish      []string `json:"publish,omitempty"`
	IP           string   `json:"ip,omitempty"`
	NoHostname   bool     `json:"no-hostname,omitempty"`
	NoHosts      bool     `json:"no-hosts,omitempty"`
	Network      string   `json:"network,omitempty"`
	NetworkAlias []string `json:"network-alias,omitempty"`
}

// NetOptions reflect the shared network options between
// pods and containers
type NetOptions struct {
	AddHosts           []string                           `json:"hostadd,omitempty"`
	Aliases            []string                           `json:"network_alias,omitempty"`
	Networks           map[string]types.PerNetworkOptions `json:"networks,omitempty"`
	UseImageResolvConf bool                               `json:"no_manage_resolv_conf,omitempty"`
	DNSOptions         []string                           `json:"dns_option,omitempty"`
	DNSSearch          []string                           `json:"dns_search,omitempty"`
	DNSServers         []net.IP                           `json:"dns_server,omitempty"`
	HostsFile          string                             `json:"hosts_file,omitempty"`
	Network            specgen.Namespace                  `json:"netns"`
	NoHostname         bool                               `json:"no_manage_hostname,omitempty"`
	NoHosts            bool                               `json:"no_manage_hosts,omitempty"`
	PublishPorts       []types.PortMapping                `json:"portmappings,omitempty"`
	// NetworkOptions are additional options for each network
	NetworkOptions map[string][]string `json:"network_options,omitempty"`
}

// InspectOptions all CLI inspect commands and inspect sub-commands use the same options
type InspectOptions struct {
	// Format - change the output to JSON or a Go template.
	Format string `json:",omitempty"`
	// Latest - inspect the latest container Podman is aware of.
	Latest bool `json:",omitempty"`
	// Size (containers only) - display total file size.
	Size bool `json:",omitempty"`
	// Type -- return JSON for specified type.
	Type string `json:",omitempty"`
	// All -- inspect all
	All bool `json:",omitempty"`
}

// DiffOptions all API and CLI diff commands and diff sub-commands use the same options
type DiffOptions struct {
	Format string          `json:",omitempty"` // CLI only
	Latest bool            `json:",omitempty"` // API and CLI, only supported by containers
	Type   define.DiffType // Type which should be compared
}

// DiffReport provides changes for object
type DiffReport struct {
	Changes []archive.Change
}

type EventsOptions struct {
	FromStart bool
	EventChan chan events.ReadResult
	Filter    []string
	Stream    bool
	Since     string
	Until     string
}

// ContainerCreateResponse is the response struct for creating a container
type ContainerCreateResponse = entitiesTypes.ContainerCreateResponse

// BuildOptions describe the options for building container images.
type BuildOptions = entitiesTypes.BuildOptions

// BuildReport is the image-build report.
type BuildReport = entitiesTypes.BuildReport

// FarmBuildOptions describes the options for building container images on farm nodes
type FarmBuildOptions = entitiesTypes.FarmBuildOptions

type IDOrNameResponse struct {
	// The Id or Name of an object
	IDOrName string
}

type IDResponse = entitiesTypes.IDResponse
