//go:build containers_image_openpgp

package signature

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"go.podman.io/image/v5/signature/internal"
	"go.podman.io/storage/pkg/homedir"

	// This is a fallback code; the primary recommendation is to use the gpgme mechanism
	// implementation, which is out-of-process and more appropriate for handling long-term private key material
	// than any Go implementation.
	// For this verify-only fallback, we haven't reviewed any of the
	// existing alternatives to choose; so, for now, continue to
	// use this frozen deprecated implementation.
	//lint:ignore SA1019 See above
	"golang.org/x/crypto/openpgp" //nolint:staticcheck
)

// A GPG/OpenPGP signing mechanism, implemented using x/crypto/openpgp.
type openpgpSigningMechanism struct {
	keyring openpgp.EntityList
}

// newGPGSigningMechanismInDirectory returns a new GPG/OpenPGP signing mechanism, using optionalDir if not empty.
// The caller must call .Close() on the returned SigningMechanism.
func newGPGSigningMechanismInDirectory(optionalDir string) (signingMechanismWithPassphrase, error) {
	m := &openpgpSigningMechanism{
		keyring: openpgp.EntityList{},
	}

	gpgHome := optionalDir
	if gpgHome == "" {
		gpgHome = os.Getenv("GNUPGHOME")
		if gpgHome == "" {
			gpgHome = path.Join(homedir.Get(), ".gnupg")
		}
	}

	pubring, err := os.ReadFile(path.Join(gpgHome, "pubring.gpg"))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else {
		_, err := m.importKeysFromBytes(pubring)
		if err != nil {
			return nil, err
		}
	}
	return m, nil
}

// newEphemeralGPGSigningMechanism returns a new GPG/OpenPGP signing mechanism which
// recognizes _only_ public keys from the supplied blob, and returns the identities
// of these keys.
// The caller must call .Close() on the returned SigningMechanism.
func newEphemeralGPGSigningMechanism(blobs [][]byte) (signingMechanismWithPassphrase, []string, error) {
	m := &openpgpSigningMechanism{
		keyring: openpgp.EntityList{},
	}
	keyIdentities := []string{}
	for _, blob := range blobs {
		ki, err := m.importKeysFromBytes(blob)
		if err != nil {
			return nil, nil, err
		}
		keyIdentities = append(keyIdentities, ki...)
	}

	return m, keyIdentities, nil
}

func (m *openpgpSigningMechanism) Close() error {
	return nil
}

// importKeysFromBytes imports public keys from the supplied blob and returns their identities.
// The blob is assumed to have an appropriate format (the caller is expected to know which one).
func (m *openpgpSigningMechanism) importKeysFromBytes(blob []byte) ([]string, error) {
	keyring, err := openpgp.ReadKeyRing(bytes.NewReader(blob))
	if err != nil {
		k, e2 := openpgp.ReadArmoredKeyRing(bytes.NewReader(blob))
		if e2 != nil {
			return nil, err // The original error  -- FIXME: is this better?
		}
		keyring = k
	}

	keyIdentities := []string{}
	for _, entity := range keyring {
		if entity.PrimaryKey == nil {
			// Coverage: This should never happen, openpgp.ReadEntity fails with a
			// openpgp.errors.StructuralError instead of returning an entity with this
			// field set to nil.
			continue
		}
		// Uppercase the fingerprint to be compatible with gpgme
		keyIdentities = append(keyIdentities, strings.ToUpper(fmt.Sprintf("%x", entity.PrimaryKey.Fingerprint)))
		m.keyring = append(m.keyring, entity)
	}
	return keyIdentities, nil
}

// SupportsSigning returns nil if the mechanism supports signing, or a SigningNotSupportedError.
func (m *openpgpSigningMechanism) SupportsSigning() error {
	return SigningNotSupportedError("signing is not supported in go.podman.io/image built with the containers_image_openpgp build tag")
}

// Sign creates a (non-detached) signature of input using keyIdentity.
// Fails with a SigningNotSupportedError if the mechanism does not support signing.
func (m *openpgpSigningMechanism) SignWithPassphrase(input []byte, keyIdentity string, passphrase string) ([]byte, error) {
	return nil, SigningNotSupportedError("signing is not supported in go.podman.io/image built with the containers_image_openpgp build tag")
}

// Sign creates a (non-detached) signature of input using keyIdentity.
// Fails with a SigningNotSupportedError if the mechanism does not support signing.
func (m *openpgpSigningMechanism) Sign(input []byte, keyIdentity string) ([]byte, error) {
	return m.SignWithPassphrase(input, keyIdentity, "")
}

// Verify parses unverifiedSignature and returns the content and the signer's identity.
// For mechanisms created using NewEphemeralGPGSigningMechanism, the returned key identity
// is expected to be one of the values returned by NewEphemeralGPGSigningMechanism,
// or the mechanism should implement signingMechanismWithVerificationIdentityLookup.
func (m *openpgpSigningMechanism) Verify(unverifiedSignature []byte) (contents []byte, keyIdentity string, err error) {
	md, err := openpgp.ReadMessage(bytes.NewReader(unverifiedSignature), m.keyring, nil, nil)
	if err != nil {
		return nil, "", err
	}
	if !md.IsSigned {
		return nil, "", errors.New("not signed")
	}
	content, err := io.ReadAll(md.UnverifiedBody)
	if err != nil {
		// Coverage: md.UnverifiedBody.Read only fails if the body is encrypted
		// (and possibly also signed, but it _must_ be encrypted) and the signing
		// “modification detection code” detects a mismatch. But in that case,
		// we would expect the signature verification to fail as well, and that is checked
		// first.  Besides, we are not supplying any decryption keys, so we really
		// can never reach this “encrypted data MDC mismatch” path.
		return nil, "", err
	}
	if md.SignatureError != nil {
		return nil, "", fmt.Errorf("signature error: %v", md.SignatureError)
	}
	if md.SignedBy == nil {
		return nil, "", internal.NewInvalidSignatureError(fmt.Sprintf("Key not found for key ID %x in signature", md.SignedByKeyId))
	}
	if md.Signature != nil {
		if md.Signature.SigLifetimeSecs != nil {
			expiry := md.Signature.CreationTime.Add(time.Duration(*md.Signature.SigLifetimeSecs) * time.Second)
			if time.Now().After(expiry) {
				return nil, "", internal.NewInvalidSignatureError(fmt.Sprintf("Signature expired on %s", expiry))
			}
		}
	} else if md.SignatureV3 == nil {
		// Coverage: If md.SignedBy != nil, the final md.UnverifiedBody.Read() either sets one of md.Signature or md.SignatureV3,
		// or sets md.SignatureError.
		return nil, "", internal.NewInvalidSignatureError("Unexpected openpgp.MessageDetails: neither Signature nor SignatureV3 is set")
	}

	// Uppercase the fingerprint to be compatible with gpgme
	return content, strings.ToUpper(fmt.Sprintf("%x", md.SignedBy.Entity.PrimaryKey.Fingerprint)), nil
}

// UntrustedSignatureContents returns UNTRUSTED contents of the signature WITHOUT ANY VERIFICATION,
// along with a short identifier of the key used for signing.
// WARNING: The short key identifier (which corresponds to "Key ID" for OpenPGP keys)
// is NOT the same as a "key identity" used in other calls to this interface, and
// the values may have no recognizable relationship if the public key is not available.
func (m *openpgpSigningMechanism) UntrustedSignatureContents(untrustedSignature []byte) (untrustedContents []byte, shortKeyIdentifier string, err error) {
	return gpgUntrustedSignatureContents(untrustedSignature)
}
