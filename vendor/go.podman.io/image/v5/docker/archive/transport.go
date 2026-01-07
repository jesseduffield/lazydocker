package archive

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.podman.io/image/v5/docker/internal/tarfile"
	"go.podman.io/image/v5/docker/reference"
	ctrImage "go.podman.io/image/v5/internal/image"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
)

func init() {
	transports.Register(Transport)
}

// Transport is an ImageTransport for local Docker archives.
var Transport = archiveTransport{}

type archiveTransport struct{}

func (t archiveTransport) Name() string {
	return "docker-archive"
}

// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an ImageReference.
func (t archiveTransport) ParseReference(reference string) (types.ImageReference, error) {
	return ParseReference(reference)
}

// ValidatePolicyConfigurationScope checks that scope is a valid name for a signature.PolicyTransportScopes keys
// (i.e. a valid PolicyConfigurationIdentity() or PolicyConfigurationNamespaces() return value).
// It is acceptable to allow an invalid value which will never be matched, it can "only" cause user confusion.
// scope passed to this function will not be "", that value is always allowed.
func (t archiveTransport) ValidatePolicyConfigurationScope(scope string) error {
	// See the explanation in archiveReference.PolicyConfigurationIdentity.
	return errors.New(`docker-archive: does not support any scopes except the default "" one`)
}

// archiveReference is an ImageReference for Docker images.
type archiveReference struct {
	path string
	// May be nil to read the only image in an archive, or to create an untagged image.
	ref reference.NamedTagged
	// If not -1, a zero-based index of the image in the manifest. Valid only for sources.
	// Must not be set if ref is set.
	sourceIndex int
	// If not nil, must have been created from path (but archiveReader.path may point at a temporary
	// file, not necessarily path precisely).
	archiveReader *tarfile.Reader
	// If not nil, must have been created for path
	writer *Writer
}

// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an Docker ImageReference.
func ParseReference(refString string) (types.ImageReference, error) {
	if refString == "" {
		return nil, fmt.Errorf("docker-archive reference %s isn't of the form <path>[:<reference>]", refString)
	}

	path, tagOrIndex, gotTagOrIndex := strings.Cut(refString, ":")
	var nt reference.NamedTagged
	sourceIndex := -1

	if gotTagOrIndex {
		// A :tag or :@index was specified.
		if len(tagOrIndex) > 0 && tagOrIndex[0] == '@' {
			i, err := strconv.Atoi(tagOrIndex[1:])
			if err != nil {
				return nil, fmt.Errorf("Invalid source index %s: %w", tagOrIndex, err)
			}
			if i < 0 {
				return nil, fmt.Errorf("Invalid source index @%d: must not be negative", i)
			}
			sourceIndex = i
		} else {
			ref, err := reference.ParseNormalizedNamed(tagOrIndex)
			if err != nil {
				return nil, fmt.Errorf("docker-archive parsing reference: %w", err)
			}
			ref = reference.TagNameOnly(ref)
			refTagged, isTagged := ref.(reference.NamedTagged)
			if !isTagged { // If ref contains a digest, TagNameOnly does not change it
				return nil, fmt.Errorf("reference does not include a tag: %s", ref.String())
			}
			nt = refTagged
		}
	}

	return newReference(path, nt, sourceIndex, nil, nil)
}

// NewReference returns a Docker archive reference for a path and an optional reference.
func NewReference(path string, ref reference.NamedTagged) (types.ImageReference, error) {
	return newReference(path, ref, -1, nil, nil)
}

// NewIndexReference returns a Docker archive reference for a path and a zero-based source manifest index.
func NewIndexReference(path string, sourceIndex int) (types.ImageReference, error) {
	if sourceIndex < 0 {
		return nil, fmt.Errorf("invalid call to NewIndexReference with negative index %d", sourceIndex)
	}
	return newReference(path, nil, sourceIndex, nil, nil)
}

