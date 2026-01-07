package layout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/directory/explicitfilepath"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/image"
	"go.podman.io/image/v5/internal/manifest"
	"go.podman.io/image/v5/oci/internal"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
)

func init() {
	transports.Register(Transport)
}

var (
	// Transport is an ImageTransport for OCI directories.
	Transport = ociTransport{}

	// ErrMoreThanOneImage is an error returned when the manifest includes
	// more than one image and the user should choose which one to use.
	ErrMoreThanOneImage = errors.New("more than one image in oci, choose an image")
)

type ociTransport struct{}

func (t ociTransport) Name() string {
	return "oci"
}

// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an ImageReference.
func (t ociTransport) ParseReference(reference string) (types.ImageReference, error) {
	return ParseReference(reference)
}

// ValidatePolicyConfigurationScope checks that scope is a valid name for a signature.PolicyTransportScopes keys
// (i.e. a valid PolicyConfigurationIdentity() or PolicyConfigurationNamespaces() return value).
// It is acceptable to allow an invalid value which will never be matched, it can "only" cause user confusion.
// scope passed to this function will not be "", that value is always allowed.
func (t ociTransport) ValidatePolicyConfigurationScope(scope string) error {
	return internal.ValidateScope(scope)
}

// ociReference is an ImageReference for OCI directory paths.
type ociReference struct {
	// Note that the interpretation of paths below depends on the underlying filesystem state, which may change under us at any time!
	// Either of the paths may point to a different, or no, inode over time.  resolvedDir may contain symbolic links, and so on.

	// Generally we follow the intent of the user, and use the "dir" member for filesystem operations (e.g. the user can use a relative path to avoid
	// being exposed to symlinks and renames in the parent directories to the working directory).
	// (But in general, we make no attempt to be completely safe against concurrent hostile filesystem modifications.)
	dir         string // As specified by the user. May be relative, contain symlinks, etc.
	resolvedDir string // Absolute path with no symlinks, at least at the time of its creation. Primarily used for policy namespaces.
	// If image=="" && sourceIndex==-1, it means the "only image" in the index.json is used in the case it is a source
	// for destinations, the image name annotation "image.ref.name" is not added to the index.json.
	//
	// Must not be set if sourceIndex is set (the value is not -1).
	image string
	// If not -1, a zero-based index of an image in the manifest index. Valid only for sources.
	// Must not be set if image is set.
	sourceIndex int
}

// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an OCI ImageReference.
func ParseReference(reference string) (types.ImageReference, error) {
	dir, image, index, err := internal.ParseReferenceIntoElements(reference)
	if err != nil {
		return nil, err
	}
	return newReference(dir, image, index)
}

// newReference returns an OCI reference for a directory, and an image name annotation or sourceIndex.
//
// If sourceIndex==-1, the index will not be valid to point out the source image, only image will be used.
// We do not expose an API supplying the resolvedDir; we could, but recomputing it
// is generally cheap enough that we prefer being confident about the properties of resolvedDir.
func newReference(dir, image string, sourceIndex int) (types.ImageReference, error) {
	resolved, err := explicitfilepath.ResolvePathToFullyExplicit(dir)
	if err != nil {
		return nil, err
	}

	if err := internal.ValidateOCIPath(dir); err != nil {
		return nil, err
	}

	if err = internal.ValidateImageName(image); err != nil {
		return nil, err
	}

	if sourceIndex != -1 && sourceIndex < 0 {
		return nil, fmt.Errorf("Invalid oci: layout reference: index @%d must not be negative", sourceIndex)
	}
	if sourceIndex != -1 && image != "" {
		return nil, fmt.Errorf("Invalid oci: layout reference: cannot use both an image %s and a source index @%d", image, sourceIndex)
	}
	return ociReference{dir: dir, resolvedDir: resolved, image: image, sourceIndex: sourceIndex}, nil
}

// NewIndexReference returns an OCI reference for a path and a zero-based source manifest index.
func NewIndexReference(dir string, sourceIndex int) (types.ImageReference, error) {
	if sourceIndex < 0 {
		return nil, fmt.Errorf("invalid call to NewIndexReference with negative index %d", sourceIndex)
	}
	return newReference(dir, "", sourceIndex)
}

