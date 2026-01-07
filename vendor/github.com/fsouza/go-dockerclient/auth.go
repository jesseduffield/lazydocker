// Copyright 2015 go-dockerclient authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
)

// ErrCannotParseDockercfg is the error returned by NewAuthConfigurations when the dockercfg cannot be parsed.
var ErrCannotParseDockercfg = errors.New("failed to read authentication from dockercfg")

// AuthConfiguration represents authentication options to use in the PushImage
// method. It represents the authentication in the Docker index server.
type AuthConfiguration struct {
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	Email         string `json:"email,omitempty"`
	ServerAddress string `json:"serveraddress,omitempty"`

	// IdentityToken can be supplied with the identitytoken response of the AuthCheck call
	// see https://pkg.go.dev/github.com/docker/docker/api/types?tab=doc#AuthConfig
	// It can be used in place of password not in conjunction with it
	IdentityToken string `json:"identitytoken,omitempty"`

	// RegistryToken can be supplied with the registrytoken
	RegistryToken string `json:"registrytoken,omitempty"`
}

func (c AuthConfiguration) isEmpty() bool {
	return c == AuthConfiguration{}
}

func (c AuthConfiguration) headerKey() string {
	return "X-Registry-Auth"
}

// AuthConfigurations represents authentication options to use for the
// PushImage method accommodating the new X-Registry-Config header
type AuthConfigurations struct {
	Configs map[string]AuthConfiguration `json:"configs"`
}

func (c AuthConfigurations) isEmpty() bool {
	return len(c.Configs) == 0
}

func (AuthConfigurations) headerKey() string {
	return "X-Registry-Config"
}

// merge updates the configuration. If a key is defined in both maps, the one
// in c.Configs takes precedence.
func (c *AuthConfigurations) merge(other AuthConfigurations) {
	for k, v := range other.Configs {
		if c.Configs == nil {
			c.Configs = make(map[string]AuthConfiguration)
		}
		if _, ok := c.Configs[k]; !ok {
			c.Configs[k] = v
		}
	}
}

// AuthConfigurations119 is used to serialize a set of AuthConfigurations
// for Docker API >= 1.19.
type AuthConfigurations119 map[string]AuthConfiguration

func (c AuthConfigurations119) isEmpty() bool {
	return len(c) == 0
}

func (c AuthConfigurations119) headerKey() string {
	return "X-Registry-Config"
}

// dockerConfig represents a registry authentation configuration from the
// .dockercfg file.
type dockerConfig struct {
	Auth          string `json:"auth"`
	Email         string `json:"email"`
	IdentityToken string `json:"identitytoken"`
	RegistryToken string `json:"registrytoken"`
}

// NewAuthConfigurationsFromFile returns AuthConfigurations from a path containing JSON
// in the same format as the .dockercfg file.
func NewAuthConfigurationsFromFile(path string) (*AuthConfigurations, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return NewAuthConfigurations(r)
}

func cfgPaths(dockerConfigEnv string, homeEnv string) []string {
	if dockerConfigEnv != "" {
		return []string{
			path.Join(dockerConfigEnv, "plaintext-passwords.json"),
			path.Join(dockerConfigEnv, "config.json"),
		}
	}
	if homeEnv != "" {
		return []string{
			path.Join(homeEnv, ".docker", "plaintext-passwords.json"),
			path.Join(homeEnv, ".docker", "config.json"),
			path.Join(homeEnv, ".dockercfg"),
		}
	}
	return nil
}

