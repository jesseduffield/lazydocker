package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	dockerAPITypes "github.com/docker/docker/api/types/registry"
	"github.com/sirupsen/logrus"
	imageAuth "go.podman.io/image/v5/pkg/docker/config"
	"go.podman.io/image/v5/types"
)

// xRegistryAuthHeader is the key to the encoded registry authentication configuration in an http-request header.
// This header supports one registry per header occurrence. To support N registries provide N headers, one per registry.
// As of Docker API 1.40 and Libpod API 1.0.0, this header is supported by all endpoints.
const xRegistryAuthHeader = "X-Registry-Auth"

// xRegistryConfigHeader is the key to the encoded registry authentication configuration in an http-request header.
// This header supports N registries in one header via a Base64 encoded, JSON map.
// As of Docker API 1.40 and Libpod API 2.0.0, this header is supported by build endpoints.
const xRegistryConfigHeader = "X-Registry-Config"

// GetCredentials queries the http.Request for X-Registry-.* headers and extracts
// the necessary authentication information for libpod operations, possibly
// creating a config file. If that is the case, the caller must call RemoveAuthFile.
func GetCredentials(r *http.Request) (*types.DockerAuthConfig, string, error) {
	nonemptyHeaderValue := func(key string) ([]string, bool) {
		hdr := r.Header.Values(key)
		return hdr, len(hdr) > 0
	}
	var override *types.DockerAuthConfig
	var fileContents map[string]types.DockerAuthConfig
	var headerName string
	var err error
	if hdr, ok := nonemptyHeaderValue(xRegistryConfigHeader); ok {
		headerName = xRegistryConfigHeader
		override, fileContents, err = getConfigCredentials(r, hdr)
	} else if hdr, ok := nonemptyHeaderValue(xRegistryAuthHeader); ok {
		headerName = xRegistryAuthHeader
		override, fileContents, err = getAuthCredentials(hdr)
	} else {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse %q header for %s: %w", headerName, r.URL.String(), err)
	}

	var authFile string
	if fileContents == nil {
		authFile = ""
	} else {
		authFile, err = authConfigsToAuthFile(fileContents)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse %q header for %s: %w", headerName, r.URL.String(), err)
		}
	}
	return override, authFile, nil
}

// getConfigCredentials extracts one or more docker.AuthConfig from a request and its
// xRegistryConfigHeader value.  An empty key will be used as default while a named registry will be
// returned as types.DockerAuthConfig
func getConfigCredentials(r *http.Request, headers []string) (*types.DockerAuthConfig, map[string]types.DockerAuthConfig, error) {
	var auth *types.DockerAuthConfig
	configs := make(map[string]types.DockerAuthConfig)

	for _, h := range headers {
		param, err := base64.URLEncoding.DecodeString(h)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decode %q: %w", xRegistryConfigHeader, err)
		}

		ac := make(map[string]dockerAPITypes.AuthConfig)
		err = json.Unmarshal(param, &ac)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to unmarshal %q: %w", xRegistryConfigHeader, err)
		}

		for k, v := range ac {
			configs[k] = dockerAuthToImageAuth(v)
		}
	}

	// Empty key implies no registry given in API
	if c, found := configs[""]; found {
		auth = &c
	}

	// Override any default given above if specialized credentials provided
	if registries, found := r.URL.Query()["registry"]; found {
		for _, r := range registries {
			for k, v := range configs {
				if strings.Contains(k, r) {
					v := v
					auth = &v
					break
				}
			}
			if auth != nil {
				break
			}
		}

		if auth == nil {
			logrus.Debugf("%q header found in request, but \"registry=%v\" query parameter not provided",
				xRegistryConfigHeader, registries)
		} else {
			logrus.Debugf("%q header found in request for username %q", xRegistryConfigHeader, auth.Username)
		}
	}

	return auth, configs, nil
}

