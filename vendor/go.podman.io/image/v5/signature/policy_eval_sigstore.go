// Policy evaluation for prSigstoreSigned.

package signature

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"

	digest "github.com/opencontainers/go-digest"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"go.podman.io/image/v5/internal/multierr"
	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/signature/internal"
)

// configBytesSources contains configuration fields which may result in one or more []byte values
type configBytesSources struct {
	inconsistencyErrorMessage string   // Error to return if more than one source is set
	path                      string   // …Path: a path to a file containing the data, or ""
	paths                     []string // …Paths: paths to files containing the data, or nil
	data                      []byte   // …Data: a single instance ofhe raw data, or nil
	datas                     [][]byte // …Datas: the raw data, or nil // codespell:ignore datas
}

// loadBytesFromConfigSources ensures at most one of the sources in src is set,
// and returns the referenced data, or nil if neither is set.
func loadBytesFromConfigSources(src configBytesSources) ([][]byte, error) {
	sources := 0
	var data [][]byte // = nil
	if src.path != "" {
		sources++
		d, err := os.ReadFile(src.path)
		if err != nil {
			return nil, err
		}
		data = [][]byte{d}
	}
	if src.paths != nil {
		sources++
		data = [][]byte{}
		for _, path := range src.paths {
			d, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			data = append(data, d)
		}
	}
	if src.data != nil {
		sources++
		data = [][]byte{src.data}
	}
	if src.datas != nil { // codespell:ignore datas
		sources++
		data = src.datas // codespell:ignore datas
	}
	if sources > 1 {
		return nil, errors.New(src.inconsistencyErrorMessage)
	}
	return data, nil
}

// prepareTrustRoot creates a fulcioTrustRoot from the input data.
// (This also prevents external implementations of this interface, ensuring that prSigstoreSignedFulcio is the only one.)
func (f *prSigstoreSignedFulcio) prepareTrustRoot() (*fulcioTrustRoot, error) {
	caCertPEMs, err := loadBytesFromConfigSources(configBytesSources{
		inconsistencyErrorMessage: `Internal inconsistency: both "caPath" and "caData" specified`,
		path:                      f.CAPath,
		data:                      f.CAData,
	})
	if err != nil {
		return nil, err
	}
	if len(caCertPEMs) != 1 {
		return nil, errors.New(`Internal inconsistency: Fulcio specified with not exactly one of "caPath" nor "caData"`)
	}
	certs := x509.NewCertPool()
	if ok := certs.AppendCertsFromPEM(caCertPEMs[0]); !ok {
		return nil, errors.New("error loading Fulcio CA certificates")
	}
	fulcio := fulcioTrustRoot{
		caCertificates: certs,
		oidcIssuer:     f.OIDCIssuer,
		subjectEmail:   f.SubjectEmail,
	}
	if err := fulcio.validate(); err != nil {
		return nil, err
	}
	return &fulcio, nil
}

// prepareTrustRoot creates a pkiTrustRoot from the input data.
// (This also prevents external implementations of this interface, ensuring that prSigstoreSignedPKI is the only one.)
func (p *prSigstoreSignedPKI) prepareTrustRoot() (*pkiTrustRoot, error) {
	caRootsCertPEMs, err := loadBytesFromConfigSources(configBytesSources{
		inconsistencyErrorMessage: `Internal inconsistency: both "caRootsPath" and "caRootsData" specified`,
		path:                      p.CARootsPath,
		data:                      p.CARootsData,
	})
	if err != nil {
		return nil, err
	}
	if len(caRootsCertPEMs) != 1 {
		return nil, errors.New(`Internal inconsistency: PKI specified with not exactly one of "caRootsPath" nor "caRootsData"`)
	}
	rootsCerts := x509.NewCertPool()
	if ok := rootsCerts.AppendCertsFromPEM(caRootsCertPEMs[0]); !ok {
		return nil, errors.New("error loading PKI CA Roots certificates")
	}
	pki := pkiTrustRoot{
		caRootsCertificates: rootsCerts,
		subjectEmail:        p.SubjectEmail,
		subjectHostname:     p.SubjectHostname,
	}
	caIntermediateCertPEMs, err := loadBytesFromConfigSources(configBytesSources{
		inconsistencyErrorMessage: `Internal inconsistency: both "caIntermediatesPath" and "caIntermediatesData" specified`,
		path:                      p.CAIntermediatesPath,
		data:                      p.CAIntermediatesData,
	})
	if err != nil {
		return nil, err
	}
	if caIntermediateCertPEMs != nil {
		if len(caIntermediateCertPEMs) != 1 {
			return nil, errors.New(`Internal inconsistency: PKI specified with invalid value from "caIntermediatesPath" or "caIntermediatesData"`)
		}
		intermediatePool := x509.NewCertPool()
		trustedIntermediates, err := cryptoutils.UnmarshalCertificatesFromPEM(caIntermediateCertPEMs[0])
		if err != nil {
			return nil, internal.NewInvalidSignatureError(fmt.Sprintf("loading trusted intermediate certificates: %v", err))
		}
		for _, trustedIntermediateCert := range trustedIntermediates {
			intermediatePool.AddCert(trustedIntermediateCert)
		}
		pki.caIntermediateCertificates = intermediatePool
	}

	if err := pki.validate(); err != nil {
		return nil, err
	}
	return &pki, nil
}