// NewAuthConfigurationsFromDockerCfg returns AuthConfigurations from system
// config files. The following files are checked in the order listed:
//
// If the environment variable DOCKER_CONFIG is set to a non-empty string:
//
// - $DOCKER_CONFIG/plaintext-passwords.json
// - $DOCKER_CONFIG/config.json
//
// Otherwise, it looks for files in the $HOME directory and the legacy
// location:
//
// - $HOME/.docker/plaintext-passwords.json
// - $HOME/.docker/config.json
// - $HOME/.dockercfg
func NewAuthConfigurationsFromDockerCfg() (*AuthConfigurations, error) {
	pathsToTry := cfgPaths(os.Getenv("DOCKER_CONFIG"), os.Getenv("HOME"))
	if len(pathsToTry) < 1 {
		return nil, errors.New("no docker configuration found")
	}
	return newAuthConfigurationsFromDockerCfg(pathsToTry)
}

func newAuthConfigurationsFromDockerCfg(pathsToTry []string) (*AuthConfigurations, error) {
	var result *AuthConfigurations
	var auths *AuthConfigurations
	var err error
	for _, path := range pathsToTry {
		auths, err = NewAuthConfigurationsFromFile(path)
		if err != nil {
			continue
		}

		if result == nil {
			result = auths
		} else {
			result.merge(*auths)
		}
	}

	if result != nil {
		return result, nil
	}
	return result, err
}

// NewAuthConfigurations returns AuthConfigurations from a JSON encoded string in the
// same format as the .dockercfg file.
func NewAuthConfigurations(r io.Reader) (*AuthConfigurations, error) {
	var auth *AuthConfigurations
	confs, err := parseDockerConfig(r)
	if err != nil {
		return nil, err
	}
	auth, err = authConfigs(confs)
	if err != nil {
		return nil, err
	}
	return auth, nil
}

func parseDockerConfig(r io.Reader) (map[string]dockerConfig, error) {
	buf := new(bytes.Buffer)
	buf.ReadFrom(r)
	byteData := buf.Bytes()

	confsWrapper := struct {
		Auths map[string]dockerConfig `json:"auths"`
	}{}
	if err := json.Unmarshal(byteData, &confsWrapper); err == nil {
		if len(confsWrapper.Auths) > 0 {
			return confsWrapper.Auths, nil
		}
	}

	var confs map[string]dockerConfig
	if err := json.Unmarshal(byteData, &confs); err != nil {
		return nil, err
	}
	return confs, nil
}

// authConfigs converts a dockerConfigs map to a AuthConfigurations object.
func authConfigs(confs map[string]dockerConfig) (*AuthConfigurations, error) {
	c := &AuthConfigurations{
		Configs: make(map[string]AuthConfiguration),
	}

	for reg, conf := range confs {
		if conf.Auth == "" {
			continue
		}

		// support both padded and unpadded encoding
		data, err := base64.StdEncoding.DecodeString(conf.Auth)
		if err != nil {
			data, err = base64.StdEncoding.WithPadding(base64.NoPadding).DecodeString(conf.Auth)
		}
		if err != nil {
			return nil, errors.New("error decoding plaintext credentials")
		}

		userpass := strings.SplitN(string(data), ":", 2)
		if len(userpass) != 2 {
			return nil, ErrCannotParseDockercfg
		}

		authConfig := AuthConfiguration{
			Email:         conf.Email,
			Username:      userpass[0],
			Password:      userpass[1],
			ServerAddress: reg,
		}

		// if identitytoken provided then zero the password and set it
		if conf.IdentityToken != "" {
			authConfig.Password = ""
			authConfig.IdentityToken = conf.IdentityToken
		}

		// if registrytoken provided then zero the password and set it
		if conf.RegistryToken != "" {
			authConfig.Password = ""
			authConfig.RegistryToken = conf.RegistryToken
		}
		c.Configs[reg] = authConfig
	}

	return c, nil
}

// AuthStatus returns the authentication status for Docker API versions >= 1.23.
type AuthStatus struct {
	Status        string `json:"Status,omitempty" yaml:"Status,omitempty" toml:"Status,omitempty"`
	IdentityToken string `json:"IdentityToken,omitempty" yaml:"IdentityToken,omitempty" toml:"IdentityToken,omitempty"`
}

