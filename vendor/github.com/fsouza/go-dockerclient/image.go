// Copyright 2013 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// APIImages represent an image returned in the ListImages call.
type APIImages struct {
	ID          string            `json:"Id" yaml:"Id" toml:"Id"`
	RepoTags    []string          `json:"RepoTags,omitempty" yaml:"RepoTags,omitempty" toml:"RepoTags,omitempty"`
	Created     int64             `json:"Created,omitempty" yaml:"Created,omitempty" toml:"Created,omitempty"`
	Size        int64             `json:"Size,omitempty" yaml:"Size,omitempty" toml:"Size,omitempty"`
	VirtualSize int64             `json:"VirtualSize,omitempty" yaml:"VirtualSize,omitempty" toml:"VirtualSize,omitempty"`
	ParentID    string            `json:"ParentId,omitempty" yaml:"ParentId,omitempty" toml:"ParentId,omitempty"`
	RepoDigests []string          `json:"RepoDigests,omitempty" yaml:"RepoDigests,omitempty" toml:"RepoDigests,omitempty"`
	Labels      map[string]string `json:"Labels,omitempty" yaml:"Labels,omitempty" toml:"Labels,omitempty"`
}

// RootFS represents the underlying layers used by an image
type RootFS struct {
	Type   string   `json:"Type,omitempty" yaml:"Type,omitempty" toml:"Type,omitempty"`
	Layers []string `json:"Layers,omitempty" yaml:"Layers,omitempty" toml:"Layers,omitempty"`
}

// Image is the type representing a docker image and its various properties
type Image struct {
	ID              string    `json:"Id" yaml:"Id" toml:"Id"`
	RepoTags        []string  `json:"RepoTags,omitempty" yaml:"RepoTags,omitempty" toml:"RepoTags,omitempty"`
	Parent          string    `json:"Parent,omitempty" yaml:"Parent,omitempty" toml:"Parent,omitempty"`
	Comment         string    `json:"Comment,omitempty" yaml:"Comment,omitempty" toml:"Comment,omitempty"`
	Created         time.Time `json:"Created,omitempty" yaml:"Created,omitempty" toml:"Created,omitempty"`
	Container       string    `json:"Container,omitempty" yaml:"Container,omitempty" toml:"Container,omitempty"`
	ContainerConfig Config    `json:"ContainerConfig,omitempty" yaml:"ContainerConfig,omitempty" toml:"ContainerConfig,omitempty"`
	DockerVersion   string    `json:"DockerVersion,omitempty" yaml:"DockerVersion,omitempty" toml:"DockerVersion,omitempty"`
	Author          string    `json:"Author,omitempty" yaml:"Author,omitempty" toml:"Author,omitempty"`
	Config          *Config   `json:"Config,omitempty" yaml:"Config,omitempty" toml:"Config,omitempty"`
	Architecture    string    `json:"Architecture,omitempty" yaml:"Architecture,omitempty"`
	Size            int64     `json:"Size,omitempty" yaml:"Size,omitempty" toml:"Size,omitempty"`
	VirtualSize     int64     `json:"VirtualSize,omitempty" yaml:"VirtualSize,omitempty" toml:"VirtualSize,omitempty"`
	RepoDigests     []string  `json:"RepoDigests,omitempty" yaml:"RepoDigests,omitempty" toml:"RepoDigests,omitempty"`
	RootFS          *RootFS   `json:"RootFS,omitempty" yaml:"RootFS,omitempty" toml:"RootFS,omitempty"`
	OS              string    `json:"Os,omitempty" yaml:"Os,omitempty" toml:"Os,omitempty"`
}

// ImagePre012 serves the same purpose as the Image type except that it is for
// earlier versions of the Docker API (pre-012 to be specific)
type ImagePre012 struct {
	ID              string    `json:"id"`
	Parent          string    `json:"parent,omitempty"`
	Comment         string    `json:"comment,omitempty"`
	Created         time.Time `json:"created"`
	Container       string    `json:"container,omitempty"`
	ContainerConfig Config    `json:"container_config,omitempty"`
	DockerVersion   string    `json:"docker_version,omitempty"`
	Author          string    `json:"author,omitempty"`
	Config          *Config   `json:"config,omitempty"`
	Architecture    string    `json:"architecture,omitempty"`
	Size            int64     `json:"size,omitempty"`
}