// getAuthCredentials extracts one or more DockerAuthConfigs from an xRegistryAuthHeader
// value.  The header could specify a single-auth config in which case the
// first return value is set.  In case of a multi-auth header, the contents are
// returned in the second return value.
func getAuthCredentials(headers []string) (*types.DockerAuthConfig, map[string]types.DockerAuthConfig, error) {
	authHeader := headers[0]

	// First look for a multi-auth header (i.e., a map).
	authConfigs, err := parseMultiAuthHeader(authHeader)
	if err == nil {
		return nil, authConfigs, nil
	}

	// Fallback to looking for a single-auth header (i.e., one config).
	authConfig, err := parseSingleAuthHeader(authHeader)
	if err != nil {
		return nil, nil, err
	}
	return &authConfig, nil, nil
}

// MakeXRegistryConfigHeader returns a map with the "X-Registry-Config" header set, which can
// conveniently be used in the http stack.
func MakeXRegistryConfigHeader(sys *types.SystemContext, username, password string) (http.Header, error) {
	if sys == nil {
		sys = &types.SystemContext{}
	}
	authConfigs, err := imageAuth.GetAllCredentials(sys)
	if err != nil {
		return nil, err
	}

	if username != "" {
		authConfigs[""] = types.DockerAuthConfig{
			Username: username,
			Password: password,
		}
	}

	if len(authConfigs) == 0 {
		return nil, nil
	}
	content, err := encodeMultiAuthConfigs(authConfigs)
	if err != nil {
		return nil, err
	}
	return http.Header{xRegistryConfigHeader: []string{content}}, nil
}

// MakeXRegistryAuthHeader returns a map with the "X-Registry-Auth" header set, which can
// conveniently be used in the http stack.
func MakeXRegistryAuthHeader(sys *types.SystemContext, username, password string) (http.Header, error) {
	if username != "" {
		content, err := encodeSingleAuthConfig(types.DockerAuthConfig{Username: username, Password: password})
		if err != nil {
			return nil, err
		}
		return http.Header{xRegistryAuthHeader: []string{content}}, nil
	}

	if sys == nil {
		sys = &types.SystemContext{}
	}
	authConfigs, err := imageAuth.GetAllCredentials(sys)
	if err != nil {
		return nil, err
	}
	content, err := encodeMultiAuthConfigs(authConfigs)
	if err != nil {
		return nil, err
	}
	return http.Header{xRegistryAuthHeader: []string{content}}, nil
}

// RemoveAuthfile is a convenience function that is meant to be called in a
// deferred statement. If non-empty, it removes the specified authfile and log
// errors.  It's meant to reduce boilerplate code at call sites of
// `GetCredentials`.
func RemoveAuthfile(authfile string) {
	if authfile == "" {
		return
	}
	if err := os.Remove(authfile); err != nil {
		logrus.Errorf("Removing temporary auth file %q: %v", authfile, err)
	}
}

// encodeSingleAuthConfig serializes the auth configuration as a base64 encoded JSON payload.
func encodeSingleAuthConfig(authConfig types.DockerAuthConfig) (string, error) {
	conf := imageAuthToDockerAuth(authConfig)
	buf, err := json.Marshal(conf)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(buf), nil
}

// encodeMultiAuthConfigs serializes the auth configurations as a base64 encoded JSON payload.
func encodeMultiAuthConfigs(authConfigs map[string]types.DockerAuthConfig) (string, error) {
	confs := make(map[string]dockerAPITypes.AuthConfig)
	for registry, authConf := range authConfigs {
		confs[registry] = imageAuthToDockerAuth(authConf)
	}
	buf, err := json.Marshal(confs)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(buf), nil
}

