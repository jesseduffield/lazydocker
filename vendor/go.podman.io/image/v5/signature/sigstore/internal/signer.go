package internal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	sigstoreSignature "github.com/sigstore/sigstore/pkg/signature"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/signature/internal"
)

type Option func(*SigstoreSigner) error

// SigstoreSigner is a signer.SignerImplementation implementation for sigstore signatures.
// It is initialized using various closures that implement Option, sadly over several subpackages, to decrease the
// dependency impact.
type SigstoreSigner struct {
	PrivateKey       sigstoreSignature.Signer // May be nil during initialization
	SigningKeyOrCert []byte                   // For possible Rekor upload; always initialized together with PrivateKey

	// Fulcio results to include
	FulcioGeneratedCertificate      []byte // Or nil
	FulcioGeneratedCertificateChain []byte // Or nil

	// Rekor state
	RekorUploader func(ctx context.Context, keyOrCertBytes []byte, signatureBytes []byte, payloadBytes []byte) ([]byte, error) // Or nil
}

// ProgressMessage returns a human-readable sentence that makes sense to write before starting to create a single signature.
func (s *SigstoreSigner) ProgressMessage() string {
	return "Signing image using a sigstore signature"
}

// SignImageManifest creates a new signature for manifest m as dockerReference.
func (s *SigstoreSigner) SignImageManifest(ctx context.Context, m []byte, dockerReference reference.Named) (signature.Signature, error) {
	if s.PrivateKey == nil {
		return nil, errors.New("internal error: nothing to sign with, should have been detected in NewSigner")
	}

	if reference.IsNameOnly(dockerReference) {
		return nil, fmt.Errorf("reference %s can’t be signed, it has neither a tag nor a digest", dockerReference.String())
	}
	manifestDigest, err := manifest.Digest(m)
	if err != nil {
		return nil, err
	}
	// sigstore/cosign completely ignores dockerReference for actual policy decisions.
	// They record the repo (but NOT THE TAG) in the value; without the tag we can’t detect version rollbacks.
	// So, just do what simple signing does, and cosign won’t mind.
	payloadData := internal.NewUntrustedSigstorePayload(manifestDigest, dockerReference.String())
	payloadBytes, err := json.Marshal(payloadData)
	if err != nil {
		return nil, err
	}

	// github.com/sigstore/cosign/internal/pkg/cosign.payloadSigner uses signatureoptions.WithContext(),
	// which seems to be not used by anything. So we don’t bother.
	signatureBytes, err := s.PrivateKey.SignMessage(bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("creating signature: %w", err)
	}
	base64Signature := base64.StdEncoding.EncodeToString(signatureBytes)
	var rekorSETBytes []byte // = nil
	if s.RekorUploader != nil {
		set, err := s.RekorUploader(ctx, s.SigningKeyOrCert, signatureBytes, payloadBytes)
		if err != nil {
			return nil, err
		}
		rekorSETBytes = set
	}

	annotations := map[string]string{
		signature.SigstoreSignatureAnnotationKey: base64Signature,
	}
	if s.FulcioGeneratedCertificate != nil {
		annotations[signature.SigstoreCertificateAnnotationKey] = string(s.FulcioGeneratedCertificate)
	}
	if s.FulcioGeneratedCertificateChain != nil {
		annotations[signature.SigstoreIntermediateCertificateChainAnnotationKey] = string(s.FulcioGeneratedCertificateChain)
	}
	if rekorSETBytes != nil {
		annotations[signature.SigstoreSETAnnotationKey] = string(rekorSETBytes)
	}
	return signature.SigstoreFromComponents(signature.SigstoreSignatureMIMEType, payloadBytes, annotations), nil
}

func (s *SigstoreSigner) Close() error {
	return nil
}
