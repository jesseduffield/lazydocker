package simplesigning

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.podman.io/image/v5/docker/reference"
	internalSig "go.podman.io/image/v5/internal/signature"
	internalSigner "go.podman.io/image/v5/internal/signer"
	"go.podman.io/image/v5/signature"
	"go.podman.io/image/v5/signature/signer"
)

// simpleSigner is a signer.SignerImplementation implementation for simple signing signatures.
type simpleSigner struct {
	mech           signature.SigningMechanism
	keyFingerprint string
	passphrase     string // "" if not provided.
}

type Option func(*simpleSigner) error

// WithKeyFingerprint returns an Option for NewSigner, specifying a key to sign with, using the provided GPG key fingerprint.
func WithKeyFingerprint(keyFingerprint string) Option {
	return func(s *simpleSigner) error {
		s.keyFingerprint = keyFingerprint
		return nil
	}
}

// WithPassphrase returns an Option for NewSigner, specifying a passphrase for the private key.
// If this is not specified, the system may interactively prompt using a gpg-agent / pinentry.
func WithPassphrase(passphrase string) Option {
	return func(s *simpleSigner) error {
		// The gpgme implementation can’t use passphrase with \n; reject it here for consistent behavior.
		if strings.Contains(passphrase, "\n") {
			return errors.New("invalid passphrase: must not contain a line break")
		}
		s.passphrase = passphrase
		return nil
	}
}

// NewSigner returns a signature.Signer which creates “simple signing” signatures using the user’s default
// GPG configuration ($GNUPGHOME / ~/.gnupg).
//
// The set of options must identify a key to sign with, probably using a WithKeyFingerprint.
//
// The caller must call Close() on the returned Signer.
func NewSigner(opts ...Option) (*signer.Signer, error) {
	mech, err := signature.NewGPGSigningMechanism()
	if err != nil {
		return nil, fmt.Errorf("initializing GPG: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			mech.Close()
		}
	}()
	if err := mech.SupportsSigning(); err != nil {
		return nil, fmt.Errorf("Signing not supported: %w", err)
	}

	s := simpleSigner{
		mech: mech,
	}
	for _, o := range opts {
		if err := o(&s); err != nil {
			return nil, err
		}
	}
	if s.keyFingerprint == "" {
		return nil, errors.New("no key identity provided for simple signing")
	}
	// Ideally, we should look up (and unlock?) the key at this point already, but our current SigningMechanism API does not allow that.

	succeeded = true
	return internalSigner.NewSigner(&s), nil
}

// ProgressMessage returns a human-readable sentence that makes sense to write before starting to create a single signature.
func (s *simpleSigner) ProgressMessage() string {
	return "Signing image using simple signing"
}

// SignImageManifest creates a new signature for manifest m as dockerReference.
func (s *simpleSigner) SignImageManifest(ctx context.Context, m []byte, dockerReference reference.Named) (internalSig.Signature, error) {
	if reference.IsNameOnly(dockerReference) {
		return nil, fmt.Errorf("reference %s can’t be signed, it has neither a tag nor a digest", dockerReference.String())
	}
	simpleSig, err := signature.SignDockerManifestWithOptions(m, dockerReference.String(), s.mech, s.keyFingerprint, &signature.SignOptions{
		Passphrase: s.passphrase,
	})
	if err != nil {
		return nil, err
	}
	return internalSig.SimpleSigningFromBlob(simpleSig), nil
}

func (s *simpleSigner) Close() error {
	return s.mech.Close()
}