// authConfigsToAuthFile stores the specified auth configs in a temporary files
// and returns its path. The file can later be used as an auth file for contacting
// one or more container registries.  If tmpDir is empty, the system's default
// TMPDIR will be used.
func authConfigsToAuthFile(authConfigs map[string]types.DockerAuthConfig) (string, error) {
	// Initialize an empty temporary JSON file.
	tmpFile, err := os.CreateTemp("", "auth.json.")
	if err != nil {
		return "", err
	}
	if _, err := tmpFile.Write([]byte{'{', '}'}); err != nil {
		return "", fmt.Errorf("initializing temporary auth file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("closing temporary auth file: %w", err)
	}
	authFilePath := tmpFile.Name()

	// Now use the c/image packages to store the credentials. It's battle
	// tested, and we make sure to use the same code as the image backend.
	sys := types.SystemContext{AuthFilePath: authFilePath}
	for authFileKey, config := range authConfigs {
		key := normalizeAuthFileKey(authFileKey)

		// Note that we do not validate the credentials here. We assume
		// that all credentials are valid. They'll be used on demand
		// later.
		if err := imageAuth.SetAuthentication(&sys, key, config.Username, config.Password); err != nil {
			return "", fmt.Errorf("storing credentials in temporary auth file (key: %q / %q, user: %q): %w", authFileKey, key, config.Username, err)
		}
	}

	return authFilePath, nil
}

// normalizeAuthFileKey takes an auth file key and converts it into a new-style credential key
// in the canonical format, as interpreted by c/image/pkg/docker/config.
func normalizeAuthFileKey(authFileKey string) string {
	stripped := strings.TrimPrefix(authFileKey, "http://")
	stripped = strings.TrimPrefix(stripped, "https://")

	if stripped != authFileKey { // URLs are interpreted to mean complete registries
		stripped, _, _ = strings.Cut(stripped, "/")
	}

	// Only non-namespaced registry names (or URLs) need to be normalized; repo namespaces
	// always use the simple format.
	switch stripped {
	case "registry-1.docker.io", "index.docker.io":
		return "docker.io"
	default:
		return stripped
	}
}

// dockerAuthToImageAuth converts a docker auth config to one we're using
// internally from c/image.  Note that the Docker types look slightly
// different, so we need to convert to be extra sure we're not running into
// undesired side-effects when unmarshalling directly to our types.
func dockerAuthToImageAuth(authConfig dockerAPITypes.AuthConfig) types.DockerAuthConfig {
	return types.DockerAuthConfig{
		Username:      authConfig.Username,
		Password:      authConfig.Password,
		IdentityToken: authConfig.IdentityToken,
	}
}

// reverse conversion of `dockerAuthToImageAuth`.
func imageAuthToDockerAuth(authConfig types.DockerAuthConfig) dockerAPITypes.AuthConfig {
	return dockerAPITypes.AuthConfig{
		Username:      authConfig.Username,
		Password:      authConfig.Password,
		IdentityToken: authConfig.IdentityToken,
	}
}

// parseSingleAuthHeader extracts a DockerAuthConfig from an xRegistryAuthHeader value.
// The header content is a single DockerAuthConfig.
func parseSingleAuthHeader(authHeader string) (types.DockerAuthConfig, error) {
	// Accept "null" and handle it as empty value for compatibility reason with Docker.
	// Some java docker clients pass this value, e.g. this one used in Eclipse.
	if len(authHeader) == 0 || authHeader == "null" {
		return types.DockerAuthConfig{}, nil
	}

	authConfig := dockerAPITypes.AuthConfig{}
	authJSON := base64.NewDecoder(base64.URLEncoding, strings.NewReader(authHeader))
	if err := json.NewDecoder(authJSON).Decode(&authConfig); err != nil {
		return types.DockerAuthConfig{}, err
	}
	return dockerAuthToImageAuth(authConfig), nil
}

// parseMultiAuthHeader extracts a DockerAuthConfig from an xRegistryAuthHeader value.
// The header content is a map[string]DockerAuthConfigs.
func parseMultiAuthHeader(authHeader string) (map[string]types.DockerAuthConfig, error) {
	// Accept "null" and handle it as empty value for compatibility reason with Docker.
	// Some java docker clients pass this value, e.g. this one used in Eclipse.
	if len(authHeader) == 0 || authHeader == "null" {
		return nil, nil
	}

	dockerAuthConfigs := make(map[string]dockerAPITypes.AuthConfig)
	authJSON := base64.NewDecoder(base64.URLEncoding, strings.NewReader(authHeader))
	if err := json.NewDecoder(authJSON).Decode(&dockerAuthConfigs); err != nil {
		return nil, err
	}

	// Now convert to the internal types.
	authConfigs := make(map[string]types.DockerAuthConfig)
	for server := range dockerAuthConfigs {
		authConfigs[server] = dockerAuthToImageAuth(dockerAuthConfigs[server])
	}
	return authConfigs, nil
}