var (
	// ErrNoSuchImage is the error returned when the image does not exist.
	ErrNoSuchImage = errors.New("no such image")

	// ErrMissingRepo is the error returned when the remote repository is
	// missing.
	ErrMissingRepo = errors.New("missing remote repository e.g. 'github.com/user/repo'")

	// ErrMissingOutputStream is the error returned when no output stream
	// is provided to some calls, like BuildImage.
	ErrMissingOutputStream = errors.New("missing output stream")

	// ErrMultipleContexts is the error returned when both a ContextDir and
	// InputStream are provided in BuildImageOptions
	ErrMultipleContexts = errors.New("image build may not be provided BOTH context dir and input stream")

	// ErrMustSpecifyNames is the error returned when the Names field on
	// ExportImagesOptions is nil or empty
	ErrMustSpecifyNames = errors.New("must specify at least one name to export")
)

// ListImagesOptions specify parameters to the ListImages function.
//
// See https://goo.gl/BVzauZ for more details.
type ListImagesOptions struct {
	Filters map[string][]string
	All     bool
	Digests bool
	Filter  string
	Context context.Context
}

// ListImages returns the list of available images in the server.
//
// See https://goo.gl/BVzauZ for more details.
func (c *Client) ListImages(opts ListImagesOptions) ([]APIImages, error) {
	path := "/images/json?" + queryString(opts)
	resp, err := c.do(http.MethodGet, path, doOptions{context: opts.Context})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var images []APIImages
	if err := json.NewDecoder(resp.Body).Decode(&images); err != nil {
		return nil, err
	}
	return images, nil
}

// ImageHistory represent a layer in an image's history returned by the
// ImageHistory call.
type ImageHistory struct {
	ID        string   `json:"Id" yaml:"Id" toml:"Id"`
	Tags      []string `json:"Tags,omitempty" yaml:"Tags,omitempty" toml:"Tags,omitempty"`
	Created   int64    `json:"Created,omitempty" yaml:"Created,omitempty" toml:"Tags,omitempty"`
	CreatedBy string   `json:"CreatedBy,omitempty" yaml:"CreatedBy,omitempty" toml:"CreatedBy,omitempty"`
	Size      int64    `json:"Size,omitempty" yaml:"Size,omitempty" toml:"Size,omitempty"`
	Comment   string   `json:"Comment,omitempty" yaml:"Comment,omitempty" toml:"Comment,omitempty"`
}

// ImageHistory returns the history of the image by its name or ID.
//
// See https://goo.gl/fYtxQa for more details.
func (c *Client) ImageHistory(name string) ([]ImageHistory, error) {
	resp, err := c.do(http.MethodGet, "/images/"+name+"/history", doOptions{})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, ErrNoSuchImage
		}
		return nil, err
	}
	defer resp.Body.Close()
	var history []ImageHistory
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		return nil, err
	}
	return history, nil
}

// RemoveImage removes an image by its name or ID.
//
// See https://goo.gl/Vd2Pck for more details.
func (c *Client) RemoveImage(name string) error {
	resp, err := c.do(http.MethodDelete, "/images/"+name, doOptions{})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return ErrNoSuchImage
		}
		return err
	}
	resp.Body.Close()
	return nil
}

// RemoveImageOptions present the set of options available for removing an image
// from a registry.
//
// See https://goo.gl/Vd2Pck for more details.
type RemoveImageOptions struct {
	Force   bool `qs:"force"`
	NoPrune bool `qs:"noprune"`
	Context context.Context
}

// RemoveImageExtended removes an image by its name or ID.
// Extra params can be passed, see RemoveImageOptions
//
// See https://goo.gl/Vd2Pck for more details.
func (c *Client) RemoveImageExtended(name string, opts RemoveImageOptions) error {
	uri := fmt.Sprintf("/images/%s?%s", name, queryString(&opts))
	resp, err := c.do(http.MethodDelete, uri, doOptions{context: opts.Context})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return ErrNoSuchImage
		}
		return err
	}
	resp.Body.Close()
	return nil
}

