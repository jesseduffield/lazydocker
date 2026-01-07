package platform

import (
	"fmt"
	"runtime"

	"github.com/containerd/platforms"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
)

// Normalize normalizes (according to the OCI spec) the specified os,
// arch and variant.  If left empty, the individual item will be normalized.
func Normalize(rawOS, rawArch, rawVariant string) (os, arch, variant string) {
	platformSpec := v1.Platform{
		OS:           rawOS,
		Architecture: rawArch,
		Variant:      rawVariant,
	}
	normalizedSpec := platforms.Normalize(platformSpec)
	if normalizedSpec.Variant == "" && rawVariant != "" {
		normalizedSpec.Variant = rawVariant
	}
	rawPlatform := ToString(normalizedSpec.OS, normalizedSpec.Architecture, normalizedSpec.Variant)
	normalizedPlatform, err := platforms.Parse(rawPlatform)
	if err != nil {
		logrus.Debugf("Error normalizing platform: %v", err)
		return rawOS, rawArch, rawVariant
	}
	logrus.Debugf("Normalized platform %s to %s", rawPlatform, normalizedPlatform)
	os = rawOS
	if rawOS != "" {
		os = normalizedPlatform.OS
	}
	arch = rawArch
	if rawArch != "" {
		arch = normalizedPlatform.Architecture
	}
	variant = rawVariant
	if rawVariant != "" || (rawVariant == "" && normalizedPlatform.Variant != "") {
		variant = normalizedPlatform.Variant
	}
	return os, arch, variant
}

func ToString(os, arch, variant string) string {
	if os == "" {
		os = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	if variant == "" {
		return fmt.Sprintf("%s/%s", os, arch)
	}
	return fmt.Sprintf("%s/%s/%s", os, arch, variant)
}
