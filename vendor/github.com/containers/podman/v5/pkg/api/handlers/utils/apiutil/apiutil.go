//go:build !remote

package apiutil

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/containers/podman/v5/version"
	"github.com/gorilla/mux"
)

var (
	// ErrVersionNotGiven returned when version not given by client
	ErrVersionNotGiven = errors.New("version not given in URL path")
	// ErrVersionNotSupported returned when given version is too old
	ErrVersionNotSupported = errors.New("given version is not supported")
)

// IsLibpodRequest returns true if the request related to a libpod endpoint
// (e.g., /v2/libpod/...).
func IsLibpodRequest(r *http.Request) bool {
	split := strings.Split(r.URL.String(), "/")
	return len(split) >= 3 && split[2] == "libpod"
}

// IsLibpodLocalRequest returns true if the request related to a libpod local endpoint
// (e.g., /v2/libpod/local...).
func IsLibpodLocalRequest(r *http.Request) bool {
	split := strings.Split(r.URL.String(), "/")
	return len(split) >= 4 && split[2] == "libpod" && split[3] == "local"
}

// SupportedVersion validates that the version provided by client is included in the given condition
// https://github.com/blang/semver#ranges provides the details for writing conditions
// If a version is not given in URL path, ErrVersionNotGiven is returned
func SupportedVersion(r *http.Request, condition string) (semver.Version, error) {
	version := semver.Version{}
	val, ok := mux.Vars(r)["version"]
	if !ok {
		return version, ErrVersionNotGiven
	}
	safeVal, err := url.PathUnescape(val)
	if err != nil {
		return version, fmt.Errorf("unable to unescape given API version: %q: %w", val, err)
	}
	version, err = semver.ParseTolerant(safeVal)
	if err != nil {
		return version, fmt.Errorf("unable to parse given API version: %q from %q: %w", safeVal, val, err)
	}

	inRange, err := semver.ParseRange(condition)
	if err != nil {
		return version, err
	}

	if inRange(version) {
		return version, nil
	}
	return version, ErrVersionNotSupported
}

// SupportedVersionWithDefaults validates that the version provided by client valid is supported by server
// minimal API version <= client path version <= maximum API version focused on the endpoint tree from URL
func SupportedVersionWithDefaults(r *http.Request) (semver.Version, error) {
	tree := version.Compat
	if IsLibpodRequest(r) {
		tree = version.Libpod
	}

	return SupportedVersion(r,
		fmt.Sprintf(">=%s <=%s", version.APIVersion[tree][version.MinimalAPI].String(),
			version.APIVersion[tree][version.CurrentAPI].String()))
}