// InspectImage returns an image by its name or ID.
//
// See https://goo.gl/ncLTG8 for more details.
func (c *Client) InspectImage(name string) (*Image, error) {
	resp, err := c.do(http.MethodGet, "/images/"+name+"/json", doOptions{})
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, ErrNoSuchImage
		}
		return nil, err
	}
	defer resp.Body.Close()

	var image Image

	// if the caller elected to skip checking the server's version, assume it's the latest
	if c.SkipServerVersionCheck || c.expectedAPIVersion.GreaterThanOrEqualTo(apiVersion112) {
		if err := json.NewDecoder(resp.Body).Decode(&image); err != nil {
			return nil, err
		}
	} else {
		var imagePre012 ImagePre012
		if err := json.NewDecoder(resp.Body).Decode(&imagePre012); err != nil {
			return nil, err
		}

		image.ID = imagePre012.ID
		image.Parent = imagePre012.Parent
		image.Comment = imagePre012.Comment
		image.Created = imagePre012.Created
		image.Container = imagePre012.Container
		image.ContainerConfig = imagePre012.ContainerConfig
		image.DockerVersion = imagePre012.DockerVersion
		image.Author = imagePre012.Author
		image.Config = imagePre012.Config
		image.Architecture = imagePre012.Architecture
		image.Size = imagePre012.Size
	}

	return &image, nil
}

// PushImageOptions represents options to use in the PushImage method.
//
// See https://goo.gl/BZemGg for more details.
type PushImageOptions struct {
	// Name of the image
	Name string

	// Tag of the image
	Tag string

	// Registry server to push the image
	Registry string

	OutputStream      io.Writer     `qs:"-"`
	RawJSONStream     bool          `qs:"-"`
	InactivityTimeout time.Duration `qs:"-"`

	Context context.Context
}

// PushImage pushes an image to a remote registry, logging progress to w.
//
// An empty instance of AuthConfiguration may be used for unauthenticated
// pushes.
//
// See https://goo.gl/BZemGg for more details.
func (c *Client) PushImage(opts PushImageOptions, auth AuthConfiguration) error {
	if opts.Name == "" {
		return ErrNoSuchImage
	}
	headers, err := headersWithAuth(auth)
	if err != nil {
		return err
	}
	name := opts.Name
	opts.Name = ""
	path := "/images/" + name + "/push?" + queryString(&opts)
	return c.stream(http.MethodPost, path, streamOptions{
		setRawTerminal:    true,
		rawJSONStream:     opts.RawJSONStream,
		headers:           headers,
		stdout:            opts.OutputStream,
		inactivityTimeout: opts.InactivityTimeout,
		context:           opts.Context,
	})
}

// PullImageOptions present the set of options available for pulling an image
// from a registry.
//
// See https://goo.gl/qkoSsn for more details.
type PullImageOptions struct {
	All        bool
	Repository string `qs:"fromImage"`
	Tag        string
	Platform   string `ver:"1.32"`

	// Only required for Docker Engine 1.9 or 1.10 w/ Remote API < 1.21
	// and Docker Engine < 1.9
	// This parameter was removed in Docker Engine 1.11
	Registry string

	OutputStream      io.Writer     `qs:"-"`
	RawJSONStream     bool          `qs:"-"`
	InactivityTimeout time.Duration `qs:"-"`
	Context           context.Context
}

// PullImage pulls an image from a remote registry, logging progress to
// opts.OutputStream.
//
// See https://goo.gl/qkoSsn for more details.
func (c *Client) PullImage(opts PullImageOptions, auth AuthConfiguration) error {
	if opts.Repository == "" {
		return ErrNoSuchImage
	}

	headers, err := headersWithAuth(auth)
	if err != nil {
		return err
	}
	if opts.Tag == "" && strings.Contains(opts.Repository, "@") {
		parts := strings.SplitN(opts.Repository, "@", 2)
		opts.Repository = parts[0]
		opts.Tag = parts[1]
	}
	return c.createImage(&opts, headers, nil, opts.OutputStream, opts.RawJSONStream, opts.InactivityTimeout, opts.Context)
}

func (c *Client) createImage(opts any, headers map[string]string, in io.Reader, w io.Writer, rawJSONStream bool, timeout time.Duration, context context.Context) error {
	url, err := c.getPath("/images/create", opts)
	if err != nil {
		return err
	}
	return c.streamURL(http.MethodPost, url, streamOptions{
		setRawTerminal:    true,
		headers:           headers,
		in:                in,
		stdout:            w,
		rawJSONStream:     rawJSONStream,
		inactivityTimeout: timeout,
		context:           context,
	})
}

// LoadImageOptions represents the options for LoadImage Docker API Call
//
// See https://goo.gl/rEsBV3 for more details.
type LoadImageOptions struct {
	InputStream  io.Reader
	OutputStream io.Writer
	Context      context.Context
}

