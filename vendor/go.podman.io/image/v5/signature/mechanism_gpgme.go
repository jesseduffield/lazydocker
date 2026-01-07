//go:build !containers_image_openpgp

package signature

// This is shared by mechanism_gpgme_only.go and mechanism_sequoia.go; in both situations
// newGPGSigningMechanismInDirectory is implemented using GPGME.

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/proglottis/gpgme"
	"go.podman.io/image/v5/signature/internal"
)

// A GPG/OpenPGP signing mechanism, implemented using gpgme.
type gpgmeSigningMechanism struct {
	ctx          *gpgme.Context
	ephemeralDir string // If not "", a directory to be removed on Close()
}

// newGPGMESigningMechanism returns a new GPG/OpenPGP signing mechanism for ctx.
// The caller must call .Close() on the returned SigningMechanism; if ephemeralDir is set,
// the .Close() call will remove its contents.
func newGPGMESigningMechanism(ctx *gpgme.Context, ephemeralDir string) signingMechanismWithPassphrase {
	return &gpgmeSigningMechanism{
		ctx:          ctx,
		ephemeralDir: ephemeralDir,
	}
}

// newGPGSigningMechanismInDirectory returns a new GPG/OpenPGP signing mechanism, using optionalDir if not empty.
// The caller must call .Close() on the returned SigningMechanism.
func newGPGSigningMechanismInDirectory(optionalDir string) (signingMechanismWithPassphrase, error) {
	ctx, err := newGPGMEContext(optionalDir)
	if err != nil {
		return nil, err
	}
	return newGPGMESigningMechanism(ctx, ""), nil
}

// newGPGMEContext returns a new *gpgme.Context, using optionalDir if not empty.
func newGPGMEContext(optionalDir string) (*gpgme.Context, error) {
	ctx, err := gpgme.New()
	if err != nil {
		return nil, err
	}
	if err = ctx.SetProtocol(gpgme.ProtocolOpenPGP); err != nil {
		return nil, err
	}
	if optionalDir != "" {
		err := ctx.SetEngineInfo(gpgme.ProtocolOpenPGP, "", optionalDir)
		if err != nil {
			return nil, err
		}
	}
	ctx.SetArmor(false)
	ctx.SetTextMode(false)
	return ctx, nil
}

func (m *gpgmeSigningMechanism) Close() error {
	if m.ephemeralDir != "" {
		os.RemoveAll(m.ephemeralDir) // Ignore an error, if any
	}
	return nil
}

// SupportsSigning returns nil if the mechanism supports signing, or a SigningNotSupportedError.
func (m *gpgmeSigningMechanism) SupportsSigning() error {
	return nil
}

// Sign creates a (non-detached) signature of input using keyIdentity and passphrase.
// Fails with a SigningNotSupportedError if the mechanism does not support signing.
func (m *gpgmeSigningMechanism) SignWithPassphrase(input []byte, keyIdentity string, passphrase string) ([]byte, error) {
	key, err := m.ctx.GetKey(keyIdentity, true)
	if err != nil {
		return nil, err
	}
	inputData, err := gpgme.NewDataBytes(input)
	if err != nil {
		return nil, err
	}
	var sigBuffer bytes.Buffer
	sigData, err := gpgme.NewDataWriter(&sigBuffer)
	if err != nil {
		return nil, err
	}

	if passphrase != "" {
		// Callback to write the passphrase to the specified file descriptor.
		callback := func(uidHint string, prevWasBad bool, gpgmeFD *os.File) error {
			if prevWasBad {
				return errors.New("bad passphrase")
			}
			_, err := gpgmeFD.WriteString(passphrase + "\n")
			return err
		}
		if err := m.ctx.SetCallback(callback); err != nil {
			return nil, fmt.Errorf("setting gpgme passphrase callback: %w", err)
		}

		// Loopback mode will use the callback instead of prompting the user.
		if err := m.ctx.SetPinEntryMode(gpgme.PinEntryLoopback); err != nil {
			return nil, fmt.Errorf("setting gpgme pinentry mode: %w", err)
		}
	}

	if err = m.ctx.Sign([]*gpgme.Key{key}, inputData, sigData, gpgme.SigModeNormal); err != nil {
		return nil, err
	}
	return sigBuffer.Bytes(), nil
}

