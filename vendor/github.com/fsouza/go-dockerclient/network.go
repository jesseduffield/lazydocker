// Copyright 2015 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// ErrNetworkAlreadyExists is the error returned by CreateNetwork when the
// network already exists.
var ErrNetworkAlreadyExists = errors.New("network already exists")

// Network represents a network.
//
// See https://goo.gl/6GugX3 for more details.
type Network struct {
	Name       string
	ID         string `json:"Id"`
	Scope      string
	Driver     string
	IPAM       IPAMOptions
	Containers map[string]Endpoint
	Options    map[string]string
	Internal   bool
	EnableIPv6 bool `json:"EnableIPv6"`
	Labels     map[string]string
}

// Endpoint contains network resources allocated and used for a container in a network
//
// See https://goo.gl/6GugX3 for more details.
type Endpoint struct {
	Name        string
	ID          string `json:"EndpointID"`
	MacAddress  string
	IPv4Address string
	IPv6Address string
}

// ListNetworks returns all networks.
//
// See https://goo.gl/6GugX3 for more details.
func (c *Client) ListNetworks() ([]Network, error) {
	resp, err := c.do(http.MethodGet, "/networks", doOptions{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var networks []Network
	if err := json.NewDecoder(resp.Body).Decode(&networks); err != nil {
		return nil, err
	}
	return networks, nil
}

// NetworkFilterOpts is an aggregation of key=value that Docker
// uses to filter networks
type NetworkFilterOpts map[string]map[string]bool

// FilteredListNetworks returns all networks with the filters applied
//
// See goo.gl/zd2mx4 for more details.
func (c *Client) FilteredListNetworks(opts NetworkFilterOpts) ([]Network, error) {
	params, err := json.Marshal(opts)
	if err != nil {
		return nil, err
	}
	qs := make(url.Values)
	qs.Add("filters", string(params))
	path := "/networks?" + qs.Encode()
	resp, err := c.do(http.MethodGet, path, doOptions{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var networks []Network
	if err := json.NewDecoder(resp.Body).Decode(&networks); err != nil {
		return nil, err
	}
	return networks, nil
}

// NetworkInfo returns information about a network by its ID.
//
// See https://goo.gl/6GugX3 for more details.
func (c *Client) NetworkInfo(id string) (*Network, error) {
	path := "/networks/" + id
	resp, err := c.do(http.MethodGet, path, doOptions{})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, &NoSuchNetwork{ID: id}
		}
		return nil, err
	}
	defer resp.Body.Close()
	var network Network
	if err := json.NewDecoder(resp.Body).Decode(&network); err != nil {
		return nil, err
	}
	return &network, nil
}

// CreateNetworkOptions specify parameters to the CreateNetwork function and
// (for now) is the expected body of the "create network" http request message
//
// See https://goo.gl/6GugX3 for more details.
type CreateNetworkOptions struct {
	Name           string             `json:"Name" yaml:"Name" toml:"Name"`
	Driver         string             `json:"Driver" yaml:"Driver" toml:"Driver"`
	Scope          string             `json:"Scope" yaml:"Scope" toml:"Scope"`
	IPAM           *IPAMOptions       `json:"IPAM,omitempty" yaml:"IPAM" toml:"IPAM"`
	ConfigFrom     *NetworkConfigFrom `json:"ConfigFrom,omitempty" yaml:"ConfigFrom" toml:"ConfigFrom"`
	Options        map[string]any     `json:"Options" yaml:"Options" toml:"Options"`
	Labels         map[string]string  `json:"Labels" yaml:"Labels" toml:"Labels"`
	CheckDuplicate bool               `json:"CheckDuplicate" yaml:"CheckDuplicate" toml:"CheckDuplicate"`
	Internal       bool               `json:"Internal" yaml:"Internal" toml:"Internal"`
	EnableIPv6     bool               `json:"EnableIPv6" yaml:"EnableIPv6" toml:"EnableIPv6"`
	Attachable     bool               `json:"Attachable" yaml:"Attachable" toml:"Attachable"`
	ConfigOnly     bool               `json:"ConfigOnly" yaml:"ConfigOnly" toml:"ConfigOnly"`
	Ingress        bool               `json:"Ingress" yaml:"Ingress" toml:"Ingress"`
	Context        context.Context    `json:"-"`
}

// NetworkConfigFrom is used in network creation for specifying the source of a
// network configuration.
type NetworkConfigFrom struct {
	Network string `json:"Network" yaml:"Network" toml:"Network"`
}

// IPAMOptions controls IP Address Management when creating a network
//
// See https://goo.gl/T8kRVH for more details.
type IPAMOptions struct {
	Driver  string            `json:"Driver" yaml:"Driver" toml:"Driver"`
	Config  []IPAMConfig      `json:"Config" yaml:"Config" toml:"Config"`
	Options map[string]string `json:"Options" yaml:"Options" toml:"Options"`
}

// IPAMConfig represents IPAM configurations
//
// See https://goo.gl/T8kRVH for more details.
type IPAMConfig struct {
	Subnet     string            `json:",omitempty"`
	IPRange    string            `json:",omitempty"`
	Gateway    string            `json:",omitempty"`
	AuxAddress map[string]string `json:"AuxiliaryAddresses,omitempty"`
}

// CreateNetwork creates a new network, returning the network instance,
// or an error in case of failure.
//
// See https://goo.gl/6GugX3 for more details.
func (c *Client) CreateNetwork(opts CreateNetworkOptions) (*Network, error) {
	resp, err := c.do(
		http.MethodPost,
		"/networks/create",
		doOptions{
			data:    opts,
			context: opts.Context,
		},
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	type createNetworkResponse struct {
		ID string
	}
	var (
		network Network
		cnr     createNetworkResponse
	)
	if err := json.NewDecoder(resp.Body).Decode(&cnr); err != nil {
		return nil, err
	}

	network.Name = opts.Name
	network.ID = cnr.ID
	network.Driver = opts.Driver

	return &network, nil
}

// RemoveNetwork removes a network or returns an error in case of failure.
//
// See https://goo.gl/6GugX3 for more details.
func (c *Client) RemoveNetwork(id string) error {
	resp, err := c.do(http.MethodDelete, "/networks/"+id, doOptions{})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchNetwork{ID: id}
		}
		return err
	}
	resp.Body.Close()
	return nil
}

// NetworkConnectionOptions specify parameters to the ConnectNetwork and
// DisconnectNetwork function.
//
// See https://goo.gl/RV7BJU for more details.
type NetworkConnectionOptions struct {
	Container string

	// EndpointConfig is only applicable to the ConnectNetwork call
	EndpointConfig *EndpointConfig `json:"EndpointConfig,omitempty"`

	// Force is only applicable to the DisconnectNetwork call
	Force bool

	Context context.Context `json:"-"`
}

// EndpointConfig stores network endpoint details
//
// See https://goo.gl/RV7BJU for more details.
type EndpointConfig struct {
	IPAMConfig          *EndpointIPAMConfig `json:"IPAMConfig,omitempty" yaml:"IPAMConfig,omitempty" toml:"IPAMConfig,omitempty"`
	Links               []string            `json:"Links,omitempty" yaml:"Links,omitempty" toml:"Links,omitempty"`
	Aliases             []string            `json:"Aliases,omitempty" yaml:"Aliases,omitempty" toml:"Aliases,omitempty"`
	NetworkID           string              `json:"NetworkID,omitempty" yaml:"NetworkID,omitempty" toml:"NetworkID,omitempty"`
	EndpointID          string              `json:"EndpointID,omitempty" yaml:"EndpointID,omitempty" toml:"EndpointID,omitempty"`
	Gateway             string              `json:"Gateway,omitempty" yaml:"Gateway,omitempty" toml:"Gateway,omitempty"`
	IPAddress           string              `json:"IPAddress,omitempty" yaml:"IPAddress,omitempty" toml:"IPAddress,omitempty"`
	IPPrefixLen         int                 `json:"IPPrefixLen,omitempty" yaml:"IPPrefixLen,omitempty" toml:"IPPrefixLen,omitempty"`
	IPv6Gateway         string              `json:"IPv6Gateway,omitempty" yaml:"IPv6Gateway,omitempty" toml:"IPv6Gateway,omitempty"`
	GlobalIPv6Address   string              `json:"GlobalIPv6Address,omitempty" yaml:"GlobalIPv6Address,omitempty" toml:"GlobalIPv6Address,omitempty"`
	GlobalIPv6PrefixLen int                 `json:"GlobalIPv6PrefixLen,omitempty" yaml:"GlobalIPv6PrefixLen,omitempty" toml:"GlobalIPv6PrefixLen,omitempty"`
	MacAddress          string              `json:"MacAddress,omitempty" yaml:"MacAddress,omitempty" toml:"MacAddress,omitempty"`
	DriverOpts          map[string]string   `json:"DriverOpts,omitempty" yaml:"DriverOpts,omitempty" toml:"DriverOpts,omitempty"`
}

// EndpointIPAMConfig represents IPAM configurations for an
// endpoint
//
// See https://goo.gl/RV7BJU for more details.
type EndpointIPAMConfig struct {
	IPv4Address string `json:",omitempty" yaml:"IPv4Address,omitempty"`
	IPv6Address string `json:",omitempty" yaml:"IPv6Address,omitempty"`
}

// ConnectNetwork adds a container to a network or returns an error in case of
// failure.
//
// See https://goo.gl/6GugX3 for more details.
func (c *Client) ConnectNetwork(id string, opts NetworkConnectionOptions) error {
	resp, err := c.do(http.MethodPost, "/networks/"+id+"/connect", doOptions{
		data:    opts,
		context: opts.Context,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchNetworkOrContainer{NetworkID: id, ContainerID: opts.Container}
		}
		return err
	}
	resp.Body.Close()
	return nil
}

// DisconnectNetwork removes a container from a network or returns an error in
// case of failure.
//
// See https://goo.gl/6GugX3 for more details.
func (c *Client) DisconnectNetwork(id string, opts NetworkConnectionOptions) error {
	resp, err := c.do(http.MethodPost, "/networks/"+id+"/disconnect", doOptions{data: opts})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchNetworkOrContainer{NetworkID: id, ContainerID: opts.Container}
		}
		return err
	}
	resp.Body.Close()
	return nil
}