// sigstoreSignedTrustRoot contains an already parsed version of the prSigstoreSigned policy
type sigstoreSignedTrustRoot struct {
	publicKeys      []crypto.PublicKey
	fulcio          *fulcioTrustRoot
	rekorPublicKeys []*ecdsa.PublicKey
	pki             *pkiTrustRoot
}

func (pr *prSigstoreSigned) prepareTrustRoot() (*sigstoreSignedTrustRoot, error) {
	res := sigstoreSignedTrustRoot{}

	publicKeyPEMs, err := loadBytesFromConfigSources(configBytesSources{
		inconsistencyErrorMessage: `Internal inconsistency: more than one of "keyPath", "keyPaths", "keyData", "keyDatas" specified`,
		path:                      pr.KeyPath,
		paths:                     pr.KeyPaths,
		data:                      pr.KeyData,
		datas:                     pr.KeyDatas, // codespell:ignore datas
	})
	if err != nil {
		return nil, err
	}
	if publicKeyPEMs != nil {
		for index, keyData := range publicKeyPEMs {
			pk, err := cryptoutils.UnmarshalPEMToPublicKey(keyData)
			if err != nil {
				return nil, fmt.Errorf("parsing public key %d: %w", index+1, err)
			}
			res.publicKeys = append(res.publicKeys, pk)
		}
		if len(res.publicKeys) == 0 {
			return nil, errors.New(`Internal inconsistency: "keyPath", "keyPaths", "keyData" and "keyDatas" produced no public keys`)
		}
	}

	if pr.Fulcio != nil {
		f, err := pr.Fulcio.prepareTrustRoot()
		if err != nil {
			return nil, err
		}
		res.fulcio = f
	}

	rekorPublicKeyPEMs, err := loadBytesFromConfigSources(configBytesSources{
		inconsistencyErrorMessage: `Internal inconsistency: both "rekorPublicKeyPath" and "rekorPublicKeyData" specified`,
		path:                      pr.RekorPublicKeyPath,
		paths:                     pr.RekorPublicKeyPaths,
		data:                      pr.RekorPublicKeyData,
		datas:                     pr.RekorPublicKeyDatas, // codespell:ignore datas
	})
	if err != nil {
		return nil, err
	}
	if rekorPublicKeyPEMs != nil {
		for index, pem := range rekorPublicKeyPEMs {
			pk, err := cryptoutils.UnmarshalPEMToPublicKey(pem)
			if err != nil {
				return nil, fmt.Errorf("parsing Rekor public key %d: %w", index+1, err)
			}
			pkECDSA, ok := pk.(*ecdsa.PublicKey)
			if !ok {
				return nil, fmt.Errorf("Rekor public key %d is not using ECDSA", index+1)

			}
			res.rekorPublicKeys = append(res.rekorPublicKeys, pkECDSA)
		}
		if len(res.rekorPublicKeys) == 0 {
			return nil, errors.New(`Internal inconsistency: "rekorPublicKeyPath", "rekorPublicKeyPaths", "rekorPublicKeyData" and "rekorPublicKeyDatas" produced no public keys`)
		}
	}

	if pr.PKI != nil {
		p, err := pr.PKI.prepareTrustRoot()
		if err != nil {
			return nil, err
		}
		res.pki = p
	}

	return &res, nil
}

func (pr *prSigstoreSigned) isSignatureAuthorAccepted(ctx context.Context, image private.UnparsedImage, sig []byte) (signatureAcceptanceResult, *Signature, error) {
	// We don’t know of a single user of this API, and we might return unexpected values in Signature.
	// For now, just punt.
	return sarRejected, nil, errors.New("isSignatureAuthorAccepted is not implemented for sigstore")
}