// LoadImage imports a tarball docker image
//
// See https://goo.gl/rEsBV3 for more details.
func (c *Client) LoadImage(opts LoadImageOptions) error {
	return c.stream(http.MethodPost, "/images/load", streamOptions{
		setRawTerminal: true,
		in:             opts.InputStream,
		stdout:         opts.OutputStream,
		context:        opts.Context,
	})
}

// ExportImageOptions represent the options for ExportImage Docker API call.
//
// See https://goo.gl/AuySaA for more details.
type ExportImageOptions struct {
	Name              string
	OutputStream      io.Writer
	InactivityTimeout time.Duration
	Context           context.Context
}

// ExportImage exports an image (as a tar file) into the stream.
//
// See https://goo.gl/AuySaA for more details.
func (c *Client) ExportImage(opts ExportImageOptions) error {
	return c.stream(http.MethodGet, fmt.Sprintf("/images/%s/get", opts.Name), streamOptions{
		setRawTerminal:    true,
		stdout:            opts.OutputStream,
		inactivityTimeout: opts.InactivityTimeout,
		context:           opts.Context,
	})
}

// ExportImagesOptions represent the options for ExportImages Docker API call
//
// See https://goo.gl/N9XlDn for more details.
type ExportImagesOptions struct {
	Names             []string
	OutputStream      io.Writer     `qs:"-"`
	InactivityTimeout time.Duration `qs:"-"`
	Context           context.Context
}

// ExportImages exports one or more images (as a tar file) into the stream
//
// See https://goo.gl/N9XlDn for more details.
func (c *Client) ExportImages(opts ExportImagesOptions) error {
	if len(opts.Names) == 0 {
		return ErrMustSpecifyNames
	}
	// API < 1.25 allows multiple name values
	// 1.25 says name must be a comma separated list
	var err error
	var exporturl string
	if c.requestedAPIVersion.GreaterThanOrEqualTo(apiVersion125) {
		str := opts.Names[0]
		for _, val := range opts.Names[1:] {
			str += "," + val
		}
		exporturl, err = c.getPath("/images/get", ExportImagesOptions{
			Names:             []string{str},
			OutputStream:      opts.OutputStream,
			InactivityTimeout: opts.InactivityTimeout,
			Context:           opts.Context,
		})
	} else {
		exporturl, err = c.getPath("/images/get", &opts)
	}
	if err != nil {
		return err
	}
	return c.streamURL(http.MethodGet, exporturl, streamOptions{
		setRawTerminal:    true,
		stdout:            opts.OutputStream,
		inactivityTimeout: opts.InactivityTimeout,
	})
}

// ImportImageOptions present the set of informations available for importing
// an image from a source file or the stdin.
//
// See https://goo.gl/qkoSsn for more details.
type ImportImageOptions struct {
	Repository string `qs:"repo"`
	Source     string `qs:"fromSrc"`
	Tag        string `qs:"tag"`

	InputStream       io.Reader     `qs:"-"`
	OutputStream      io.Writer     `qs:"-"`
	RawJSONStream     bool          `qs:"-"`
	InactivityTimeout time.Duration `qs:"-"`
	Context           context.Context
}

// ImportImage imports an image from a url, a file or stdin
//
// See https://goo.gl/qkoSsn for more details.
func (c *Client) ImportImage(opts ImportImageOptions) error {
	if opts.Repository == "" {
		return ErrNoSuchImage
	}
	if opts.Source != "-" {
		opts.InputStream = nil
	}
	if opts.Source != "-" && !isURL(opts.Source) {
		f, err := os.Open(opts.Source)
		if err != nil {
			return err
		}
		opts.InputStream = f
		opts.Source = "-"
	}
	return c.createImage(&opts, nil, opts.InputStream, opts.OutputStream, opts.RawJSONStream, opts.InactivityTimeout, opts.Context)
}

// BuilderVersion represents either the BuildKit or V1 ("classic") builder.
type BuilderVersion string

const (
	BuilderV1       BuilderVersion = "1"
	BuilderBuildKit BuilderVersion = "2"
)