// PruneNetworksOptions specify parameters to the PruneNetworks function.
//
// See https://goo.gl/kX0S9h for more details.
type PruneNetworksOptions struct {
	Filters map[string][]string
	Context context.Context
}

// PruneNetworksResults specify results from the PruneNetworks function.
//
// See https://goo.gl/kX0S9h for more details.
type PruneNetworksResults struct {
	NetworksDeleted []string
}

// PruneNetworks deletes networks which are unused.
//
// See https://goo.gl/kX0S9h for more details.
func (c *Client) PruneNetworks(opts PruneNetworksOptions) (*PruneNetworksResults, error) {
	path := "/networks/prune?" + queryString(opts)
	resp, err := c.do(http.MethodPost, path, doOptions{context: opts.Context})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var results PruneNetworksResults
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}
	return &results, nil
}

// NoSuchNetwork is the error returned when a given network does not exist.
type NoSuchNetwork struct {
	ID string
}

func (err *NoSuchNetwork) Error() string {
	return fmt.Sprintf("No such network: %s", err.ID)
}

// NoSuchNetworkOrContainer is the error returned when a given network or
// container does not exist.
type NoSuchNetworkOrContainer struct {
	NetworkID   string
	ContainerID string
}

func (err *NoSuchNetworkOrContainer) Error() string {
	return fmt.Sprintf("No such network (%s) or container (%s)", err.NetworkID, err.ContainerID)
}
