package stubs

import (
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// IgnoresOriginalOCIConfig implements NoteOriginalOCIConfig() that does nothing.
type IgnoresOriginalOCIConfig struct{}

// NoteOriginalOCIConfig provides the config of the image, as it exists on the source, BUT converted to OCI format,
// or an error obtaining that value (e.g. if the image is an artifact and not a container image).
// The destination can use it in its TryReusingBlob/PutBlob implementations
// (otherwise it only obtains the final config after all layers are written).
func (stub IgnoresOriginalOCIConfig) NoteOriginalOCIConfig(ociConfig *imgspecv1.Image, configErr error) error {
	return nil
}
