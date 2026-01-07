package directory

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/directory/explicitfilepath"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/image"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/types"
)

func init() {
	transports.Register(Transport)
}

// Transport is an ImageTransport for directory paths.
var Transport = dirTransport{}

type dirTransport struct{}

func (t dirTransport) Name() string {
	return "dir"
}

// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an ImageReference.
func (t dirTransport) ParseReference(reference string) (types.ImageReference, error) {
	return NewReference(reference)
}

// ValidatePolicyConfigurationScope checks that scope is a valid name for a signature.PolicyTransportScopes keys
// (i.e. a valid PolicyConfigurationIdentity() or PolicyConfigurationNamespaces() return value).
// It is acceptable to allow an invalid value which will never be matched, it can "only" cause user confusion.
// scope passed to this function will not be "", that value is always allowed.
func (t dirTransport) ValidatePolicyConfigurationScope(scope string) error {
	if !strings.HasPrefix(scope, "/") {
		return fmt.Errorf("Invalid scope %s: Must be an absolute path", scope)
	}
	// Refuse also "/", otherwise "/" and "" would have the same semantics,
	// and "" could be unexpectedly shadowed by the "/" entry.
	if scope == "/" {
		return errors.New(`Invalid scope "/": Use the generic default scope ""`)
	}
	cleaned := filepath.Clean(scope)
	if cleaned != scope {
		return fmt.Errorf(`Invalid scope %s: Uses non-canonical format, perhaps try %s`, scope, cleaned)
	}
	return nil
}

// dirReference is an ImageReference for directory paths.
type dirReference struct {
	// Note that the interpretation of paths below depends on the underlying filesystem state, which may change under us at any time!
	// Either of the paths may point to a different, or no, inode over time.  resolvedPath may contain symbolic links, and so on.

	// Generally we follow the intent of the user, and use the "path" member for filesystem operations (e.g. the user can use a relative path to avoid
	// being exposed to symlinks and renames in the parent directories to the working directory).
	// (But in general, we make no attempt to be completely safe against concurrent hostile filesystem modifications.)
	path         string // As specified by the user. May be relative, contain symlinks, etc.
	resolvedPath string // Absolute path with no symlinks, at least at the time of its creation. Primarily used for policy namespaces.
}

// There is no directory.ParseReference because it is rather pointless.
// Callers who need a transport-independent interface will go through
// dirTransport.ParseReference; callers who intentionally deal with directories
// can use directory.NewReference.

// NewReference returns a directory reference for a specified path.
//
// We do not expose an API supplying the resolvedPath; we could, but recomputing it
// is generally cheap enough that we prefer being confident about the properties of resolvedPath.
func NewReference(path string) (types.ImageReference, error) {
	resolved, err := explicitfilepath.ResolvePathToFullyExplicit(path)
	if err != nil {
		return nil, err
	}
	return dirReference{path: path, resolvedPath: resolved}, nil
}

func (ref dirReference) Transport() types.ImageTransport {
	return Transport
}

// StringWithinTransport returns a string representation of the reference, which MUST be such that
// reference.Transport().ParseReference(reference.StringWithinTransport()) returns an equivalent reference.
// NOTE: The returned string is not promised to be equal to the original input to ParseReference;
// e.g. default attribute values omitted by the user may be filled in the return value, or vice versa.
// WARNING: Do not use the return value in the UI to describe an image, it does not contain the Transport().Name() prefix.
func (ref dirReference) StringWithinTransport() string {
	return ref.path
}

// DockerReference returns a Docker reference associated with this reference
// (fully explicit, i.e. !reference.IsNameOnly, but reflecting user intent,
// not e.g. after redirect or alias processing), or nil if unknown/not applicable.
func (ref dirReference) DockerReference() reference.Named {
	return nil
}

// PolicyConfigurationIdentity returns a string representation of the reference, suitable for policy lookup.
// This MUST reflect user intent, not e.g. after processing of third-party redirects or aliases;
// The value SHOULD be fully explicit about its semantics, with no hidden defaults, AND canonical
// (i.e. various references with exactly the same semantics should return the same configuration identity)
// It is fine for the return value to be equal to StringWithinTransport(), and it is desirable but
// not required/guaranteed that it will be a valid input to Transport().ParseReference().
// Returns "" if configuration identities for these references are not supported.
func (ref dirReference) PolicyConfigurationIdentity() string {
	return ref.resolvedPath
}

