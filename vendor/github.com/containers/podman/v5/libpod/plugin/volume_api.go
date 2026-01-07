package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/docker/go-plugins-helpers/sdk"
	"github.com/docker/go-plugins-helpers/volume"
	jsoniter "github.com/json-iterator/go"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/config"
	"go.podman.io/storage/pkg/fileutils"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

// Copied from docker/go-plugins-helpers/volume/api.go - not exported, so we
// need to do this to get at them.
// These are well-established paths that should not change unless the plugin API
// version changes.
var (
	activatePath    = "/Plugin.Activate"
	createPath      = "/VolumeDriver.Create"
	getPath         = "/VolumeDriver.Get"
	listPath        = "/VolumeDriver.List"
	removePath      = "/VolumeDriver.Remove"
	hostVirtualPath = "/VolumeDriver.Path"
	mountPath       = "/VolumeDriver.Mount"
	unmountPath     = "/VolumeDriver.Unmount"
)

const (
	volumePluginType = "VolumeDriver"
)

var (
	ErrNotPlugin       = errors.New("target does not appear to be a valid plugin")
	ErrNotVolumePlugin = errors.New("plugin is not a volume plugin")
	ErrPluginRemoved   = errors.New("plugin is no longer available (shut down?)")

	// This stores available, initialized volume plugins.
	pluginsLock sync.Mutex
	plugins     map[string]*VolumePlugin
)

// VolumePlugin is a single volume plugin.
type VolumePlugin struct {
	// Name is the name of the volume plugin. This will be used to refer to
	// it.
	Name string
	// SocketPath is the unix socket at which the plugin is accessed.
	SocketPath string
	// Client is the HTTP client we use to connect to the plugin.
	Client *http.Client
}

// This is the response from the activate endpoint of the API.
type activateResponse struct {
	Implements []string
}

// Validate that the given plugin is good to use.
// Add it to available plugins if so.
func validatePlugin(newPlugin *VolumePlugin) error {
	// It's a socket. Is it a plugin?
	// Hit the Activate endpoint to find out if it is, and if so what kind
	req, err := http.NewRequest(http.MethodPost, "http://plugin"+activatePath, nil)
	if err != nil {
		return fmt.Errorf("making request to volume plugin %s activation endpoint: %w", newPlugin.Name, err)
	}

	req.Header.Set("Host", newPlugin.getURI())
	req.Header.Set("Content-Type", sdk.DefaultContentTypeV1_1)

	resp, err := newPlugin.Client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request to plugin %s activation endpoint: %w", newPlugin.Name, err)
	}
	defer resp.Body.Close()

	// Response code MUST be 200. Anything else, we have to assume it's not
	// a valid plugin.
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("got status code %d from activation endpoint for plugin %s: %w", resp.StatusCode, newPlugin.Name, ErrNotPlugin)
	}

	// Read and decode the body so we can tell if this is a volume plugin.
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading activation response body from plugin %s: %w", newPlugin.Name, err)
	}

	respStruct := new(activateResponse)
	if err := json.Unmarshal(respBytes, respStruct); err != nil {
		return fmt.Errorf("unmarshalling plugin %s activation response: %w", newPlugin.Name, err)
	}

	if !slices.Contains(respStruct.Implements, volumePluginType) {
		return fmt.Errorf("plugin %s does not implement volume plugin, instead provides %s: %w", newPlugin.Name, strings.Join(respStruct.Implements, ", "), ErrNotVolumePlugin)
	}

	if plugins == nil {
		plugins = make(map[string]*VolumePlugin)
	}

	plugins[newPlugin.Name] = newPlugin

	return nil
}

// GetVolumePlugin gets a single volume plugin, with the given name, at the
// given path.
func GetVolumePlugin(name string, path string, timeout *uint, cfg *config.Config) (*VolumePlugin, error) {
	pluginsLock.Lock()
	defer pluginsLock.Unlock()

	plugin, exists := plugins[name]
	if exists {
		// This shouldn't be possible, but just in case...
		if plugin.SocketPath != filepath.Clean(path) {
			return nil, fmt.Errorf("requested path %q for volume plugin %s does not match pre-existing path for plugin, %q: %w", path, name, plugin.SocketPath, define.ErrInvalidArg)
		}

		return plugin, nil
	}

	// It's not cached. We need to get it.

	newPlugin := new(VolumePlugin)
	newPlugin.Name = name
	newPlugin.SocketPath = filepath.Clean(path)

	// Need an HTTP client to force a Unix connection.
	// And since we can reuse it, might as well cache it.
	client := new(http.Client)
	client.Timeout = 5 * time.Second
	if timeout != nil {
		client.Timeout = time.Duration(*timeout) * time.Second
	} else if cfg != nil {
		client.Timeout = time.Duration(cfg.Engine.VolumePluginTimeout) * time.Second
	}
	// This bit borrowed from pkg/bindings/connection.go
	client.Transport = &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", newPlugin.SocketPath)
		},
		DisableCompression: true,
	}
	newPlugin.Client = client

	stat, err := os.Stat(newPlugin.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("cannot access plugin %s socket %q: %w", name, newPlugin.SocketPath, err)
	}
	if stat.Mode()&os.ModeSocket == 0 {
		return nil, fmt.Errorf("volume %s path %q is not a unix socket: %w", name, newPlugin.SocketPath, ErrNotPlugin)
	}

	if err := validatePlugin(newPlugin); err != nil {
		return nil, err
	}

	return newPlugin, nil
}

