package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.podman.io/image/v5/docker/policyconfiguration"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
)

// UnknownDigestSuffix can be appended to a reference when the caller
// wants to push an image without a tag or digest.
// NewReferenceUnknownDigest() is called when this const is detected.
const UnknownDigestSuffix = "@@unknown-digest@@"

func init() {
	transports.Register(Transport)
}

// Transport is an ImageTransport for container registry-hosted images.
var Transport = dockerTransport{}

type dockerTransport struct{}

func (t dockerTransport) Name() string {
	return "docker"
}

// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an ImageReference.
func (t dockerTransport) ParseReference(reference string) (types.ImageReference, error) {
	return ParseReference(reference)
}

// ValidatePolicyConfigurationScope checks that scope is a valid name for a signature.PolicyTransportScopes keys
// (i.e. a valid PolicyConfigurationIdentity() or PolicyConfigurationNamespaces() return value).
// It is acceptable to allow an invalid value which will never be matched, it can "only" cause user confusion.
// scope passed to this function will not be "", that value is always allowed.
func (t dockerTransport) ValidatePolicyConfigurationScope(scope string) error {
	// FIXME? We could be verifying the various character set and length restrictions
	// from docker/distribution/reference.regexp.go, but other than that there
	// are few semantically invalid strings.
	return nil
}

// dockerReference is an ImageReference for Docker images.
type dockerReference struct {
	ref             reference.Named // By construction we know that !reference.IsNameOnly(ref) unless isUnknownDigest=true
	isUnknownDigest bool
}

// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an Docker ImageReference.
func ParseReference(refString string) (types.ImageReference, error) {
	refString, ok := strings.CutPrefix(refString, "//")
	if !ok {
		return nil, fmt.Errorf("docker: image reference %s does not start with //", refString)
	}
	refString, unknownDigest := strings.CutSuffix(refString, UnknownDigestSuffix)
	ref, err := reference.ParseNormalizedNamed(refString)
	if err != nil {
		return nil, err
	}

	if unknownDigest {
		if !reference.IsNameOnly(ref) {
			return nil, fmt.Errorf("docker: image reference %q has unknown digest set but it contains either a tag or digest", ref.String()+UnknownDigestSuffix)
		}
		return NewReferenceUnknownDigest(ref)
	}

	ref = reference.TagNameOnly(ref)
	return NewReference(ref)
}

// NewReference returns a Docker reference for a named reference. The reference must satisfy !reference.IsNameOnly().
func NewReference(ref reference.Named) (types.ImageReference, error) {
	return newReference(ref, false)
}

// NewReferenceUnknownDigest returns a Docker reference for a named reference, which can be used to write images without setting
// a tag on the registry. The reference must satisfy reference.IsNameOnly()
func NewReferenceUnknownDigest(ref reference.Named) (types.ImageReference, error) {
	return newReference(ref, true)
}

// newReference returns a dockerReference for a named reference.
func newReference(ref reference.Named, unknownDigest bool) (dockerReference, error) {
	if reference.IsNameOnly(ref) && !unknownDigest {
		return dockerReference{}, fmt.Errorf("Docker reference %s is not for an unknown digest case; tag or digest is needed", reference.FamiliarString(ref))
	}
	if !reference.IsNameOnly(ref) && unknownDigest {
		return dockerReference{}, fmt.Errorf("Docker reference %s is for an unknown digest case but reference has a tag or digest", reference.FamiliarString(ref))
	}
	// A github.com/distribution/reference value can have a tag and a digest at the same time!
	// The docker/distribution API does not really support that (we can’t ask for an image with a specific
	// tag and digest), so fail.  This MAY be accepted in the future.
	// (Even if it were supported, the semantics of policy namespaces are unclear - should we drop
	// the tag or the digest first?)
	_, isTagged := ref.(reference.NamedTagged)
	_, isDigested := ref.(reference.Canonical)
	if isTagged && isDigested {
		return dockerReference{}, errors.New("Docker references with both a tag and digest are currently not supported")
	}

	return dockerReference{
		ref:             ref,
		isUnknownDigest: unknownDigest,
	}, nil
}

func (ref dockerReference) Transport() types.ImageTransport {
	return Transport
}

// StringWithinTransport returns a string representation of the reference, which MUST be such that
// reference.Transport().ParseReference(reference.StringWithinTransport()) returns an equivalent reference.
// NOTE: The returned string is not promised to be equal to the original input to ParseReference;
// e.g. default attribute values omitted by the user may be filled in the return value, or vice versa.
// WARNING: Do not use the return value in the UI to describe an image, it does not contain the Transport().Name() prefix.
func (ref dockerReference) StringWithinTransport() string {
	famString := "//" + reference.FamiliarString(ref.ref)
	if ref.isUnknownDigest {
		return famString + UnknownDigestSuffix
	}
	return famString
}