func (pr *prSigstoreSigned) isSignatureAccepted(ctx context.Context, image private.UnparsedImage, sig signature.Sigstore) (signatureAcceptanceResult, error) {
	// FIXME: move this to per-context initialization
	trustRoot, err := pr.prepareTrustRoot()
	if err != nil {
		return sarRejected, err
	}

	untrustedAnnotations := sig.UntrustedAnnotations()
	untrustedBase64Signature, ok := untrustedAnnotations[signature.SigstoreSignatureAnnotationKey]
	if !ok {
		return sarRejected, fmt.Errorf("missing %s annotation", signature.SigstoreSignatureAnnotationKey)
	}
	untrustedPayload := sig.UntrustedPayload()

	keySources := 0
	if trustRoot.publicKeys != nil {
		keySources++
	}
	if trustRoot.fulcio != nil {
		keySources++
	}
	if trustRoot.pki != nil {
		keySources++
	}

	var publicKeys []crypto.PublicKey
	switch {
	case keySources > 1: // newPRSigstoreSigned rejects more than one key sources.
		return sarRejected, errors.New("Internal inconsistency: More than one of public key, Fulcio, or PKI specified")
	case keySources == 0: // newPRSigstoreSigned rejects empty key sources.
		return sarRejected, errors.New("Internal inconsistency: A public key, Fulcio, or PKI must be specified.")
	case trustRoot.publicKeys != nil:
		if trustRoot.rekorPublicKeys != nil {
			untrustedSET, ok := untrustedAnnotations[signature.SigstoreSETAnnotationKey]
			if !ok { // For user convenience; passing an empty []byte to VerifyRekorSet should work.
				return sarRejected, fmt.Errorf("missing %s annotation", signature.SigstoreSETAnnotationKey)
			}

			var rekorFailures []string
			for _, candidatePublicKey := range trustRoot.publicKeys {
				// We could use publicKeyPEM directly, but let’s re-marshal to avoid inconsistencies.
				// FIXME: We could just generate DER instead of the full PEM text
				recreatedPublicKeyPEM, err := cryptoutils.MarshalPublicKeyToPEM(candidatePublicKey)
				if err != nil {
					// Coverage: The key was loaded from a PEM format, so it’s unclear how this could fail.
					// (PEM is not essential, MarshalPublicKeyToPEM can only fail if marshaling to ASN1.DER fails.)
					return sarRejected, fmt.Errorf("re-marshaling public key to PEM: %w", err)
				}
				// We don’t care about the Rekor timestamp, just about log presence.
				_, err = internal.VerifyRekorSET(trustRoot.rekorPublicKeys, []byte(untrustedSET), recreatedPublicKeyPEM, untrustedBase64Signature, untrustedPayload)
				if err == nil {
					publicKeys = append(publicKeys, candidatePublicKey)
					break // The SET can only accept one public key entry, so if we found one, the rest either doesn’t match or is a duplicate
				}
				rekorFailures = append(rekorFailures, err.Error())
			}
			if len(publicKeys) == 0 {
				if len(rekorFailures) == 0 {
					// Coverage: We have ensured that len(trustRoot.publicKeys) != 0, when nothing succeeds, there must be at least one failure.
					return sarRejected, errors.New(`Internal inconsistency: Rekor SET did not match any key but we have no failures.`)
				}
				return sarRejected, internal.NewInvalidSignatureError(fmt.Sprintf("No public key verified against the RekorSET: %s", strings.Join(rekorFailures, ", ")))
			}
		} else {
			publicKeys = trustRoot.publicKeys
		}

	case trustRoot.fulcio != nil:
		if trustRoot.rekorPublicKeys == nil { // newPRSigstoreSigned rejects such combinations.
			return sarRejected, errors.New("Internal inconsistency: Fulcio CA specified without a Rekor public key")
		}
		untrustedSET, ok := untrustedAnnotations[signature.SigstoreSETAnnotationKey]
		if !ok { // For user convenience; passing an empty []byte to VerifyRekorSet should correctly reject it anyway.
			return sarRejected, fmt.Errorf("missing %s annotation", signature.SigstoreSETAnnotationKey)
		}
		untrustedCert, ok := untrustedAnnotations[signature.SigstoreCertificateAnnotationKey]
		if !ok { // For user convenience; passing an empty []byte to VerifyRekorSet should correctly reject it anyway.
			return sarRejected, fmt.Errorf("missing %s annotation", signature.SigstoreCertificateAnnotationKey)
		}
		var untrustedIntermediateChainBytes []byte
		if untrustedIntermediateChain, ok := untrustedAnnotations[signature.SigstoreIntermediateCertificateChainAnnotationKey]; ok {
			untrustedIntermediateChainBytes = []byte(untrustedIntermediateChain)
		}
		pk, err := verifyRekorFulcio(trustRoot.rekorPublicKeys, trustRoot.fulcio,
			[]byte(untrustedSET), []byte(untrustedCert), untrustedIntermediateChainBytes, untrustedBase64Signature, untrustedPayload)
		if err != nil {
			return sarRejected, err
		}
		publicKeys = []crypto.PublicKey{pk}

	case trustRoot.pki != nil:
		if trustRoot.rekorPublicKeys != nil { // newPRSigstoreSigned rejects such combinations.
			return sarRejected, errors.New("Internal inconsistency: PKI specified with a Rekor public key")
		}
		untrustedCert, ok := untrustedAnnotations[signature.SigstoreCertificateAnnotationKey]
		if !ok {
			return sarRejected, fmt.Errorf("missing %s annotation", signature.SigstoreCertificateAnnotationKey)
		}
		var untrustedIntermediateChainBytes []byte
		if untrustedIntermediateChain, ok := untrustedAnnotations[signature.SigstoreIntermediateCertificateChainAnnotationKey]; ok {
			untrustedIntermediateChainBytes = []byte(untrustedIntermediateChain)
		}
		pk, err := verifyPKI(trustRoot.pki, []byte(untrustedCert), untrustedIntermediateChainBytes)
		if err != nil {
			return sarRejected, err
		}
		publicKeys = []crypto.PublicKey{pk}
	}

	if len(publicKeys) == 0 {
		// Coverage: This should never happen, we ensured that trustRoot.publicKeys is non-empty if set,
		// and we have already excluded the possibility in the switch above.
		return sarRejected, fmt.Errorf("Internal inconsistency: publicKey not set before verifying sigstore payload")
	}
	signature, err := internal.VerifySigstorePayload(publicKeys, untrustedPayload, untrustedBase64Signature, internal.SigstorePayloadAcceptanceRules{
		ValidateSignedDockerReference: func(ref string) error {
			if !pr.SignedIdentity.matchesDockerReference(image, ref) {
				return PolicyRequirementError(fmt.Sprintf("Signature for identity %q is not accepted", ref))
			}
			return nil
		},
		ValidateSignedDockerManifestDigest: func(digest digest.Digest) error {
			m, _, err := image.Manifest(ctx)
			if err != nil {
				return err
			}
			digestMatches, err := manifest.MatchesDigest(m, digest)
			if err != nil {
				return err
			}
			if !digestMatches {
				return PolicyRequirementError(fmt.Sprintf("Signature for digest %s does not match", digest))
			}
			return nil
		},
	})
	if err != nil {
		return sarRejected, err
	}
	if signature == nil { // A paranoid sanity check that VerifySigstorePayload has returned consistent values
		return sarRejected, errors.New("internal error: VerifySigstorePayload succeeded but returned no data") // Coverage: This should never happen.
	}

	return sarAccepted, nil
}