func (p *VolumePlugin) getURI() string {
	return "unix://" + p.SocketPath
}

// Verify the plugin is still available.
// Does not actually ping the API, just verifies that the socket still exists.
func (p *VolumePlugin) verifyReachable() error {
	if err := fileutils.Exists(p.SocketPath); err != nil {
		if os.IsNotExist(err) {
			pluginsLock.Lock()
			defer pluginsLock.Unlock()
			delete(plugins, p.Name)
			return fmt.Errorf("%s: %w", p.Name, ErrPluginRemoved)
		}

		return fmt.Errorf("accessing plugin %s: %w", p.Name, err)
	}
	return nil
}

// Send a request to the volume plugin for handling.
// Callers *MUST* close the response when they are done.
func (p *VolumePlugin) sendRequest(toJSON any, endpoint string) (*http.Response, error) {
	var (
		reqJSON []byte
		err     error
	)

	if toJSON != nil {
		reqJSON, err = json.Marshal(toJSON)
		if err != nil {
			return nil, fmt.Errorf("marshalling request JSON for volume plugin %s endpoint %s: %w", p.Name, endpoint, err)
		}
	}

	req, err := http.NewRequest(http.MethodPost, "http://plugin"+endpoint, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("making request to volume plugin %s endpoint %s: %w", p.Name, endpoint, err)
	}

	req.Header.Set("Host", p.getURI())
	req.Header.Set("Content-Type", sdk.DefaultContentTypeV1_1)

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request to volume plugin %s endpoint %s: %w", p.Name, endpoint, err)
	}
	// We are *deliberately not closing* response here. It is the
	// responsibility of the caller to do so after reading the response.

	return resp, nil
}

// Turn an error response from a volume plugin into a well-formatted Go error.
func (p *VolumePlugin) makeErrorResponse(err, endpoint, volName string) error {
	if err == "" {
		err = "empty error from plugin"
	}
	if volName != "" {
		return fmt.Errorf("on %s on volume %s in volume plugin %s: %w", endpoint, volName, p.Name, errors.New(err))
	}
	return fmt.Errorf("on %s in volume plugin %s: %w", endpoint, p.Name, errors.New(err))
}

// Handle error responses from plugin
func (p *VolumePlugin) handleErrorResponse(resp *http.Response, endpoint, volName string) error {
	// The official plugin reference implementation uses HTTP 500 for
	// errors, but I don't think we can guarantee all plugins do that.
	// Let's interpret anything other than 200 as an error.
	// If there isn't an error, don't even bother decoding the response.
	if resp.StatusCode != http.StatusOK {
		errResp, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("reading response body from volume plugin %s: %w", p.Name, err)
		}

		errStruct := new(volume.ErrorResponse)
		if err := json.Unmarshal(errResp, errStruct); err != nil {
			return fmt.Errorf("unmarshalling JSON response from volume plugin %s: %w", p.Name, err)
		}

		return p.makeErrorResponse(errStruct.Err, endpoint, volName)
	}

	return nil
}

// CreateVolume creates a volume in the plugin.
func (p *VolumePlugin) CreateVolume(req *volume.CreateRequest) error {
	if req == nil {
		return fmt.Errorf("must provide non-nil request to CreateVolume: %w", define.ErrInvalidArg)
	}

	if err := p.verifyReachable(); err != nil {
		return err
	}

	logrus.Infof("Creating volume %s using plugin %s", req.Name, p.Name)

	resp, err := p.sendRequest(req, createPath)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return p.handleErrorResponse(resp, createPath, req.Name)
}

