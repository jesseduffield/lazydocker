//go:build !remote

package libimage

import (
	"go.podman.io/common/libimage/define"
	"go.podman.io/common/libimage/platform"
)

// PlatformPolicy controls the behavior of image-platform matching.
// Deprecated: new code should use define.PlatformPolicy directly.
type PlatformPolicy = define.PlatformPolicy

const (
	// Only debug log if an image does not match the expected platform.
	// Deprecated: new code should reference define.PlatformPolicyDefault directly.
	PlatformPolicyDefault = define.PlatformPolicyDefault
	// Warn if an image does not match the expected platform.
	// Deprecated: new code should reference define.PlatformPolicyWarn directly.
	PlatformPolicyWarn = define.PlatformPolicyWarn
)

// NormalizePlatform normalizes (according to the OCI spec) the specified os,
// arch and variant. If left empty, the individual item will be normalized.
// Deprecated: new code should call libimage/platform.Normalize() instead.
func NormalizePlatform(rawOS, rawArch, rawVariant string) (os, arch, variant string) {
	return platform.Normalize(rawOS, rawArch, rawVariant)
}