// BuildImageOptions present the set of informations available for building an
// image from a tarfile with a Dockerfile in it.
//
// For more details about the Docker building process, see
// https://goo.gl/4nYHwV.
type BuildImageOptions struct {
	Context             context.Context
	Name                string   `qs:"t"`
	Dockerfile          string   `ver:"1.25"`
	ExtraHosts          string   `ver:"1.28"`
	CacheFrom           []string `qs:"-" ver:"1.25"`
	Memory              int64
	Memswap             int64
	ShmSize             int64
	CPUShares           int64
	CPUQuota            int64 `ver:"1.21"`
	CPUPeriod           int64 `ver:"1.21"`
	CPUSetCPUs          string
	Labels              map[string]string
	InputStream         io.Reader `qs:"-"`
	OutputStream        io.Writer `qs:"-"`
	Remote              string
	Auth                AuthConfiguration  `qs:"-"` // for older docker X-Registry-Auth header
	AuthConfigs         AuthConfigurations `qs:"-"` // for newer docker X-Registry-Config header
	ContextDir          string             `qs:"-"`
	Ulimits             []ULimit           `qs:"-" ver:"1.18"`
	BuildArgs           []BuildArg         `qs:"-" ver:"1.21"`
	NetworkMode         string             `ver:"1.25"`
	Platform            string             `ver:"1.32"`
	InactivityTimeout   time.Duration      `qs:"-"`
	CgroupParent        string
	SecurityOpt         []string
	Target              string
	Outputs             string `ver:"1.40"`
	NoCache             bool
	SuppressOutput      bool           `qs:"q"`
	Pull                bool           `ver:"1.16"`
	RmTmpContainer      bool           `qs:"rm"`
	ForceRmTmpContainer bool           `qs:"forcerm" ver:"1.12"`
	RawJSONStream       bool           `qs:"-"`
	Version             BuilderVersion `qs:"version" ver:"1.39"`
}

// BuildArg represents arguments that can be passed to the image when building
// it from a Dockerfile.
//
// For more details about the Docker building process, see
// https://goo.gl/4nYHwV.
type BuildArg struct {
	Name  string `json:"Name,omitempty" yaml:"Name,omitempty" toml:"Name,omitempty"`
	Value string `json:"Value,omitempty" yaml:"Value,omitempty" toml:"Value,omitempty"`
}

// BuildImage builds an image from a tarball's url or a Dockerfile in the input
// stream.
//
// See https://goo.gl/4nYHwV for more details.
func (c *Client) BuildImage(opts BuildImageOptions) error {
	if opts.OutputStream == nil {
		return ErrMissingOutputStream
	}
	headers, err := headersWithAuth(opts.Auth, c.versionedAuthConfigs(opts.AuthConfigs))
	if err != nil {
		return err
	}

	if opts.Remote != "" && opts.Name == "" {
		opts.Name = opts.Remote
	}
	if opts.InputStream != nil || opts.ContextDir != "" {
		headers["Content-Type"] = "application/tar"
	} else if opts.Remote == "" {
		return ErrMissingRepo
	}
	if opts.ContextDir != "" {
		if opts.InputStream != nil {
			return ErrMultipleContexts
		}
		var err error
		if opts.InputStream, err = createTarStream(opts.ContextDir, opts.Dockerfile); err != nil {
			return err
		}
	}
	qs, ver := queryStringVersion(&opts)

	if len(opts.CacheFrom) > 0 {
		if b, err := json.Marshal(opts.CacheFrom); err == nil {
			item := url.Values(map[string][]string{})
			item.Add("cachefrom", string(b))
			qs = fmt.Sprintf("%s&%s", qs, item.Encode())
			if ver == nil || apiVersion125.GreaterThan(ver) {
				ver = apiVersion125
			}
		}
	}

	if len(opts.Ulimits) > 0 {
		if b, err := json.Marshal(opts.Ulimits); err == nil {
			item := url.Values(map[string][]string{})
			item.Add("ulimits", string(b))
			qs = fmt.Sprintf("%s&%s", qs, item.Encode())
			if ver == nil || apiVersion118.GreaterThan(ver) {
				ver = apiVersion118
			}
		}
	}

	if len(opts.BuildArgs) > 0 {
		v := make(map[string]string)
		for _, arg := range opts.BuildArgs {
			v[arg.Name] = arg.Value
		}
		if b, err := json.Marshal(v); err == nil {
			item := url.Values(map[string][]string{})
			item.Add("buildargs", string(b))
			qs = fmt.Sprintf("%s&%s", qs, item.Encode())
			if ver == nil || apiVersion121.GreaterThan(ver) {
				ver = apiVersion121
			}
		}
	}

	buildURL, err := c.pathVersionCheck("/build", qs, ver)
	if err != nil {
		return err
	}

	return c.streamURL(http.MethodPost, buildURL, streamOptions{
		setRawTerminal:    true,
		rawJSONStream:     opts.RawJSONStream,
		headers:           headers,
		in:                opts.InputStream,
		stdout:            opts.OutputStream,
		inactivityTimeout: opts.InactivityTimeout,
		context:           opts.Context,
	})
}

