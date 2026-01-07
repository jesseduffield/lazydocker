package impl

import "go.podman.io/image/v5/types"

// Properties collects properties of an ImageDestination that are constant throughout its lifetime
// (but might differ across instances).
type Properties struct {
	// SupportedManifestMIMETypes tells which manifest MIME types the destination supports.
	// A empty slice or nil means any MIME type can be tried to upload.
	SupportedManifestMIMETypes []string
	// DesiredLayerCompression indicates the kind of compression to apply on layers
	DesiredLayerCompression types.LayerCompression
	// AcceptsForeignLayerURLs is false if foreign layers in manifest should be actually
	// uploaded to the image destination, true otherwise.
	AcceptsForeignLayerURLs bool
	// MustMatchRuntimeOS is set to true if the destination can store only images targeted for the current runtime architecture and OS.
	MustMatchRuntimeOS bool
	// IgnoresEmbeddedDockerReference is set to true if the destination does not care about Image.EmbeddedDockerReferenceConflicts(),
	// and would prefer to receive an unmodified manifest instead of one modified for the destination.
	// Does not make a difference if Reference().DockerReference() is nil.
	IgnoresEmbeddedDockerReference bool
	// HasThreadSafePutBlob indicates that PutBlob can be executed concurrently.
	HasThreadSafePutBlob bool
}

// PropertyMethodsInitialize implements parts of private.ImageDestination corresponding to Properties.
type PropertyMethodsInitialize struct {
	// We need two separate structs, PropertyMethodsInitialize and Properties, because Go prohibits fields and methods with the same name.

	vals Properties
}

// PropertyMethods creates an PropertyMethodsInitialize for vals.
func PropertyMethods(vals Properties) PropertyMethodsInitialize {
	return PropertyMethodsInitialize{
		vals: vals,
	}
}

// SupportedManifestMIMETypes tells which manifest mime types the destination supports
// If an empty slice or nil it's returned, then any mime type can be tried to upload
func (o PropertyMethodsInitialize) SupportedManifestMIMETypes() []string {
	return o.vals.SupportedManifestMIMETypes
}

// DesiredLayerCompression indicates the kind of compression to apply on layers
func (o PropertyMethodsInitialize) DesiredLayerCompression() types.LayerCompression {
	return o.vals.DesiredLayerCompression
}

// AcceptsForeignLayerURLs returns false iff foreign layers in manifest should be actually
// uploaded to the image destination, true otherwise.
func (o PropertyMethodsInitialize) AcceptsForeignLayerURLs() bool {
	return o.vals.AcceptsForeignLayerURLs
}

// MustMatchRuntimeOS returns true iff the destination can store only images targeted for the current runtime architecture and OS. False otherwise.
func (o PropertyMethodsInitialize) MustMatchRuntimeOS() bool {
	return o.vals.MustMatchRuntimeOS
}

// IgnoresEmbeddedDockerReference() returns true iff the destination does not care about Image.EmbeddedDockerReferenceConflicts(),
// and would prefer to receive an unmodified manifest instead of one modified for the destination.
// Does not make a difference if Reference().DockerReference() is nil.
func (o PropertyMethodsInitialize) IgnoresEmbeddedDockerReference() bool {
	return o.vals.IgnoresEmbeddedDockerReference
}

// HasThreadSafePutBlob indicates whether PutBlob can be executed concurrently.
func (o PropertyMethodsInitialize) HasThreadSafePutBlob() bool {
	return o.vals.HasThreadSafePutBlob
}
