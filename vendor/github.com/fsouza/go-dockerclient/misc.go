// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/docker/docker/api/types/swarm"
)

// Version returns version information about the docker server.
//
// See https://goo.gl/mU7yje for more details.
func (c *Client) Version() (*Env, error) {
	return c.VersionWithContext(context.TODO())
}

// VersionWithContext returns version information about the docker server.
func (c *Client) VersionWithContext(ctx context.Context) (*Env, error) {
	resp, err := c.do(http.MethodGet, "/version", doOptions{context: ctx})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var env Env
	if err := env.Decode(resp.Body); err != nil {
		return nil, err
	}
	return &env, nil
}

// DockerInfo contains information about the Docker server
//
// See https://goo.gl/bHUoz9 for more details.
type DockerInfo struct {
	ID                 string
	Containers         int
	ContainersRunning  int
	ContainersPaused   int
	ContainersStopped  int
	Images             int
	Driver             string
	DriverStatus       [][2]string
	SystemStatus       [][2]string
	Plugins            PluginsInfo
	NFd                int
	NGoroutines        int
	SystemTime         string
	ExecutionDriver    string
	LoggingDriver      string
	CgroupDriver       string
	NEventsListener    int
	KernelVersion      string
	OperatingSystem    string
	OSType             string
	Architecture       string
	IndexServerAddress string
	RegistryConfig     *ServiceConfig
	SecurityOptions    []string
	NCPU               int
	MemTotal           int64
	DockerRootDir      string
	HTTPProxy          string `json:"HttpProxy"`
	HTTPSProxy         string `json:"HttpsProxy"`
	NoProxy            string
	Name               string
	Labels             []string
	ServerVersion      string
	ClusterStore       string
	Runtimes           map[string]Runtime
	ClusterAdvertise   string
	Isolation          string
	InitBinary         string
	DefaultRuntime     string
	Swarm              swarm.Info
	LiveRestoreEnabled bool
	MemoryLimit        bool
	SwapLimit          bool
	KernelMemory       bool
	CPUCfsPeriod       bool `json:"CpuCfsPeriod"`
	CPUCfsQuota        bool `json:"CpuCfsQuota"`
	CPUShares          bool
	CPUSet             bool
	IPv4Forwarding     bool
	BridgeNfIptables   bool
	BridgeNfIP6tables  bool `json:"BridgeNfIp6tables"`
	Debug              bool
	OomKillDisable     bool
	ExperimentalBuild  bool
}

// Runtime describes an OCI runtime
//
// for more information, see: https://dockr.ly/2NKM8qq
type Runtime struct {
	Path string
	Args []string `json:"runtimeArgs"`
}

// PluginsInfo is a struct with the plugins registered with the docker daemon
//
// for more information, see: https://goo.gl/bHUoz9
type PluginsInfo struct {
	// List of Volume plugins registered
	Volume []string
	// List of Network plugins registered
	Network []string
	// List of Authorization plugins registered
	Authorization []string
}

// ServiceConfig stores daemon registry services configuration.
//
// for more information, see: https://goo.gl/7iFFDz
type ServiceConfig struct {
	InsecureRegistryCIDRs []*NetIPNet
	IndexConfigs          map[string]*IndexInfo
	Mirrors               []string
}

// NetIPNet is the net.IPNet type, which can be marshalled and
// unmarshalled to JSON.
//
// for more information, see: https://goo.gl/7iFFDz
type NetIPNet net.IPNet

// MarshalJSON returns the JSON representation of the IPNet.
func (ipnet *NetIPNet) MarshalJSON() ([]byte, error) {
	return json.Marshal((*net.IPNet)(ipnet).String())
}

// UnmarshalJSON sets the IPNet from a byte array of JSON.
func (ipnet *NetIPNet) UnmarshalJSON(b []byte) (err error) {
	var ipnetStr string
	if err = json.Unmarshal(b, &ipnetStr); err == nil {
		var cidr *net.IPNet
		if _, cidr, err = net.ParseCIDR(ipnetStr); err == nil {
			*ipnet = NetIPNet(*cidr)
		}
	}
	return
}

// IndexInfo contains information about a registry.
//
// for more information, see: https://goo.gl/7iFFDz
type IndexInfo struct {
	Name     string
	Mirrors  []string
	Secure   bool
	Official bool
}

// Info returns system-wide information about the Docker server.
//
// See https://goo.gl/ElTHi2 for more details.
func (c *Client) Info() (*DockerInfo, error) {
	resp, err := c.do(http.MethodGet, "/info", doOptions{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var info DockerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// ParseRepositoryTag gets the name of the repository and returns it splitted
// in two parts: the repository and the tag. It ignores the digest when it is
// present.
//
// Some examples:
//
//	localhost.localdomain:5000/samalba/hipache:latest -> localhost.localdomain:5000/samalba/hipache, latest
//	localhost.localdomain:5000/samalba/hipache -> localhost.localdomain:5000/samalba/hipache, ""
//	busybox:latest@sha256:4a731fb46adc5cefe3ae374a8b6020fc1b6ad667a279647766e9a3cd89f6fa92 -> busybox, latest
func ParseRepositoryTag(repoTag string) (repository string, tag string) {
	parts := strings.SplitN(repoTag, "@", 2)
	repoTag = parts[0]
	n := strings.LastIndex(repoTag, ":")
	if n < 0 {
		return repoTag, ""
	}
	if tag := repoTag[n+1:]; !strings.Contains(tag, "/") {
		return repoTag[:n], tag
	}
	return repoTag, ""
}
