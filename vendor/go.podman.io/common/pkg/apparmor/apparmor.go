package apparmor

import (
	"errors"

	"go.podman.io/common/version"
)

const (
	// ProfilePrefix is used for version-independent presence checks.
	ProfilePrefix = "containers-default-"

	// Profile default name.
	Profile = ProfilePrefix + version.Version
)

var (
	// ErrApparmorUnsupported indicates that AppArmor support is not supported.
	ErrApparmorUnsupported = errors.New("AppArmor is not supported")
	// ErrApparmorRootless indicates that AppArmor support is not supported in rootless mode.
	ErrApparmorRootless = errors.New("AppArmor is not supported in rootless mode")
)