// NewReference returns an OCI reference for a directory and an optional image name annotation (if not "").
func NewReference(dir, image string) (types.ImageReference, error) {
	return newReference(dir, image, -1)
}

func (ref ociReference) Transport() types.ImageTransport {
	return Transport
}

// StringWithinTransport returns a string representation of the reference, which MUST be such that
// reference.Transport().ParseReference(reference.StringWithinTransport()) returns an equivalent reference.
// NOTE: The returned string is not promised to be equal to the original input to ParseReference;
// e.g. default attribute values omitted by the user may be filled in the return value, or vice versa.
// WARNING: Do not use the return value in the UI to describe an image, it does not contain the Transport().Name() prefix.
func (ref ociReference) StringWithinTransport() string {
	if ref.sourceIndex == -1 {
		return fmt.Sprintf("%s:%s", ref.dir, ref.image)
	}
	return fmt.Sprintf("%s:@%d", ref.dir, ref.sourceIndex)
}

// DockerReference returns a Docker reference associated with this reference
// (fully explicit, i.e. !reference.IsNameOnly, but reflecting user intent,
// not e.g. after redirect or alias processing), or nil if unknown/not applicable.
func (ref ociReference) DockerReference() reference.Named {
	return nil
}

// PolicyConfigurationIdentity returns a string representation of the reference, suitable for policy lookup.
// This MUST reflect user intent, not e.g. after processing of third-party redirects or aliases;
// The value SHOULD be fully explicit about its semantics, with no hidden defaults, AND canonical
// (i.e. various references with exactly the same semantics should return the same configuration identity)
// It is fine for the return value to be equal to StringWithinTransport(), and it is desirable but
// not required/guaranteed that it will be a valid input to Transport().ParseReference().
// Returns "" if configuration identities for these references are not supported.
func (ref ociReference) PolicyConfigurationIdentity() string {
	// NOTE: ref.image is not a part of the image identity, because "$dir:$someimage" and "$dir:" may mean the
	// same image and the two can’t be statically disambiguated.  Using at least the repository directory is
	// less granular but hopefully still useful.
	return ref.resolvedDir
}

// PolicyConfigurationNamespaces returns a list of other policy configuration namespaces to search
// for if explicit configuration for PolicyConfigurationIdentity() is not set.  The list will be processed
// in order, terminating on first match, and an implicit "" is always checked at the end.
// It is STRONGLY recommended for the first element, if any, to be a prefix of PolicyConfigurationIdentity(),
// and each following element to be a prefix of the element preceding it.
func (ref ociReference) PolicyConfigurationNamespaces() []string {
	res := []string{}
	path := ref.resolvedDir
	for {
		lastSlash := strings.LastIndex(path, "/")
		// Note that we do not include "/"; it is redundant with the default "" global default,
		// and rejected by ociTransport.ValidatePolicyConfigurationScope above.
		if lastSlash == -1 || path == "/" {
			break
		}
		res = append(res, path)
		path = path[:lastSlash]
	}
	return res
}

// NewImage returns a types.ImageCloser for this reference, possibly specialized for this ImageTransport.
// The caller must call .Close() on the returned ImageCloser.
// NOTE: If any kind of signature verification should happen, build an UnparsedImage from the value returned by NewImageSource,
// verify that UnparsedImage, and convert it into a real Image via image.FromUnparsedImage.
// WARNING: This may not do the right thing for a manifest list, see image.FromSource for details.
func (ref ociReference) NewImage(ctx context.Context, sys *types.SystemContext) (types.ImageCloser, error) {
	return image.FromReference(ctx, sys, ref)
}

// getIndex returns a pointer to the index references by this ociReference. If an error occurs opening an index nil is returned together
// with an error.
func (ref ociReference) getIndex() (*imgspecv1.Index, error) {
	return parseIndex(ref.indexPath())
}

func parseIndex(path string) (*imgspecv1.Index, error) {
	return parseJSON[imgspecv1.Index](path)
}

