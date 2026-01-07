package openshift

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.podman.io/image/v5/docker/policyconfiguration"
	"go.podman.io/image/v5/docker/reference"
	genericImage "go.podman.io/image/v5/internal/image"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/regexp"
)

func init() {
	transports.Register(Transport)
}

// Transport is an ImageTransport for OpenShift registry-hosted images.
var Transport = openshiftTransport{}

type openshiftTransport struct{}

func (t openshiftTransport) Name() string {
	return "atomic"
}

// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an ImageReference.
func (t openshiftTransport) ParseReference(reference string) (types.ImageReference, error) {
	return ParseReference(reference)
}

// Note that imageNameRegexp is namespace/stream:tag, this
// is HOSTNAME/namespace/stream:tag or parent prefixes.
// Keep this in sync with imageNameRegexp!
var scopeRegexp = regexp.Delayed("^[^/]*(/[^:/]*(/[^:/]*(:[^:/]*)?)?)?$")

// ValidatePolicyConfigurationScope checks that scope is a valid name for a signature.PolicyTransportScopes keys
// (i.e. a valid PolicyConfigurationIdentity() or PolicyConfigurationNamespaces() return value).
// It is acceptable to allow an invalid value which will never be matched, it can "only" cause user confusion.
// scope passed to this function will not be "", that value is always allowed.
func (t openshiftTransport) ValidatePolicyConfigurationScope(scope string) error {
	if scopeRegexp.FindStringIndex(scope) == nil {
		return fmt.Errorf("Invalid scope name %s", scope)
	}
	return nil
}

// openshiftReference is an ImageReference for OpenShift images.
type openshiftReference struct {
	dockerReference reference.NamedTagged
	namespace       string // Computed from dockerReference in advance.
	stream          string // Computed from dockerReference in advance.
}

// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an OpenShift ImageReference.
func ParseReference(ref string) (types.ImageReference, error) {
	r, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference %q: %w", ref, err)
	}
	tagged, ok := r.(reference.NamedTagged)
	if !ok {
		return nil, fmt.Errorf("invalid image reference %s, expected format: 'hostname/namespace/stream:tag'", ref)
	}
	return NewReference(tagged)
}

// NewReference returns an OpenShift reference for a reference.NamedTagged
func NewReference(dockerRef reference.NamedTagged) (types.ImageReference, error) {
	r := strings.SplitN(reference.Path(dockerRef), "/", 3)
	if len(r) != 2 {
		return nil, fmt.Errorf("invalid image reference: %s, expected format: 'hostname/namespace/stream:tag'",
			reference.FamiliarString(dockerRef))
	}
	return openshiftReference{
		namespace:       r[0],
		stream:          r[1],
		dockerReference: dockerRef,
	}, nil
}

func (ref openshiftReference) Transport() types.ImageTransport {
	return Transport
}

// StringWithinTransport returns a string representation of the reference, which MUST be such that
// reference.Transport().ParseReference(reference.StringWithinTransport()) returns an equivalent reference.
// NOTE: The returned string is not promised to be equal to the original input to ParseReference;
// e.g. default attribute values omitted by the user may be filled in the return value, or vice versa.
// WARNING: Do not use the return value in the UI to describe an image, it does not contain the Transport().Name() prefix.
func (ref openshiftReference) StringWithinTransport() string {
	return reference.FamiliarString(ref.dockerReference)
}

// DockerReference returns a Docker reference associated with this reference
// (fully explicit, i.e. !reference.IsNameOnly, but reflecting user intent,
// not e.g. after redirect or alias processing), or nil if unknown/not applicable.
func (ref openshiftReference) DockerReference() reference.Named {
	return ref.dockerReference
}

// PolicyConfigurationIdentity returns a string representation of the reference, suitable for policy lookup.
// This MUST reflect user intent, not e.g. after processing of third-party redirects or aliases;
// The value SHOULD be fully explicit about its semantics, with no hidden defaults, AND canonical
// (i.e. various references with exactly the same semantics should return the same configuration identity)
// It is fine for the return value to be equal to StringWithinTransport(), and it is desirable but
// not required/guaranteed that it will be a valid input to Transport().ParseReference().
// Returns "" if configuration identities for these references are not supported.
func (ref openshiftReference) PolicyConfigurationIdentity() string {
	res, err := policyconfiguration.DockerReferenceIdentity(ref.dockerReference)
	if res == "" || err != nil { // Coverage: Should never happen, NewReference constructs a valid tagged reference.
		panic(fmt.Sprintf("Internal inconsistency: policyconfiguration.DockerReferenceIdentity returned %#v, %v", res, err))
	}
	return res
}

// PolicyConfigurationNamespaces returns a list of other policy configuration namespaces to search
// for if explicit configuration for PolicyConfigurationIdentity() is not set.  The list will be processed
// in order, terminating on first match, and an implicit "" is always checked at the end.
// It is STRONGLY recommended for the first element, if any, to be a prefix of PolicyConfigurationIdentity(),
// and each following element to be a prefix of the element preceding it.
func (ref openshiftReference) PolicyConfigurationNamespaces() []string {
	return policyconfiguration.DockerReferenceNamespaces(ref.dockerReference)
}

// NewImage returns a types.ImageCloser for this reference, possibly specialized for this ImageTransport.
// The caller must call .Close() on the returned ImageCloser.
// NOTE: If any kind of signature verification should happen, build an UnparsedImage from the value returned by NewImageSource,
// verify that UnparsedImage, and convert it into a real Image via image.FromUnparsedImage.
// WARNING: This may not do the right thing for a manifest list, see image.FromSource for details.
func (ref openshiftReference) NewImage(ctx context.Context, sys *types.SystemContext) (types.ImageCloser, error) {
	return genericImage.FromReference(ctx, sys, ref)
}

// NewImageSource returns a types.ImageSource for this reference.
// The caller must call .Close() on the returned ImageSource.
func (ref openshiftReference) NewImageSource(ctx context.Context, sys *types.SystemContext) (types.ImageSource, error) {
	return newImageSource(sys, ref)
}

// NewImageDestination returns a types.ImageDestination for this reference.
// The caller must call .Close() on the returned ImageDestination.
func (ref openshiftReference) NewImageDestination(ctx context.Context, sys *types.SystemContext) (types.ImageDestination, error) {
	return newImageDestination(ctx, sys, ref)
}

// DeleteImage deletes the named image from the registry, if supported.
func (ref openshiftReference) DeleteImage(ctx context.Context, sys *types.SystemContext) error {
	return errors.New("Deleting images not implemented for atomic: images")
}