// Sign creates a (non-detached) signature of input using keyIdentity.
// Fails with a SigningNotSupportedError if the mechanism does not support signing.
func (m *gpgmeSigningMechanism) Sign(input []byte, keyIdentity string) ([]byte, error) {
	return m.SignWithPassphrase(input, keyIdentity, "")
}

// Verify parses unverifiedSignature and returns the content and the signer's identity.
// For mechanisms created using NewEphemeralGPGSigningMechanism, the returned key identity
// is expected to be one of the values returned by NewEphemeralGPGSigningMechanism,
// or the mechanism should implement signingMechanismWithVerificationIdentityLookup.
func (m *gpgmeSigningMechanism) Verify(unverifiedSignature []byte) (contents []byte, keyIdentity string, err error) {
	signedBuffer := bytes.Buffer{}
	signedData, err := gpgme.NewDataWriter(&signedBuffer)
	if err != nil {
		return nil, "", err
	}
	unverifiedSignatureData, err := gpgme.NewDataBytes(unverifiedSignature)
	if err != nil {
		return nil, "", err
	}
	_, sigs, err := m.ctx.Verify(unverifiedSignatureData, nil, signedData)
	if err != nil {
		return nil, "", err
	}
	if len(sigs) != 1 {
		return nil, "", internal.NewInvalidSignatureError(fmt.Sprintf("Unexpected GPG signature count %d", len(sigs)))
	}
	sig := sigs[0]
	// This is sig.Summary == gpgme.SigSumValid except for key trust, which we handle ourselves
	if sig.Status != nil || sig.Validity == gpgme.ValidityNever || sig.ValidityReason != nil || sig.WrongKeyUsage {
		// FIXME: Better error reporting eventually
		return nil, "", internal.NewInvalidSignatureError(fmt.Sprintf("Invalid GPG signature: %#v", sig))
	}
	return signedBuffer.Bytes(), sig.Fingerprint, nil
}

// keyIdentityForVerificationKeyIdentity re-checks the key identity returned by Verify
// if it doesn't match an identity returned by NewEphemeralGPGSigningMechanism, trying to match it.
// (To be more specific, for mechanisms which return a subkey fingerprint from Verify,
// this converts the subkey fingerprint into the corresponding primary key fingerprint.)
func (m *gpgmeSigningMechanism) keyIdentityForVerificationKeyIdentity(keyIdentity string) (string, error) {
	// In theory, if keyIdentity refers to a subkey, the same subkey could be attached to different primary keys;
	// in that case, GetKey fails with “ambiguous name”.
	// We _could_ handle that, by using KeyList* (GetKey is internally just a helper for KeyList*), but sharing
	// a subkey that way is very unexpected, so, for now, prefer the much simpler implementation.
	key, err := m.ctx.GetKey(keyIdentity, false)
	if err != nil {
		return "", err
	}
	// In theory this value could be nil if (gpg --list-keys --with-colons) misses a "pub:" line
	// or a "fpr:" line, but gpg (in recent enough versions) prints that unconditionally. // codespell:ignore fpr
	return key.Fingerprint(), nil
}

// UntrustedSignatureContents returns UNTRUSTED contents of the signature WITHOUT ANY VERIFICATION,
// along with a short identifier of the key used for signing.
// WARNING: The short key identifier (which corresponds to "Key ID" for OpenPGP keys)
// is NOT the same as a "key identity" used in other calls to this interface, and
// the values may have no recognizable relationship if the public key is not available.
func (m *gpgmeSigningMechanism) UntrustedSignatureContents(untrustedSignature []byte) (untrustedContents []byte, shortKeyIdentifier string, err error) {
	return gpgUntrustedSignatureContents(untrustedSignature)
}
