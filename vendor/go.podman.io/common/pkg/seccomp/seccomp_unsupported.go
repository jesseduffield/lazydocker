//go:build !linux || !seccomp

// SPDX-License-Identifier: Apache-2.0

// Copyright 2013-2018 Docker, Inc.

package seccomp

import (
	"errors"

	"github.com/opencontainers/runtime-spec/specs-go"
)

var errNotSupported = errors.New("seccomp not enabled in this build")

// LoadProfile returns an error on unsupported systems.
func LoadProfile(body string, rs *specs.Spec) (*specs.LinuxSeccomp, error) {
	return nil, errNotSupported
}

// GetDefaultProfile returns an error on unsupported systems.
func GetDefaultProfile(rs *specs.Spec) (*specs.LinuxSeccomp, error) {
	return nil, errNotSupported
}

// LoadProfileFromBytes takes a byte slice and decodes the seccomp profile.
func LoadProfileFromBytes(body []byte, rs *specs.Spec) (*specs.LinuxSeccomp, error) {
	return nil, errNotSupported
}

// LoadProfileFromConfig takes a Seccomp struct and a spec to retrieve a LinuxSeccomp.
func LoadProfileFromConfig(config *Seccomp, specgen *specs.Spec) (*specs.LinuxSeccomp, error) {
	return nil, errNotSupported
}

// IsEnabled returns true if seccomp is enabled for the host.
func IsEnabled() bool {
	return false
}

// IsSupported returns true if the system has been configured to support
// seccomp.
func IsSupported() bool {
	return false
}