func parseJSON[T any](path string) (*T, error) {
	content, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer content.Close()

	obj := new(T)
	if err := json.NewDecoder(content).Decode(obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (ref ociReference) getManifestDescriptor() (imgspecv1.Descriptor, int, error) {
	index, err := ref.getIndex()
	if err != nil {
		return imgspecv1.Descriptor{}, -1, err
	}

	switch {
	case ref.image != "" && ref.sourceIndex != -1: // Coverage: newReference refuses to create such references.
		return imgspecv1.Descriptor{}, -1, fmt.Errorf("Internal error: Cannot have both ref %s and source index @%d",
			ref.image, ref.sourceIndex)

	case ref.sourceIndex != -1:
		if ref.sourceIndex >= len(index.Manifests) {
			return imgspecv1.Descriptor{}, -1, fmt.Errorf("index %d is too large, only %d entries available", ref.sourceIndex, len(index.Manifests))
		}
		return index.Manifests[ref.sourceIndex], ref.sourceIndex, nil

	case ref.image != "":
		// if image specified, look through all manifests for a match
		var unsupportedMIMETypes []string
		for i, md := range index.Manifests {
			if refName, ok := md.Annotations[imgspecv1.AnnotationRefName]; ok && refName == ref.image {
				if md.MediaType == imgspecv1.MediaTypeImageManifest || md.MediaType == imgspecv1.MediaTypeImageIndex || md.MediaType == manifest.DockerV2Schema2MediaType || md.MediaType == manifest.DockerV2ListMediaType {
					return md, i, nil
				}
				unsupportedMIMETypes = append(unsupportedMIMETypes, md.MediaType)
			}
		}
		if len(unsupportedMIMETypes) != 0 {
			return imgspecv1.Descriptor{}, -1, fmt.Errorf("reference %q matches unsupported manifest MIME types %q", ref.image, unsupportedMIMETypes)
		}
		return imgspecv1.Descriptor{}, -1, ImageNotFoundError{ref}

	default:
		// return manifest if only one image is in the oci directory
		if len(index.Manifests) != 1 {
			// ask user to choose image when more than one image in the oci directory
			return imgspecv1.Descriptor{}, -1, ErrMoreThanOneImage
		}
		return index.Manifests[0], 0, nil
	}
}

// LoadManifestDescriptor loads the manifest descriptor to be used to retrieve the image name
// when pulling an image
func LoadManifestDescriptor(imgRef types.ImageReference) (imgspecv1.Descriptor, error) {
	ociRef, ok := imgRef.(ociReference)
	if !ok {
		return imgspecv1.Descriptor{}, errors.New("error typecasting, need type ociRef")
	}
	md, _, err := ociRef.getManifestDescriptor()
	return md, err
}

// NewImageSource returns a types.ImageSource for this reference.
// The caller must call .Close() on the returned ImageSource.
func (ref ociReference) NewImageSource(ctx context.Context, sys *types.SystemContext) (types.ImageSource, error) {
	return newImageSource(sys, ref)
}

// NewImageDestination returns a types.ImageDestination for this reference.
// The caller must call .Close() on the returned ImageDestination.
func (ref ociReference) NewImageDestination(ctx context.Context, sys *types.SystemContext) (types.ImageDestination, error) {
	return newImageDestination(sys, ref)
}

// ociLayoutPath returns a path for the oci-layout within a directory using OCI conventions.
func (ref ociReference) ociLayoutPath() string {
	return filepath.Join(ref.dir, imgspecv1.ImageLayoutFile)
}

// indexPath returns a path for the index.json within a directory using OCI conventions.
func (ref ociReference) indexPath() string {
	return filepath.Join(ref.dir, imgspecv1.ImageIndexFile)
}

// blobPath returns a path for a blob within a directory using OCI image-layout conventions.
func (ref ociReference) blobPath(digest digest.Digest, sharedBlobDir string) (string, error) {
	if err := digest.Validate(); err != nil {
		return "", fmt.Errorf("unexpected digest reference %s: %w", digest, err)
	}
	var blobDir string
	if sharedBlobDir != "" {
		blobDir = sharedBlobDir
	} else {
		blobDir = filepath.Join(ref.dir, imgspecv1.ImageBlobsDir)
	}
	return filepath.Join(blobDir, digest.Algorithm().String(), digest.Encoded()), nil
}