// AuthCheck validates the given credentials. It returns nil if successful.
//
// For Docker API versions >= 1.23, the AuthStatus struct will be populated, otherwise it will be empty.`
//
// See https://goo.gl/6nsZkH for more details.
func (c *Client) AuthCheck(conf *AuthConfiguration) (AuthStatus, error) {
	return c.AuthCheckWithContext(conf, context.TODO())
}

// AuthCheckWithContext validates the given credentials. It returns nil if successful. The context object
// can be used to cancel the request.
//
// For Docker API versions >= 1.23, the AuthStatus struct will be populated, otherwise it will be empty.
//
// See https://goo.gl/6nsZkH for more details.
func (c *Client) AuthCheckWithContext(conf *AuthConfiguration, ctx context.Context) (AuthStatus, error) {
	var authStatus AuthStatus
	if conf == nil {
		return authStatus, errors.New("conf is nil")
	}
	resp, err := c.do(http.MethodPost, "/auth", doOptions{data: conf, context: ctx})
	if err != nil {
		return authStatus, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return authStatus, err
	}
	if len(data) == 0 {
		return authStatus, nil
	}
	if err := json.Unmarshal(data, &authStatus); err != nil {
		return authStatus, err
	}
	return authStatus, nil
}

// helperCredentials represents credentials commit from an helper
type helperCredentials struct {
	Username string `json:"Username,omitempty"`
	Secret   string `json:"Secret,omitempty"`
}

// NewAuthConfigurationsFromCredsHelpers returns AuthConfigurations from
// installed credentials helpers
func NewAuthConfigurationsFromCredsHelpers(registry string) (*AuthConfiguration, error) {
	// Load docker configuration file in order to find a possible helper provider
	pathsToTry := cfgPaths(os.Getenv("DOCKER_CONFIG"), os.Getenv("HOME"))
	if len(pathsToTry) < 1 {
		return nil, errors.New("no docker configuration found")
	}

	provider, err := getHelperProviderFromDockerCfg(pathsToTry, registry)
	if err != nil {
		return nil, err
	}

	c, err := getCredentialsFromHelper(provider, registry)
	if err != nil {
		return nil, err
	}

	creds := new(AuthConfiguration)
	creds.Username = c.Username
	creds.Password = c.Secret
	return creds, nil
}

func getHelperProviderFromDockerCfg(pathsToTry []string, registry string) (string, error) {
	for _, path := range pathsToTry {
		content, err := os.ReadFile(path)
		if err != nil {
			// if we can't read the file keep going
			continue
		}

		provider, err := parseCredsDockerConfig(content, registry)
		if err != nil {
			continue
		}
		if provider != "" {
			return provider, nil
		}
	}
	return "", errors.New("no docker credentials provider found")
}

func parseCredsDockerConfig(config []byte, registry string) (string, error) {
	creds := struct {
		CredsStore  string            `json:"credsStore,omitempty"`
		CredHelpers map[string]string `json:"credHelpers,omitempty"`
	}{}
	err := json.Unmarshal(config, &creds)
	if err != nil {
		return "", err
	}

	provider, ok := creds.CredHelpers[registry]
	if ok {
		return provider, nil
	}
	return creds.CredsStore, nil
}

// Run and parse the found credential helper
func getCredentialsFromHelper(provider string, registry string) (*helperCredentials, error) {
	helpercreds, err := runDockerCredentialsHelper(provider, registry)
	if err != nil {
		return nil, err
	}

	c := new(helperCredentials)
	err = json.Unmarshal(helpercreds, c)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func runDockerCredentialsHelper(provider string, registry string) ([]byte, error) {
	cmd := exec.Command("docker-credential-"+provider, "get")

	var stdout bytes.Buffer

	cmd.Stdin = bytes.NewBuffer([]byte(registry))
	cmd.Stdout = &stdout

	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	return stdout.Bytes(), nil
}