// DockerReference returns a Docker reference associated with this reference
// (fully explicit, i.e. !reference.IsNameOnly, but reflecting user intent,
// not e.g. after redirect or alias processing), or nil if unknown/not applicable.
func (ref dockerReference) DockerReference() reference.Named {
	return ref.ref
}

// PolicyConfigurationIdentity returns a string representation of the reference, suitable for policy lookup.
// This MUST reflect user intent, not e.g. after processing of third-party redirects or aliases;
// The value SHOULD be fully explicit about its semantics, with no hidden defaults, AND canonical
// (i.e. various references with exactly the same semantics should return the same configuration identity)
// It is fine for the return value to be equal to StringWithinTransport(), and it is desirable but
// not required/guaranteed that it will be a valid input to Transport().ParseReference().
// Returns "" if configuration identities for these references are not supported.
func (ref dockerReference) PolicyConfigurationIdentity() string {
	if ref.isUnknownDigest {
		return ref.ref.Name()
	}
	res, err := policyconfiguration.DockerReferenceIdentity(ref.ref)
	if res == "" || err != nil { // Coverage: Should never happen, NewReference above should refuse values which could cause a failure.
		panic(fmt.Sprintf("Internal inconsistency: policyconfiguration.DockerReferenceIdentity returned %#v, %v", res, err))
	}
	return res
}

// PolicyConfigurationNamespaces returns a list of other policy configuration namespaces to search
// for if explicit configuration for PolicyConfigurationIdentity() is not set.  The list will be processed
// in order, terminating on first match, and an implicit "" is always checked at the end.
// It is STRONGLY recommended for the first element, if any, to be a prefix of PolicyConfigurationIdentity(),
// and each following element to be a prefix of the element preceding it.
func (ref dockerReference) PolicyConfigurationNamespaces() []string {
	namespaces := policyconfiguration.DockerReferenceNamespaces(ref.ref)
	if ref.isUnknownDigest {
		if len(namespaces) != 0 && namespaces[0] == ref.ref.Name() {
			namespaces = namespaces[1:]
		}
	}
	return namespaces
}

// NewImage returns a types.ImageCloser for this reference, possibly specialized for this ImageTransport.
// The caller must call .Close() on the returned ImageCloser.
// NOTE: If any kind of signature verification should happen, build an UnparsedImage from the value returned by NewImageSource,
// verify that UnparsedImage, and convert it into a real Image via image.FromUnparsedImage.
// WARNING: This may not do the right thing for a manifest list, see image.FromSource for details.
func (ref dockerReference) NewImage(ctx context.Context, sys *types.SystemContext) (types.ImageCloser, error) {
	return newImage(ctx, sys, ref)
}

// NewImageSource returns a types.ImageSource for this reference.
// The caller must call .Close() on the returned ImageSource.
func (ref dockerReference) NewImageSource(ctx context.Context, sys *types.SystemContext) (types.ImageSource, error) {
	return newImageSource(ctx, sys, ref)
}

// NewImageDestination returns a types.ImageDestination for this reference.
// The caller must call .Close() on the returned ImageDestination.
func (ref dockerReference) NewImageDestination(ctx context.Context, sys *types.SystemContext) (types.ImageDestination, error) {
	return newImageDestination(sys, ref)
}

// DeleteImage deletes the named image from the registry, if supported.
func (ref dockerReference) DeleteImage(ctx context.Context, sys *types.SystemContext) error {
	return deleteImage(ctx, sys, ref)
}

// tagOrDigest returns a tag or digest from the reference.
func (ref dockerReference) tagOrDigest() (string, error) {
	if ref, ok := ref.ref.(reference.Canonical); ok {
		return ref.Digest().String(), nil
	}
	if ref, ok := ref.ref.(reference.NamedTagged); ok {
		return ref.Tag(), nil
	}

	if ref.isUnknownDigest {
		return "", fmt.Errorf("Docker reference %q is for an unknown digest case, has neither a digest nor a tag", reference.FamiliarString(ref.ref))
	}
	// This should not happen, NewReference above refuses reference.IsNameOnly values.
	return "", fmt.Errorf("Internal inconsistency: Reference %s unexpectedly has neither a digest nor a tag", reference.FamiliarString(ref.ref))
}
