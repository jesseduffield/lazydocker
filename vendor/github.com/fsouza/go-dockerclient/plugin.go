// Copyright 2018 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// PluginPrivilege represents a privilege for a plugin.
type PluginPrivilege struct {
	Name        string   `json:"Name,omitempty" yaml:"Name,omitempty" toml:"Name,omitempty"`
	Description string   `json:"Description,omitempty" yaml:"Description,omitempty" toml:"Description,omitempty"`
	Value       []string `json:"Value,omitempty" yaml:"Value,omitempty" toml:"Value,omitempty"`
}

// InstallPluginOptions specify parameters to the InstallPlugins function.
//
// See https://goo.gl/C4t7Tz for more details.
type InstallPluginOptions struct {
	Remote  string
	Name    string
	Plugins []PluginPrivilege `qs:"-"`

	Auth AuthConfiguration

	Context context.Context
}

// InstallPlugins installs a plugin or returns an error in case of failure.
//
// See https://goo.gl/C4t7Tz for more details.
func (c *Client) InstallPlugins(opts InstallPluginOptions) error {
	headers, err := headersWithAuth(opts.Auth)
	if err != nil {
		return err
	}

	path := "/plugins/pull?" + queryString(opts)
	resp, err := c.do(http.MethodPost, path, doOptions{
		data:    opts.Plugins,
		context: opts.Context,
		headers: headers,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// PullPlugin streams back the progress of the pull, we must consume the whole body
	// otherwise the pull will be canceled on the engine.
	if _, err := io.ReadAll(resp.Body); err != nil {
		return err
	}
	return nil
}

// PluginSettings stores plugin settings.
//
// See https://goo.gl/C4t7Tz for more details.
type PluginSettings struct {
	Env     []string `json:"Env,omitempty" yaml:"Env,omitempty" toml:"Env,omitempty"`
	Args    []string `json:"Args,omitempty" yaml:"Args,omitempty" toml:"Args,omitempty"`
	Devices []string `json:"Devices,omitempty" yaml:"Devices,omitempty" toml:"Devices,omitempty"`
}

// PluginInterface stores plugin interface.
//
// See https://goo.gl/C4t7Tz for more details.
type PluginInterface struct {
	Types  []string `json:"Types,omitempty" yaml:"Types,omitempty" toml:"Types,omitempty"`
	Socket string   `json:"Socket,omitempty" yaml:"Socket,omitempty" toml:"Socket,omitempty"`
}

// PluginNetwork stores plugin network type.
//
// See https://goo.gl/C4t7Tz for more details.
type PluginNetwork struct {
	Type string `json:"Type,omitempty" yaml:"Type,omitempty" toml:"Type,omitempty"`
}

// PluginLinux stores plugin linux setting.
//
// See https://goo.gl/C4t7Tz for more details.
type PluginLinux struct {
	Capabilities    []string             `json:"Capabilities,omitempty" yaml:"Capabilities,omitempty" toml:"Capabilities,omitempty"`
	AllowAllDevices bool                 `json:"AllowAllDevices,omitempty" yaml:"AllowAllDevices,omitempty" toml:"AllowAllDevices,omitempty"`
	Devices         []PluginLinuxDevices `json:"Devices,omitempty" yaml:"Devices,omitempty" toml:"Devices,omitempty"`
}

// PluginLinuxDevices stores plugin linux device setting.
//
// See https://goo.gl/C4t7Tz for more details.
type PluginLinuxDevices struct {
	Name        string   `json:"Name,omitempty" yaml:"Name,omitempty" toml:"Name,omitempty"`
	Description string   `json:"Documentation,omitempty" yaml:"Documentation,omitempty" toml:"Documentation,omitempty"`
	Settable    []string `json:"Settable,omitempty" yaml:"Settable,omitempty" toml:"Settable,omitempty"`
	Path        string   `json:"Path,omitempty" yaml:"Path,omitempty" toml:"Path,omitempty"`
}

// PluginEnv stores plugin environment.
//
// See https://goo.gl/C4t7Tz for more details.
type PluginEnv struct {
	Name        string   `json:"Name,omitempty" yaml:"Name,omitempty" toml:"Name,omitempty"`
	Description string   `json:"Description,omitempty" yaml:"Description,omitempty" toml:"Description,omitempty"`
	Settable    []string `json:"Settable,omitempty" yaml:"Settable,omitempty" toml:"Settable,omitempty"`
	Value       string   `json:"Value,omitempty" yaml:"Value,omitempty" toml:"Value,omitempty"`
}

// PluginArgs stores plugin arguments.
//
// See https://goo.gl/C4t7Tz for more details.
type PluginArgs struct {
	Name        string   `json:"Name,omitempty" yaml:"Name,omitempty" toml:"Name,omitempty"`
	Description string   `json:"Description,omitempty" yaml:"Description,omitempty" toml:"Description,omitempty"`
	Settable    []string `json:"Settable,omitempty" yaml:"Settable,omitempty" toml:"Settable,omitempty"`
	Value       []string `json:"Value,omitempty" yaml:"Value,omitempty" toml:"Value,omitempty"`
}

// PluginUser stores plugin user.
//
// See https://goo.gl/C4t7Tz for more details.
type PluginUser struct {
	UID int32 `json:"UID,omitempty" yaml:"UID,omitempty" toml:"UID,omitempty"`
	GID int32 `json:"GID,omitempty" yaml:"GID,omitempty" toml:"GID,omitempty"`
}

// PluginConfig stores plugin config.
//
// See https://goo.gl/C4t7Tz for more details.
type PluginConfig struct {
	Description     string `json:"Description,omitempty" yaml:"Description,omitempty" toml:"Description,omitempty"`
	Documentation   string
	Interface       PluginInterface `json:"Interface,omitempty" yaml:"Interface,omitempty" toml:"Interface,omitempty"`
	Entrypoint      []string        `json:"Entrypoint,omitempty" yaml:"Entrypoint,omitempty" toml:"Entrypoint,omitempty"`
	WorkDir         string          `json:"WorkDir,omitempty" yaml:"WorkDir,omitempty" toml:"WorkDir,omitempty"`
	User            PluginUser      `json:"User,omitempty" yaml:"User,omitempty" toml:"User,omitempty"`
	Network         PluginNetwork   `json:"Network,omitempty" yaml:"Network,omitempty" toml:"Network,omitempty"`
	Linux           PluginLinux     `json:"Linux,omitempty" yaml:"Linux,omitempty" toml:"Linux,omitempty"`
	PropagatedMount string          `json:"PropagatedMount,omitempty" yaml:"PropagatedMount,omitempty" toml:"PropagatedMount,omitempty"`
	Mounts          []Mount         `json:"Mounts,omitempty" yaml:"Mounts,omitempty" toml:"Mounts,omitempty"`
	Env             []PluginEnv     `json:"Env,omitempty" yaml:"Env,omitempty" toml:"Env,omitempty"`
	Args            PluginArgs      `json:"Args,omitempty" yaml:"Args,omitempty" toml:"Args,omitempty"`
}

// PluginDetail specify results from the ListPlugins function.
//
// See https://goo.gl/C4t7Tz for more details.
type PluginDetail struct {
	ID       string         `json:"Id,omitempty" yaml:"Id,omitempty" toml:"Id,omitempty"`
	Name     string         `json:"Name,omitempty" yaml:"Name,omitempty" toml:"Name,omitempty"`
	Tag      string         `json:"Tag,omitempty" yaml:"Tag,omitempty" toml:"Tag,omitempty"`
	Active   bool           `json:"Enabled,omitempty" yaml:"Active,omitempty" toml:"Active,omitempty"`
	Settings PluginSettings `json:"Settings,omitempty" yaml:"Settings,omitempty" toml:"Settings,omitempty"`
	Config   PluginConfig   `json:"Config,omitempty" yaml:"Config,omitempty" toml:"Config,omitempty"`
}

// ListPlugins returns pluginDetails or an error.
//
// See https://goo.gl/C4t7Tz for more details.
func (c *Client) ListPlugins(ctx context.Context) ([]PluginDetail, error) {
	resp, err := c.do(http.MethodGet, "/plugins", doOptions{
		context: ctx,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	pluginDetails := make([]PluginDetail, 0)
	if err := json.NewDecoder(resp.Body).Decode(&pluginDetails); err != nil {
		return nil, err
	}
	return pluginDetails, nil
}

// ListFilteredPluginsOptions specify parameters to the ListFilteredPlugins function.
//
// See https://goo.gl/C4t7Tz for more details.
type ListFilteredPluginsOptions struct {
	Filters map[string][]string
	Context context.Context
}

// ListFilteredPlugins returns pluginDetails or an error.
//
// See https://goo.gl/rmdmWg for more details.
func (c *Client) ListFilteredPlugins(opts ListFilteredPluginsOptions) ([]PluginDetail, error) {
	path := "/plugins/json?" + queryString(opts)
	resp, err := c.do(http.MethodGet, path, doOptions{
		context: opts.Context,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	pluginDetails := make([]PluginDetail, 0)
	if err := json.NewDecoder(resp.Body).Decode(&pluginDetails); err != nil {
		return nil, err
	}
	return pluginDetails, nil
}

// GetPluginPrivileges returns pluginPrivileges or an error.
//
// See https://goo.gl/C4t7Tz for more details.
func (c *Client) GetPluginPrivileges(remote string, ctx context.Context) ([]PluginPrivilege, error) {
	return c.GetPluginPrivilegesWithOptions(
		GetPluginPrivilegesOptions{
			Remote:  remote,
			Context: ctx,
		})
}

// GetPluginPrivilegesOptions specify parameters to the GetPluginPrivilegesWithOptions function.
//
// See https://goo.gl/C4t7Tz for more details.
type GetPluginPrivilegesOptions struct {
	Remote  string
	Auth    AuthConfiguration
	Context context.Context
}

// GetPluginPrivilegesWithOptions returns pluginPrivileges or an error.
//
// See https://goo.gl/C4t7Tz for more details.
func (c *Client) GetPluginPrivilegesWithOptions(opts GetPluginPrivilegesOptions) ([]PluginPrivilege, error) {
	headers, err := headersWithAuth(opts.Auth)
	if err != nil {
		return nil, err
	}

	path := "/plugins/privileges?" + queryString(opts)
	resp, err := c.do(http.MethodGet, path, doOptions{
		context: opts.Context,
		headers: headers,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var pluginPrivileges []PluginPrivilege
	if err := json.NewDecoder(resp.Body).Decode(&pluginPrivileges); err != nil {
		return nil, err
	}
	return pluginPrivileges, nil
}

// InspectPlugins returns a pluginDetail or an error.
//
// See https://goo.gl/C4t7Tz for more details.
func (c *Client) InspectPlugins(name string, ctx context.Context) (*PluginDetail, error) {
	resp, err := c.do(http.MethodGet, "/plugins/"+name+"/json", doOptions{
		context: ctx,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, &NoSuchPlugin{ID: name}
		}
		return nil, err
	}
	defer resp.Body.Close()
	var pluginDetail PluginDetail
	if err := json.NewDecoder(resp.Body).Decode(&pluginDetail); err != nil {
		return nil, err
	}
	return &pluginDetail, nil
}

// RemovePluginOptions specify parameters to the RemovePlugin function.
//
// See https://goo.gl/C4t7Tz for more details.
type RemovePluginOptions struct {
	// The Name of the plugin.
	Name string `qs:"-"`

	Force   bool `qs:"force"`
	Context context.Context
}

// RemovePlugin returns a PluginDetail or an error.
//
// See https://goo.gl/C4t7Tz for more details.
func (c *Client) RemovePlugin(opts RemovePluginOptions) (*PluginDetail, error) {
	path := "/plugins/" + opts.Name + "?" + queryString(opts)
	resp, err := c.do(http.MethodDelete, path, doOptions{context: opts.Context})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, &NoSuchPlugin{ID: opts.Name}
		}
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if len(body) == 0 {
		// Seems like newer docker versions won't return the plugindetail after removal
		return nil, nil
	}

	var pluginDetail PluginDetail
	if err := json.Unmarshal(body, &pluginDetail); err != nil {
		return nil, err
	}
	return &pluginDetail, nil
}

// EnablePluginOptions specify parameters to the EnablePlugin function.
//
// See https://goo.gl/C4t7Tz for more details.
type EnablePluginOptions struct {
	// The Name of the plugin.
	Name    string `qs:"-"`
	Timeout int64  `qs:"timeout"`

	Context context.Context
}

// EnablePlugin enables plugin that opts point or returns an error.
//
// See https://goo.gl/C4t7Tz for more details.
func (c *Client) EnablePlugin(opts EnablePluginOptions) error {
	path := "/plugins/" + opts.Name + "/enable?" + queryString(opts)
	resp, err := c.do(http.MethodPost, path, doOptions{context: opts.Context})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// DisablePluginOptions specify parameters to the DisablePlugin function.
//
// See https://goo.gl/C4t7Tz for more details.
type DisablePluginOptions struct {
	// The Name of the plugin.
	Name string `qs:"-"`

	Context context.Context
}

// DisablePlugin disables plugin that opts point or returns an error.
//
// See https://goo.gl/C4t7Tz for more details.
func (c *Client) DisablePlugin(opts DisablePluginOptions) error {
	path := "/plugins/" + opts.Name + "/disable"
	resp, err := c.do(http.MethodPost, path, doOptions{context: opts.Context})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// CreatePluginOptions specify parameters to the CreatePlugin function.
//
// See https://goo.gl/C4t7Tz for more details.
type CreatePluginOptions struct {
	// The Name of the plugin.
	Name string `qs:"name"`
	// Path to tar containing plugin
	Path string `qs:"-"`

	Context context.Context
}

// CreatePlugin creates plugin that opts point or returns an error.
//
// See https://goo.gl/C4t7Tz for more details.
func (c *Client) CreatePlugin(opts CreatePluginOptions) (string, error) {
	path := "/plugins/create?" + queryString(opts)
	resp, err := c.do(http.MethodPost, path, doOptions{
		data:    opts.Path,
		context: opts.Context,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	containerNameBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(containerNameBytes), nil
}

// PushPluginOptions specify parameters to PushPlugin function.
//
// See https://goo.gl/C4t7Tz for more details.
type PushPluginOptions struct {
	// The Name of the plugin.
	Name string

	Context context.Context
}

// PushPlugin pushes plugin that opts point or returns an error.
//
// See https://goo.gl/C4t7Tz for more details.
func (c *Client) PushPlugin(opts PushPluginOptions) error {
	path := "/plugins/" + opts.Name + "/push"
	resp, err := c.do(http.MethodPost, path, doOptions{context: opts.Context})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ConfigurePluginOptions specify parameters to the ConfigurePlugin
//
// See https://goo.gl/C4t7Tz for more details.
type ConfigurePluginOptions struct {
	// The Name of the plugin.
	Name string `qs:"name"`
	Envs []string

	Context context.Context
}

// ConfigurePlugin configures plugin that opts point or returns an error.
//
// See https://goo.gl/C4t7Tz for more details.
func (c *Client) ConfigurePlugin(opts ConfigurePluginOptions) error {
	path := "/plugins/" + opts.Name + "/set"
	resp, err := c.do(http.MethodPost, path, doOptions{
		data:    opts.Envs,
		context: opts.Context,
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return &NoSuchPlugin{ID: opts.Name}
		}
		return err
	}
	resp.Body.Close()
	return nil
}

// NoSuchPlugin is the error returned when a given plugin does not exist.
type NoSuchPlugin struct {
	ID  string
	Err error
}

func (err *NoSuchPlugin) Error() string {
	if err.Err != nil {
		return err.Err.Error()
	}
	return "No such plugin: " + err.ID
}