// newReference returns a docker archive reference for a path, an optional reference or sourceIndex,
// and optionally a tarfile.Reader and/or a tarfile.Writer matching path.
func newReference(path string, ref reference.NamedTagged, sourceIndex int,
	archiveReader *tarfile.Reader, writer *Writer) (types.ImageReference, error) {
	if strings.Contains(path, ":") {
		return nil, fmt.Errorf("Invalid docker-archive: reference: colon in path %q is not supported", path)
	}
	if ref != nil && sourceIndex != -1 {
		return nil, fmt.Errorf("Invalid docker-archive: reference: cannot use both a tag and a source index")
	}
	if _, isDigest := ref.(reference.Canonical); isDigest {
		return nil, fmt.Errorf("docker-archive doesn't support digest references: %s", ref.String())
	}
	if sourceIndex != -1 && sourceIndex < 0 {
		return nil, fmt.Errorf("Invalid docker-archive: reference: index @%d must not be negative", sourceIndex)
	}
	return archiveReference{
		path:          path,
		ref:           ref,
		sourceIndex:   sourceIndex,
		archiveReader: archiveReader,
		writer:        writer,
	}, nil
}

func (ref archiveReference) Transport() types.ImageTransport {
	return Transport
}

// StringWithinTransport returns a string representation of the reference, which MUST be such that
// reference.Transport().ParseReference(reference.StringWithinTransport()) returns an equivalent reference.
// NOTE: The returned string is not promised to be equal to the original input to ParseReference;
// e.g. default attribute values omitted by the user may be filled in the return value, or vice versa.
// WARNING: Do not use the return value in the UI to describe an image, it does not contain the Transport().Name() prefix.
func (ref archiveReference) StringWithinTransport() string {
	switch {
	case ref.ref != nil:
		return fmt.Sprintf("%s:%s", ref.path, ref.ref.String())
	case ref.sourceIndex != -1:
		return fmt.Sprintf("%s:@%d", ref.path, ref.sourceIndex)
	default:
		return ref.path
	}
}

// DockerReference returns a Docker reference associated with this reference
// (fully explicit, i.e. !reference.IsNameOnly, but reflecting user intent,
// not e.g. after redirect or alias processing), or nil if unknown/not applicable.
func (ref archiveReference) DockerReference() reference.Named {
	return ref.ref
}

// PolicyConfigurationIdentity returns a string representation of the reference, suitable for policy lookup.
// This MUST reflect user intent, not e.g. after processing of third-party redirects or aliases;
// The value SHOULD be fully explicit about its semantics, with no hidden defaults, AND canonical
// (i.e. various references with exactly the same semantics should return the same configuration identity)
// It is fine for the return value to be equal to StringWithinTransport(), and it is desirable but
// not required/guaranteed that it will be a valid input to Transport().ParseReference().
// Returns "" if configuration identities for these references are not supported.
func (ref archiveReference) PolicyConfigurationIdentity() string {
	// Punt, the justification is similar to dockerReference.PolicyConfigurationIdentity.
	return ""
}

// PolicyConfigurationNamespaces returns a list of other policy configuration namespaces to search
// for if explicit configuration for PolicyConfigurationIdentity() is not set.  The list will be processed
// in order, terminating on first match, and an implicit "" is always checked at the end.
// It is STRONGLY recommended for the first element, if any, to be a prefix of PolicyConfigurationIdentity(),
// and each following element to be a prefix of the element preceding it.
func (ref archiveReference) PolicyConfigurationNamespaces() []string {
	// TODO
	return []string{}
}

// NewImage returns a types.ImageCloser for this reference, possibly specialized for this ImageTransport.
// The caller must call .Close() on the returned ImageCloser.
// NOTE: If any kind of signature verification should happen, build an UnparsedImage from the value returned by NewImageSource,
// verify that UnparsedImage, and convert it into a real Image via image.FromUnparsedImage.
// WARNING: This may not do the right thing for a manifest list, see image.FromSource for details.
func (ref archiveReference) NewImage(ctx context.Context, sys *types.SystemContext) (types.ImageCloser, error) {
	return ctrImage.FromReference(ctx, sys, ref)
}

// NewImageSource returns a types.ImageSource for this reference.
// The caller must call .Close() on the returned ImageSource.
func (ref archiveReference) NewImageSource(ctx context.Context, sys *types.SystemContext) (types.ImageSource, error) {
	return newImageSource(sys, ref)
}

// NewImageDestination returns a types.ImageDestination for this reference.
// The caller must call .Close() on the returned ImageDestination.
func (ref archiveReference) NewImageDestination(ctx context.Context, sys *types.SystemContext) (types.ImageDestination, error) {
	return newImageDestination(sys, ref)
}

// DeleteImage deletes the named image from the registry, if supported.
func (ref archiveReference) DeleteImage(ctx context.Context, sys *types.SystemContext) error {
	// Not really supported, for safety reasons.
	return errors.New("Deleting images not implemented for docker-archive: images")
}