// PolicyConfigurationNamespaces returns a list of other policy configuration namespaces to search
// for if explicit configuration for PolicyConfigurationIdentity() is not set.  The list will be processed
// in order, terminating on first match, and an implicit "" is always checked at the end.
// It is STRONGLY recommended for the first element, if any, to be a prefix of PolicyConfigurationIdentity(),
// and each following element to be a prefix of the element preceding it.
func (ref dirReference) PolicyConfigurationNamespaces() []string {
	res := []string{}
	path := ref.resolvedPath
	for {
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash == -1 || lastSlash == 0 {
			break
		}
		path = path[:lastSlash]
		res = append(res, path)
	}
	// Note that we do not include "/"; it is redundant with the default "" global default,
	// and rejected by dirTransport.ValidatePolicyConfigurationScope above.
	return res
}

// NewImage returns a types.ImageCloser for this reference, possibly specialized for this ImageTransport.
// The caller must call .Close() on the returned ImageCloser.
// NOTE: If any kind of signature verification should happen, build an UnparsedImage from the value returned by NewImageSource,
// verify that UnparsedImage, and convert it into a real Image via image.FromUnparsedImage.
// WARNING: This may not do the right thing for a manifest list, see image.FromSource for details.
func (ref dirReference) NewImage(ctx context.Context, sys *types.SystemContext) (types.ImageCloser, error) {
	return image.FromReference(ctx, sys, ref)
}

// NewImageSource returns a types.ImageSource for this reference.
// The caller must call .Close() on the returned ImageSource.
func (ref dirReference) NewImageSource(ctx context.Context, sys *types.SystemContext) (types.ImageSource, error) {
	return newImageSource(ref), nil
}

// NewImageDestination returns a types.ImageDestination for this reference.
// The caller must call .Close() on the returned ImageDestination.
func (ref dirReference) NewImageDestination(ctx context.Context, sys *types.SystemContext) (types.ImageDestination, error) {
	return newImageDestination(sys, ref)
}

// DeleteImage deletes the named image from the registry, if supported.
func (ref dirReference) DeleteImage(ctx context.Context, sys *types.SystemContext) error {
	return errors.New("Deleting images not implemented for dir: images")
}

// manifestPath returns a path for the manifest within a directory using our conventions.
func (ref dirReference) manifestPath(instanceDigest *digest.Digest) (string, error) {
	if instanceDigest != nil {
		if err := instanceDigest.Validate(); err != nil { // digest.Digest.Encoded() panics on failure, and could possibly result in a path with ../, so validate explicitly.
			return "", err
		}
		return filepath.Join(ref.path, instanceDigest.Encoded()+".manifest.json"), nil
	}
	return filepath.Join(ref.path, "manifest.json"), nil
}

// layerPath returns a path for a layer tarball within a directory using our conventions.
func (ref dirReference) layerPath(digest digest.Digest) (string, error) {
	if err := digest.Validate(); err != nil { // digest.Digest.Encoded() panics on failure, and could possibly result in a path with ../, so validate explicitly.
		return "", err
	}
	// FIXME: Should we keep the digest identification?
	return filepath.Join(ref.path, digest.Encoded()), nil
}

// signaturePath returns a path for a signature within a directory using our conventions.
func (ref dirReference) signaturePath(index int, instanceDigest *digest.Digest) (string, error) {
	if instanceDigest != nil {
		if err := instanceDigest.Validate(); err != nil { // digest.Digest.Encoded() panics on failure, and could possibly result in a path with ../, so validate explicitly.
			return "", err
		}
		return filepath.Join(ref.path, fmt.Sprintf(instanceDigest.Encoded()+".signature-%d", index+1)), nil
	}
	return filepath.Join(ref.path, fmt.Sprintf("signature-%d", index+1)), nil
}

// versionPath returns a path for the version file within a directory using our conventions.
func (ref dirReference) versionPath() string {
	return filepath.Join(ref.path, "version")
}