func (pr *prSigstoreSigned) isRunningImageAllowed(ctx context.Context, image private.UnparsedImage) (bool, error) {
	sigs, err := image.UntrustedSignatures(ctx)
	if err != nil {
		return false, err
	}
	var rejections []error
	foundNonSigstoreSignatures := 0
	foundSigstoreNonAttachments := 0
	for _, s := range sigs {
		sigstoreSig, ok := s.(signature.Sigstore)
		if !ok {
			foundNonSigstoreSignatures++
			continue
		}
		if sigstoreSig.UntrustedMIMEType() != signature.SigstoreSignatureMIMEType {
			foundSigstoreNonAttachments++
			continue
		}

		var reason error
		switch res, err := pr.isSignatureAccepted(ctx, image, sigstoreSig); res {
		case sarAccepted:
			// One accepted signature is enough.
			return true, nil
		case sarRejected:
			reason = err
		case sarUnknown:
			// Huh?! This should not happen at all; treat it as any other invalid value.
			fallthrough
		default:
			reason = fmt.Errorf(`Internal error: Unexpected signature verification result %q`, string(res))
		}
		rejections = append(rejections, reason)
	}
	var summary error
	switch len(rejections) {
	case 0:
		if foundNonSigstoreSignatures == 0 && foundSigstoreNonAttachments == 0 {
			// A nice message for the most common case.
			summary = PolicyRequirementError("A signature was required, but no signature exists")
		} else {
			summary = PolicyRequirementError(fmt.Sprintf("A signature was required, but no signature exists (%d non-sigstore signatures, %d sigstore non-signature attachments)",
				foundNonSigstoreSignatures, foundSigstoreNonAttachments))
		}
	case 1:
		summary = rejections[0]
	default:
		summary = PolicyRequirementError(multierr.Format("None of the signatures were accepted, reasons: ", "; ", "", rejections).Error())
	}
	return false, summary
}