func (c *Client) versionedAuthConfigs(authConfigs AuthConfigurations) registryAuth {
	if c.serverAPIVersion == nil {
		c.checkAPIVersion()
	}
	if c.serverAPIVersion != nil && c.serverAPIVersion.GreaterThanOrEqualTo(apiVersion119) {
		return AuthConfigurations119(authConfigs.Configs)
	}
	return authConfigs
}

// TagImageOptions present the set of options to tag an image.
//
// See https://goo.gl/prHrvo for more details.
type TagImageOptions struct {
	Repo    string
	Tag     string
	Force   bool
	Context context.Context
}

// TagImage adds a tag to the image identified by the given name.
//
// See https://goo.gl/prHrvo for more details.
func (c *Client) TagImage(name string, opts TagImageOptions) error {
	if name == "" {
		return ErrNoSuchImage
	}
	resp, err := c.do(http.MethodPost, "/images/"+name+"/tag?"+queryString(&opts), doOptions{
		context: opts.Context,
	})
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNoSuchImage
	}

	return err
}

func isURL(u string) bool {
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	return p.Scheme == "http" || p.Scheme == "https"
}

func headersWithAuth(auths ...registryAuth) (map[string]string, error) {
	headers := make(map[string]string)

	for _, auth := range auths {
		if auth.isEmpty() {
			continue
		}
		data, err := json.Marshal(auth)
		if err != nil {
			return nil, err
		}
		headers[auth.headerKey()] = base64.URLEncoding.EncodeToString(data)
	}

	return headers, nil
}

// APIImageSearch reflect the result of a search on the Docker Hub.
//
// See https://goo.gl/KLO9IZ for more details.
type APIImageSearch struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty" toml:"description,omitempty"`
	IsOfficial  bool   `json:"is_official,omitempty" yaml:"is_official,omitempty" toml:"is_official,omitempty"`
	IsAutomated bool   `json:"is_automated,omitempty" yaml:"is_automated,omitempty" toml:"is_automated,omitempty"`
	Name        string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	StarCount   int    `json:"star_count,omitempty" yaml:"star_count,omitempty" toml:"star_count,omitempty"`
}

// SearchImages search the docker hub with a specific given term.
//
// See https://goo.gl/KLO9IZ for more details.
func (c *Client) SearchImages(term string) ([]APIImageSearch, error) {
	resp, err := c.do(http.MethodGet, "/images/search?term="+term, doOptions{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var searchResult []APIImageSearch
	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return nil, err
	}
	return searchResult, nil
}

// SearchImagesEx search the docker hub with a specific given term and authentication.
//
// See https://goo.gl/KLO9IZ for more details.
func (c *Client) SearchImagesEx(term string, auth AuthConfiguration) ([]APIImageSearch, error) {
	headers, err := headersWithAuth(auth)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(http.MethodGet, "/images/search?term="+term, doOptions{
		headers: headers,
	})
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var searchResult []APIImageSearch
	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return nil, err
	}

	return searchResult, nil
}

// PruneImagesOptions specify parameters to the PruneImages function.
//
// See https://goo.gl/qfZlbZ for more details.
type PruneImagesOptions struct {
	Filters map[string][]string
	Context context.Context
}

// PruneImagesResults specify results from the PruneImages function.
//
// See https://goo.gl/qfZlbZ for more details.
type PruneImagesResults struct {
	ImagesDeleted  []struct{ Untagged, Deleted string }
	SpaceReclaimed int64
}

// PruneImages deletes images which are unused.
//
// See https://goo.gl/qfZlbZ for more details.
func (c *Client) PruneImages(opts PruneImagesOptions) (*PruneImagesResults, error) {
	path := "/images/prune?" + queryString(opts)
	resp, err := c.do(http.MethodPost, path, doOptions{context: opts.Context})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var results PruneImagesResults
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}
	return &results, nil
}
