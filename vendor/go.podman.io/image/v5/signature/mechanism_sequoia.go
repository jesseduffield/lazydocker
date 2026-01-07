//go:build containers_image_sequoia

package signature

import (
	"go.podman.io/image/v5/signature/internal/sequoia"
)

// A GPG/OpenPGP signing mechanism, implemented using Sequoia and only supporting verification.
// Legacy users who reach newGPGSigningMechanismInDirectory will use GPGME.
// Signing using Sequoia is preferable, but should happen via signature/simplesequoia.NewSigner, not using
// the legacy mechanism API.
type sequoiaEphemeralSigningMechanism struct {
	inner *sequoia.SigningMechanism
}

// newEphemeralGPGSigningMechanism returns a new GPG/OpenPGP signing mechanism which
// recognizes _only_ public keys from the supplied blobs, and returns the identities
// of these keys.
// The caller must call .Close() on the returned SigningMechanism.
func newEphemeralGPGSigningMechanism(blobs [][]byte) (signingMechanismWithPassphrase, []string, error) {
	if err := sequoia.Init(); err != nil {
		return nil, nil, err // Coverage: This is impractical to test in-process, with the static go_sequoia_dlhandle.
	}

	mech, err := sequoia.NewEphemeralMechanism()
	if err != nil {
		return nil, nil, err
	}
	keyIdentities := []string{}
	for _, blob := range blobs {
		ki, err := mech.ImportKeys(blob)
		if err != nil {
			return nil, nil, err
		}
		keyIdentities = append(keyIdentities, ki...)
	}

	return &sequoiaEphemeralSigningMechanism{
		inner: mech,
	}, keyIdentities, nil
}

func (m *sequoiaEphemeralSigningMechanism) Close() error {
	return m.inner.Close()
}

// SupportsSigning returns nil if the mechanism supports signing, or a SigningNotSupportedError.
func (m *sequoiaEphemeralSigningMechanism) SupportsSigning() error {
	// This code is externally reachable via NewEphemeralGPGSigningMechanism(), but that API provides no way to
	// import or generate a key.
	return SigningNotSupportedError("caller error: Attempt to sign using a mechanism created via NewEphemeralGPGSigningMechanism().")
}

// Sign creates a (non-detached) signature of input using keyIdentity and passphrase.
// Fails with a SigningNotSupportedError if the mechanism does not support signing.
func (m *sequoiaEphemeralSigningMechanism) SignWithPassphrase(input []byte, keyIdentity string, passphrase string) ([]byte, error) {
	// This code is externally reachable via NewEphemeralGPGSigningMechanism(), but that API provides no way to
	// import or generate a key.
	return nil, SigningNotSupportedError("caller error: Attempt to sign using a mechanism created via NewEphemeralGPGSigningMechanism().")
}

// Sign creates a (non-detached) signature of input using keyIdentity.
// Fails with a SigningNotSupportedError if the mechanism does not support signing.
func (m *sequoiaEphemeralSigningMechanism) Sign(input []byte, keyIdentity string) ([]byte, error) {
	return m.SignWithPassphrase(input, keyIdentity, "")
}

// Verify parses unverifiedSignature and returns the content and the signer's identity.
// For mechanisms created using NewEphemeralGPGSigningMechanism, the returned key identity
// is expected to be one of the values returned by NewEphemeralGPGSigningMechanism,
// or the mechanism should implement signingMechanismWithVerificationIdentityLookup.
func (m *sequoiaEphemeralSigningMechanism) Verify(unverifiedSignature []byte) (contents []byte, keyIdentity string, err error) {
	return m.inner.Verify(unverifiedSignature)
}

// UntrustedSignatureContents returns UNTRUSTED contents of the signature WITHOUT ANY VERIFICATION,
// along with a short identifier of the key used for signing.
// WARNING: The short key identifier (which corresponds to "Key ID" for OpenPGP keys)
// is NOT the same as a "key identity" used in other calls to this interface, and
// the values may have no recognizable relationship if the public key is not available.
func (m *sequoiaEphemeralSigningMechanism) UntrustedSignatureContents(untrustedSignature []byte) (untrustedContents []byte, shortKeyIdentifier string, err error) {
	return gpgUntrustedSignatureContents(untrustedSignature)
}
