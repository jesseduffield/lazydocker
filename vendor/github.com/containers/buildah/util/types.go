package util

import (
	"github.com/containers/buildah/define"
)

const (
	// DefaultRuntime if containers.conf fails.
	DefaultRuntime = define.DefaultRuntime
)

var (
	// Deprecated: DefaultCapabilities values should be retrieved from
	// github.com/containers/common/pkg/config
	DefaultCapabilities = define.DefaultCapabilities //nolint

	// Deprecated: DefaultNetworkSysctl values should be retrieved from
	// github.com/containers/common/pkg/config
	DefaultNetworkSysctl = define.DefaultNetworkSysctl //nolint
)
