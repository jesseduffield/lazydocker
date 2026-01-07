package sbom

import (
	"slices"

	"github.com/containers/buildah/define"
)

// Preset returns a predefined SBOMScanOptions structure that has the passed-in
// name as one of its "Type" values.
func Preset(name string) (preset *define.SBOMScanOptions, err error) {
	// If you change these, make sure you update references in
	// buildah-commit.1.md and buildah-build.1.md to match!
	presets := []define.SBOMScanOptions{
		{
			Type:  []string{"", "syft", "syft-cyclonedx"},
			Image: "ghcr.io/anchore/syft",
			Commands: []string{
				"/syft scan -q dir:{ROOTFS} --output cyclonedx-json={OUTPUT}",
				"/syft scan -q dir:{CONTEXT} --output cyclonedx-json={OUTPUT}",
			},
			// ImageSBOMOutput: "/root/buildinfo/content_manifests/sbom-cyclonedx.json",
			// ImagePURLOutput: "/root/buildinfo/content_manifests/sbom-purl.json",
			MergeStrategy: define.SBOMMergeStrategyCycloneDXByComponentNameAndVersion,
		},
		{
			Type:  []string{"syft-spdx"},
			Image: "ghcr.io/anchore/syft",
			Commands: []string{
				"/syft scan -q dir:{ROOTFS} --output spdx-json={OUTPUT}",
				"/syft scan -q dir:{CONTEXT} --output spdx-json={OUTPUT}",
			},
			// ImageSBOMOutput: "/root/buildinfo/content_manifests/sbom-spdx.json",
			// ImagePURLOutput: "/root/buildinfo/content_manifests/sbom-purl.json",
			MergeStrategy: define.SBOMMergeStrategySPDXByPackageNameAndVersionInfo,
		},

		{
			Type:  []string{"trivy", "trivy-cyclonedx"},
			Image: "ghcr.io/aquasecurity/trivy",
			Commands: []string{
				"trivy filesystem -q {ROOTFS} --format cyclonedx --output {OUTPUT}",
				"trivy filesystem -q {CONTEXT} --format cyclonedx --output {OUTPUT}",
			},
			// ImageSBOMOutput: "/root/buildinfo/content_manifests/sbom-cyclonedx.json",
			// ImagePURLOutput: "/root/buildinfo/content_manifests/sbom-purl.json",
			MergeStrategy: define.SBOMMergeStrategyCycloneDXByComponentNameAndVersion,
		},
		{
			Type:  []string{"trivy-spdx"},
			Image: "ghcr.io/aquasecurity/trivy",
			Commands: []string{
				"trivy filesystem -q {ROOTFS} --format spdx-json --output {OUTPUT}",
				"trivy filesystem -q {CONTEXT} --format spdx-json --output {OUTPUT}",
			},
			// ImageSBOMOutput: "/root/buildinfo/content_manifests/sbom-spdx.json",
			// ImagePURLOutput: "/root/buildinfo/content_manifests/sbom-purl.json",
			MergeStrategy: define.SBOMMergeStrategySPDXByPackageNameAndVersionInfo,
		},
	}
	for _, preset := range presets {
		if slices.Contains(preset.Type, name) {
			return &preset, nil
		}
	}
	return nil, nil
}
