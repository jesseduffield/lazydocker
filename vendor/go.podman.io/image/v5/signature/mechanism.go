// Note: Consider the API unstable until the code supports at least three different image formats or transports.

package signature

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	// This code is used only to parse the data in an explicitly-untrusted
	// code path, where cryptography is not relevant. For now, continue to
	// use this frozen deprecated implementation. When mechanism_openpgp.go
	// migrates to another implementation, this should migrate as well.
	//lint:ignore SA1019 See above
	"golang.org/x/crypto/openpgp" //nolint:staticcheck
)

// SigningMechanism abstracts a way to sign binary blobs and verify their signatures.
// Each mechanism should eventually be closed by calling Close().
type SigningMechanism interface {
	// Close removes resources associated with the mechanism, if any.
	Close() error
	// SupportsSigning returns nil if the mechanism supports signing, or a SigningNotSupportedError.
	SupportsSigning() error
	// Sign creates a (non-detached) signature of input using keyIdentity.
	// Fails with a SigningNotSupportedError if the mechanism does not support signing.
	Sign(input []byte, keyIdentity string) ([]byte, error)
	// Verify parses unverifiedSignature and returns the content and the signer's identity.
	// For mechanisms created using NewEphemeralGPGSigningMechanism, the returned key identity
	// is expected to be one of the values returned by NewEphemeralGPGSigningMechanism,
	// or the mechanism should implement signingMechanismWithVerificationIdentityLookup.
	Verify(unverifiedSignature []byte) (contents []byte, keyIdentity string, err error)
	// UntrustedSignatureContents returns UNTRUSTED contents of the signature WITHOUT ANY VERIFICATION,
	// along with a short identifier of the key used for signing.
	// WARNING: The short key identifier (which corresponds to "Key ID" for OpenPGP keys)
	// is NOT the same as a "key identity" used in other calls to this interface, and
	// the values may have no recognizable relationship if the public key is not available.
	UntrustedSignatureContents(untrustedSignature []byte) (untrustedContents []byte, shortKeyIdentifier string, err error)
}

// signingMechanismWithPassphrase is an internal extension of SigningMechanism.
type signingMechanismWithPassphrase interface {
	SigningMechanism

	// Sign creates a (non-detached) signature of input using keyIdentity and passphrase.
	// Fails with a SigningNotSupportedError if the mechanism does not support signing.
	SignWithPassphrase(input []byte, keyIdentity string, passphrase string) ([]byte, error)
}

// signingMechanismWithVerificationIdentityLookup is an internal extension of SigningMechanism.
type signingMechanismWithVerificationIdentityLookup interface {
	SigningMechanism
	// keyIdentityForVerificationKeyIdentity re-checks the key identity returned by Verify
	// if it doesn't match an identity returned by NewEphemeralGPGSigningMechanism, trying to match it.
	// (To be more specific, for mechanisms which return a subkey fingerprint from Verify,
	// this converts the subkey fingerprint into the corresponding primary key fingerprint.)
	keyIdentityForVerificationKeyIdentity(keyIdentity string) (string, error)
}

// SigningNotSupportedError is returned when trying to sign using a mechanism which does not support that.
type SigningNotSupportedError string

func (err SigningNotSupportedError) Error() string {
	return string(err)
}

// NewGPGSigningMechanism returns a new GPG/OpenPGP signing mechanism for the user’s default
// GPG configuration ($GNUPGHOME / ~/.gnupg)
// The caller must call .Close() on the returned SigningMechanism.
func NewGPGSigningMechanism() (SigningMechanism, error) {
	return newGPGSigningMechanismInDirectory("")
}

// NewEphemeralGPGSigningMechanism returns a new GPG/OpenPGP signing mechanism which
// recognizes _only_ public keys from the supplied blob, and returns the identities
// of these keys.
// The caller must call .Close() on the returned SigningMechanism.
func NewEphemeralGPGSigningMechanism(blob []byte) (SigningMechanism, []string, error) {
	return newEphemeralGPGSigningMechanism([][]byte{blob})
}

// gpgUntrustedSignatureContents returns UNTRUSTED contents of the signature WITHOUT ANY VERIFICATION,
// along with a short identifier of the key used for signing.
// WARNING: The short key identifier (which corresponds to "Key ID" for OpenPGP keys)
// is NOT the same as a "key identity" used in other calls to this interface, and
// the values may have no recognizable relationship if the public key is not available.
func gpgUntrustedSignatureContents(untrustedSignature []byte) (untrustedContents []byte, shortKeyIdentifier string, err error) {
	// This uses the Golang-native OpenPGP implementation instead of gpgme because we are not doing any cryptography.
	md, err := openpgp.ReadMessage(bytes.NewReader(untrustedSignature), openpgp.EntityList{}, nil, nil)
	if err != nil {
		return nil, "", err
	}
	if !md.IsSigned {
		return nil, "", errors.New("The input is not a signature")
	}
	content, err := io.ReadAll(md.UnverifiedBody)
	if err != nil {
		// Coverage: An error during reading the body can happen only if
		// 1) the message is encrypted, which is not our case (and we don’t give ReadMessage the key
		// to decrypt the contents anyway), or
		// 2) the message is signed AND we give ReadMessage a corresponding public key, which we don’t.
		return nil, "", err
	}

	// Uppercase the key ID for minimal consistency with the gpgme-returned fingerprints
	// (but note that key ID is a suffix of the fingerprint only for V4 keys, not V3)!
	return content, strings.ToUpper(fmt.Sprintf("%016X", md.SignedByKeyId)), nil
}
