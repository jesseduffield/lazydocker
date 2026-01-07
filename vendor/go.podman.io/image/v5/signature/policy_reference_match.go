// PolicyReferenceMatch implementations.

package signature

import (
	"fmt"
	"strings"

	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/transports"
)

// parseImageAndDockerReference converts an image and a reference string into two parsed entities, failing on any error and handling unidentified images.
func parseImageAndDockerReference(image private.UnparsedImage, s2 string) (reference.Named, reference.Named, error) {
	r1 := image.Reference().DockerReference()
	if r1 == nil {
		return nil, nil, PolicyRequirementError(fmt.Sprintf("Docker reference match attempted on image %s with no known Docker reference identity",
			transports.ImageName(image.Reference())))
	}
	r2, err := reference.ParseNormalizedNamed(s2)
	if err != nil {
		return nil, nil, err
	}
	return r1, r2, nil
}

func (prm *prmMatchExact) matchesDockerReference(image private.UnparsedImage, signatureDockerReference string) bool {
	intended, signature, err := parseImageAndDockerReference(image, signatureDockerReference)
	if err != nil {
		return false
	}
	// Do not add default tags: image.Reference().DockerReference() should contain it already, and signatureDockerReference should be exact; so, verify that now.
	if reference.IsNameOnly(intended) || reference.IsNameOnly(signature) {
		return false
	}
	return signature.String() == intended.String()
}

// matchRepoDigestOrExactReferenceValues implements prmMatchRepoDigestOrExact.matchesDockerReference
// using reference.Named values.
func matchRepoDigestOrExactReferenceValues(intended, signature reference.Named) bool {
	// Do not add default tags: image.Reference().DockerReference() should contain it already, and signatureDockerReference should be exact; so, verify that now.
	if reference.IsNameOnly(signature) {
		return false
	}
	switch intended.(type) {
	case reference.NamedTagged: // Includes the case when intended has both a tag and a digest.
		return signature.String() == intended.String()
	case reference.Canonical:
		// We don’t actually compare the manifest digest against the signature here; that happens prSignedBy.in UnparsedImage.Manifest.
		// Because UnparsedImage.Manifest verifies the intended.Digest() against the manifest, and prSignedBy verifies the signature digest against the manifest,
		// we know that signature digest matches intended.Digest() (but intended.Digest() and signature digest may use different algorithms)
		return signature.Name() == intended.Name()
	default: // !reference.IsNameOnly(intended)
		return false
	}
}
func (prm *prmMatchRepoDigestOrExact) matchesDockerReference(image private.UnparsedImage, signatureDockerReference string) bool {
	intended, signature, err := parseImageAndDockerReference(image, signatureDockerReference)
	if err != nil {
		return false
	}
	return matchRepoDigestOrExactReferenceValues(intended, signature)
}

func (prm *prmMatchRepository) matchesDockerReference(image private.UnparsedImage, signatureDockerReference string) bool {
	intended, signature, err := parseImageAndDockerReference(image, signatureDockerReference)
	if err != nil {
		return false
	}
	return signature.Name() == intended.Name()
}

// parseDockerReferences converts two reference strings into parsed entities, failing on any error
func parseDockerReferences(s1, s2 string) (reference.Named, reference.Named, error) {
	r1, err := reference.ParseNormalizedNamed(s1)
	if err != nil {
		return nil, nil, err
	}
	r2, err := reference.ParseNormalizedNamed(s2)
	if err != nil {
		return nil, nil, err
	}
	return r1, r2, nil
}

func (prm *prmExactReference) matchesDockerReference(image private.UnparsedImage, signatureDockerReference string) bool {
	intended, signature, err := parseDockerReferences(prm.DockerReference, signatureDockerReference)
	if err != nil {
		return false
	}
	// prm.DockerReference and signatureDockerReference should be exact; so, verify that now.
	if reference.IsNameOnly(intended) || reference.IsNameOnly(signature) {
		return false
	}
	return signature.String() == intended.String()
}

func (prm *prmExactRepository) matchesDockerReference(image private.UnparsedImage, signatureDockerReference string) bool {
	intended, signature, err := parseDockerReferences(prm.DockerRepository, signatureDockerReference)
	if err != nil {
		return false
	}
	return signature.Name() == intended.Name()
}

// refMatchesPrefix returns true if ref matches prm.Prefix.
func (prm *prmRemapIdentity) refMatchesPrefix(ref reference.Named) bool {
	name := ref.Name()
	switch {
	case len(name) < len(prm.Prefix):
		return false
	case len(name) == len(prm.Prefix):
		return name == prm.Prefix
	case len(name) > len(prm.Prefix):
		// We are matching only ref.Name(), not ref.String(), so the only separator we are
		// expecting is '/':
		// - '@' is only valid to separate a digest, i.e. not a part of ref.Name()
		// - similarly ':' to mark a tag would not be a part of ref.Name(); it can be a part of a
		//   host:port domain syntax, but we don't treat that specially and require an exact match
		//   of the domain.
		return strings.HasPrefix(name, prm.Prefix) && name[len(prm.Prefix)] == '/'
	default:
		panic("Internal error: impossible comparison outcome")
	}
}

// remapReferencePrefix returns the result of remapping ref, if it matches prm.Prefix
// or the original ref if it does not.
func (prm *prmRemapIdentity) remapReferencePrefix(ref reference.Named) (reference.Named, error) {
	if !prm.refMatchesPrefix(ref) {
		return ref, nil
	}
	refString := ref.String()
	newNamedRef := strings.Replace(refString, prm.Prefix, prm.SignedPrefix, 1)
	newParsedRef, err := reference.ParseNamed(newNamedRef)
	if err != nil {
		return nil, fmt.Errorf(`error rewriting reference from %q to %q: %w`, refString, newNamedRef, err)
	}
	return newParsedRef, nil
}

func (prm *prmRemapIdentity) matchesDockerReference(image private.UnparsedImage, signatureDockerReference string) bool {
	intended, signature, err := parseImageAndDockerReference(image, signatureDockerReference)
	if err != nil {
		return false
	}
	intended, err = prm.remapReferencePrefix(intended)
	if err != nil {
		return false
	}
	return matchRepoDigestOrExactReferenceValues(intended, signature)
}
