package internal

import (
	"maps"
	"slices"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// DeepCopyDescriptor copies a Descriptor, deeply copying its contents.
func DeepCopyDescriptor(original *v1.Descriptor) *v1.Descriptor {
	tmp := *original
	if original.URLs != nil {
		tmp.URLs = slices.Clone(original.URLs)
	}
	if original.Annotations != nil {
		tmp.Annotations = maps.Clone(original.Annotations)
	}
	if original.Data != nil {
		tmp.Data = slices.Clone(original.Data)
	}
	if original.Platform != nil {
		tmpPlatform := *original.Platform
		if original.Platform.OSFeatures != nil {
			tmpPlatform.OSFeatures = slices.Clone(original.Platform.OSFeatures)
		}
		tmp.Platform = &tmpPlatform
	}
	return &tmp
}
