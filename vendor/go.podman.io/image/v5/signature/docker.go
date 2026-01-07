// Note: Consider the API unstable until the code supports at least three different image formats or transports.

package signature

import (
	"errors"
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/signature/internal"
)

// SignOptions includes optional parameters for signing container images.
type SignOptions struct {
	// Passphare to use when signing with the key identity.
	Passphrase string
}

// SignDockerManifest returns a signature for manifest as the specified dockerReference,
// using mech and keyIdentity, and the specified options.
func SignDockerManifestWithOptions(m []byte, dockerReference string, mech SigningMechanism, keyIdentity string, options *SignOptions) ([]byte, error) {
	manifestDigest, err := manifest.Digest(m)
	if err != nil {
		return nil, err
	}
	sig := newUntrustedSignature(manifestDigest, dockerReference)

	var passphrase string
	if options != nil {
		passphrase = options.Passphrase
		// The gpgme implementation can’t use passphrase with \n; reject it here for consistent behavior.
		if strings.Contains(passphrase, "\n") {
			return nil, errors.New("invalid passphrase: must not contain a line break")
		}
	}

	return sig.sign(mech, keyIdentity, passphrase)
}

// SignDockerManifest returns a signature for manifest as the specified dockerReference,
// using mech and keyIdentity.
func SignDockerManifest(m []byte, dockerReference string, mech SigningMechanism, keyIdentity string) ([]byte, error) {
	return SignDockerManifestWithOptions(m, dockerReference, mech, keyIdentity, nil)
}

// VerifyDockerManifestSignature checks that unverifiedSignature uses expectedKeyIdentity to sign unverifiedManifest as expectedDockerReference,
// using mech.
func VerifyDockerManifestSignature(unverifiedSignature, unverifiedManifest []byte,
	expectedDockerReference string, mech SigningMechanism, expectedKeyIdentity string) (*Signature, error) {
	sig, _, err := VerifyImageManifestSignatureUsingKeyIdentityList(unverifiedSignature, unverifiedManifest, expectedDockerReference, mech, []string{expectedKeyIdentity})
	return sig, err
}

// VerifyImageManifestSignatureUsingKeyIdentityList checks that unverifiedSignature uses one of the expectedKeyIdentities
// to sign unverifiedManifest as expectedDockerReference, using mech. Returns the verified signature and the key identity that
// was used to verify it.
func VerifyImageManifestSignatureUsingKeyIdentityList(unverifiedSignature, unverifiedManifest []byte,
	expectedDockerReference string, mech SigningMechanism, expectedKeyIdentities []string) (*Signature, string, error) {
	expectedRef, err := reference.ParseNormalizedNamed(expectedDockerReference)
	if err != nil {
		return nil, "", err
	}
	sig, matchedKeyIdentity, err := verifyAndExtractSignature(mech, unverifiedSignature, signatureAcceptanceRules{
		acceptedKeyIdentities: expectedKeyIdentities,
		validateSignedDockerReference: func(signedDockerReference string) error {
			signedRef, err := reference.ParseNormalizedNamed(signedDockerReference)
			if err != nil {
				return internal.NewInvalidSignatureError(fmt.Sprintf("Invalid docker reference %q in signature", signedDockerReference))
			}
			if signedRef.String() != expectedRef.String() {
				return internal.NewInvalidSignatureError(fmt.Sprintf("Docker reference %q does not match %q",
					signedDockerReference, expectedDockerReference))
			}
			return nil
		},
		validateSignedDockerManifestDigest: func(signedDockerManifestDigest digest.Digest) error {
			matches, err := manifest.MatchesDigest(unverifiedManifest, signedDockerManifestDigest)
			if err != nil {
				return err
			}
			if !matches {
				return internal.NewInvalidSignatureError(fmt.Sprintf("Signature for docker digest %q does not match", signedDockerManifestDigest))
			}
			return nil
		},
	})
	if err != nil {
		return nil, "", err
	}
	return sig, matchedKeyIdentity, err
}
