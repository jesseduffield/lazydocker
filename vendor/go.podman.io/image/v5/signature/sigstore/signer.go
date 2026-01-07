package sigstore

import (
	"errors"
	"fmt"
	"os"

	"github.com/sigstore/sigstore/pkg/cryptoutils"
	internalSigner "go.podman.io/image/v5/internal/signer"
	"go.podman.io/image/v5/signature/signer"
	"go.podman.io/image/v5/signature/sigstore/internal"
)

type Option = internal.Option

func WithPrivateKeyFile(file string, passphrase []byte) Option {
	return func(s *internal.SigstoreSigner) error {
		if s.PrivateKey != nil {
			return fmt.Errorf("multiple private key sources specified when preparing to create sigstore signatures")
		}

		if passphrase == nil {
			return errors.New("private key passphrase not provided")
		}

		privateKeyPEM, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading private key from %s: %w", file, err)
		}
		signerVerifier, err := loadPrivateKey(privateKeyPEM, passphrase)
		if err != nil {
			return fmt.Errorf("initializing private key: %w", err)
		}
		publicKey, err := signerVerifier.PublicKey()
		if err != nil {
			return fmt.Errorf("getting public key from private key: %w", err)
		}
		publicKeyPEM, err := cryptoutils.MarshalPublicKeyToPEM(publicKey)
		if err != nil {
			return fmt.Errorf("converting public key to PEM: %w", err)
		}
		s.PrivateKey = signerVerifier
		s.SigningKeyOrCert = publicKeyPEM
		return nil
	}
}

func NewSigner(opts ...Option) (*signer.Signer, error) {
	s := internal.SigstoreSigner{}
	for _, o := range opts {
		if err := o(&s); err != nil {
			return nil, err
		}
	}
	if s.PrivateKey == nil {
		return nil, errors.New("no private key source provided (neither a private key nor Fulcio) when preparing to create sigstore signatures")
	}

	return internalSigner.NewSigner(&s), nil
}