// ListVolumes lists volumes available in the plugin.
func (p *VolumePlugin) ListVolumes() ([]*volume.Volume, error) {
	if err := p.verifyReachable(); err != nil {
		return nil, err
	}

	logrus.Infof("Listing volumes using plugin %s", p.Name)

	resp, err := p.sendRequest(nil, listPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := p.handleErrorResponse(resp, listPath, ""); err != nil {
		return nil, err
	}

	volumeRespBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body from volume plugin %s: %w", p.Name, err)
	}

	volumeResp := new(volume.ListResponse)
	if err := json.Unmarshal(volumeRespBytes, volumeResp); err != nil {
		return nil, fmt.Errorf("unmarshalling volume plugin %s list response: %w", p.Name, err)
	}

	return volumeResp.Volumes, nil
}

// GetVolume gets a single volume from the plugin.
func (p *VolumePlugin) GetVolume(req *volume.GetRequest) (*volume.Volume, error) {
	if req == nil {
		return nil, fmt.Errorf("must provide non-nil request to GetVolume: %w", define.ErrInvalidArg)
	}

	if err := p.verifyReachable(); err != nil {
		return nil, err
	}

	logrus.Infof("Getting volume %s using plugin %s", req.Name, p.Name)

	resp, err := p.sendRequest(req, getPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := p.handleErrorResponse(resp, getPath, req.Name); err != nil {
		return nil, err
	}

	getRespBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body from volume plugin %s: %w", p.Name, err)
	}

	getResp := new(volume.GetResponse)
	if err := json.Unmarshal(getRespBytes, getResp); err != nil {
		return nil, fmt.Errorf("unmarshalling volume plugin %s get response: %w", p.Name, err)
	}

	return getResp.Volume, nil
}

// RemoveVolume removes a single volume from the plugin.
func (p *VolumePlugin) RemoveVolume(req *volume.RemoveRequest) error {
	if req == nil {
		return fmt.Errorf("must provide non-nil request to RemoveVolume: %w", define.ErrInvalidArg)
	}

	if err := p.verifyReachable(); err != nil {
		return err
	}

	logrus.Infof("Removing volume %s using plugin %s", req.Name, p.Name)

	resp, err := p.sendRequest(req, removePath)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return p.handleErrorResponse(resp, removePath, req.Name)
}

// GetVolumePath gets the path the given volume is mounted at.
func (p *VolumePlugin) GetVolumePath(req *volume.PathRequest) (string, error) {
	if req == nil {
		return "", fmt.Errorf("must provide non-nil request to GetVolumePath: %w", define.ErrInvalidArg)
	}

	if err := p.verifyReachable(); err != nil {
		return "", err
	}

	logrus.Infof("Getting volume %s path using plugin %s", req.Name, p.Name)

	resp, err := p.sendRequest(req, hostVirtualPath)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if err := p.handleErrorResponse(resp, hostVirtualPath, req.Name); err != nil {
		return "", err
	}

	pathRespBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response body from volume plugin %s: %w", p.Name, err)
	}

	pathResp := new(volume.PathResponse)
	if err := json.Unmarshal(pathRespBytes, pathResp); err != nil {
		return "", fmt.Errorf("unmarshalling volume plugin %s path response: %w", p.Name, err)
	}

	return pathResp.Mountpoint, nil
}

// MountVolume mounts the given volume. The ID argument is the ID of the
// mounting container, used for internal record-keeping by the plugin. Returns
// the path the volume has been mounted at.
func (p *VolumePlugin) MountVolume(req *volume.MountRequest) (string, error) {
	if req == nil {
		return "", fmt.Errorf("must provide non-nil request to MountVolume: %w", define.ErrInvalidArg)
	}

	if err := p.verifyReachable(); err != nil {
		return "", err
	}

	logrus.Infof("Mounting volume %s using plugin %s for container %s", req.Name, p.Name, req.ID)

	resp, err := p.sendRequest(req, mountPath)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if err := p.handleErrorResponse(resp, mountPath, req.Name); err != nil {
		return "", err
	}

	mountRespBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response body from volume plugin %s: %w", p.Name, err)
	}

	mountResp := new(volume.MountResponse)
	if err := json.Unmarshal(mountRespBytes, mountResp); err != nil {
		return "", fmt.Errorf("unmarshalling volume plugin %s path response: %w", p.Name, err)
	}

	return mountResp.Mountpoint, nil
}

// UnmountVolume unmounts the given volume. The ID argument is the ID of the
// container that is unmounting, used for internal record-keeping by the plugin.
func (p *VolumePlugin) UnmountVolume(req *volume.UnmountRequest) error {
	if req == nil {
		return fmt.Errorf("must provide non-nil request to UnmountVolume: %w", define.ErrInvalidArg)
	}

	if err := p.verifyReachable(); err != nil {
		return err
	}

	logrus.Infof("Unmounting volume %s using plugin %s for container %s", req.Name, p.Name, req.ID)

	resp, err := p.sendRequest(req, unmountPath)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return p.handleErrorResponse(resp, unmountPath, req.Name)
}
