package signature

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/sigstore/fulcio/pkg/certificate"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"go.podman.io/image/v5/signature/internal"
)

// fulcioTrustRoot contains policy allow validating Fulcio-issued certificates.
// Users should call validate() on the policy before using it.
type fulcioTrustRoot struct {
	caCertificates *x509.CertPool
	oidcIssuer     string
	subjectEmail   string
}

func (f *fulcioTrustRoot) validate() error {
	if f.oidcIssuer == "" {
		return errors.New("Internal inconsistency: Fulcio use set up without OIDC issuer")
	}
	if f.subjectEmail == "" {
		return errors.New("Internal inconsistency: Fulcio use set up without subject email")
	}
	return nil
}

// fulcioIssuerInCertificate returns the OIDC issuer recorded by Fulcio in unutrustedCertificate;
// it fails if the extension is not present in the certificate, or on any inconsistency.
func fulcioIssuerInCertificate(untrustedCertificate *x509.Certificate) (string, error) {
	// == Validate the recorded OIDC issuer
	gotOIDCIssuer1 := false
	gotOIDCIssuer2 := false
	var oidcIssuer1, oidcIssuer2 string
	// certificate.ParseExtensions doesn’t reject duplicate extensions, and doesn’t detect inconsistencies
	// between certificate.OIDIssuer and certificate.OIDIssuerV2.
	// Go 1.19 rejects duplicate extensions universally; but until we can require Go 1.19,
	// reject duplicates manually.
	for _, untrustedExt := range untrustedCertificate.Extensions {
		if untrustedExt.Id.Equal(certificate.OIDIssuer) { //nolint:staticcheck // This is deprecated, but we must continue to accept it.
			if gotOIDCIssuer1 {
				// Coverage: This is unreachable in Go ≥1.19, which rejects certificates with duplicate extensions
				// already in ParseCertificate.
				return "", internal.NewInvalidSignatureError("Fulcio certificate has a duplicate OIDC issuer v1 extension")
			}
			oidcIssuer1 = string(untrustedExt.Value)
			gotOIDCIssuer1 = true
		} else if untrustedExt.Id.Equal(certificate.OIDIssuerV2) {
			if gotOIDCIssuer2 {
				// Coverage: This is unreachable in Go ≥1.19, which rejects certificates with duplicate extensions
				// already in ParseCertificate.
				return "", internal.NewInvalidSignatureError("Fulcio certificate has a duplicate OIDC issuer v2 extension")
			}
			rest, err := asn1.Unmarshal(untrustedExt.Value, &oidcIssuer2)
			if err != nil {
				return "", internal.NewInvalidSignatureError(fmt.Sprintf("invalid ASN.1 in OIDC issuer v2 extension: %v", err))
			}
			if len(rest) != 0 {
				return "", internal.NewInvalidSignatureError("invalid ASN.1 in OIDC issuer v2 extension, trailing data")
			}
			gotOIDCIssuer2 = true
		}
	}
	switch {
	case gotOIDCIssuer1 && gotOIDCIssuer2:
		if oidcIssuer1 != oidcIssuer2 {
			return "", internal.NewInvalidSignatureError(fmt.Sprintf("inconsistent OIDC issuer extension values: v1 %#v, v2 %#v",
				oidcIssuer1, oidcIssuer2))
		}
		return oidcIssuer1, nil
	case gotOIDCIssuer1:
		return oidcIssuer1, nil
	case gotOIDCIssuer2:
		return oidcIssuer2, nil
	default:
		return "", internal.NewInvalidSignatureError("Fulcio certificate is missing the issuer extension")
	}
}

func (f *fulcioTrustRoot) verifyFulcioCertificateAtTime(relevantTime time.Time, untrustedCertificateBytes []byte, untrustedIntermediateChainBytes []byte) (crypto.PublicKey, error) {
	// == Verify the certificate is correctly signed
	var untrustedIntermediatePool *x509.CertPool // = nil
	// untrustedCertificateChainPool.AppendCertsFromPEM does something broadly similar,
	// but it seems to optimize for memory usage at the cost of larger CPU usage (i.e. to load
	// the hundreds of trusted CAs). Golang’s TLS code similarly calls individual AddCert
	// for intermediate certificates.
	if len(untrustedIntermediateChainBytes) > 0 {
		untrustedIntermediateChain, err := cryptoutils.UnmarshalCertificatesFromPEM(untrustedIntermediateChainBytes)
		if err != nil {
			return nil, internal.NewInvalidSignatureError(fmt.Sprintf("loading certificate chain: %v", err))
		}
		untrustedIntermediatePool = x509.NewCertPool()
		if len(untrustedIntermediateChain) > 1 {
			for _, untrustedIntermediateCert := range untrustedIntermediateChain[:len(untrustedIntermediateChain)-1] {
				untrustedIntermediatePool.AddCert(untrustedIntermediateCert)
			}
		}
	}

	untrustedCertificate, err := parseLeafCertFromPEM(untrustedCertificateBytes)
	if err != nil {
		return nil, err
	}

	// Go rejects Subject Alternative Name that has no DNSNames, EmailAddresses, IPAddresses and URIs;
	// we match SAN ourselves, so override that.
	if len(untrustedCertificate.UnhandledCriticalExtensions) > 0 {
		var remaining []asn1.ObjectIdentifier
		for _, oid := range untrustedCertificate.UnhandledCriticalExtensions {
			if !oid.Equal(cryptoutils.SANOID) {
				remaining = append(remaining, oid)
			}
		}
		untrustedCertificate.UnhandledCriticalExtensions = remaining
	}

	if _, err := untrustedCertificate.Verify(x509.VerifyOptions{
		Intermediates: untrustedIntermediatePool,
		Roots:         f.caCertificates,
		// NOTE: Cosign uses untrustedCertificate.NotBefore here (i.e. uses _that_ time for intermediate certificate validation),
		// and validates the leaf certificate against relevantTime manually.
		// We verify the full certificate chain against relevantTime instead.
		// Assuming the certificate is fulcio-generated and very short-lived, that should make little difference.
		CurrentTime: relevantTime,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	}); err != nil {
		return nil, internal.NewInvalidSignatureError(fmt.Sprintf("veryfing leaf certificate failed: %v", err))
	}

	// Cosign verifies a SCT of the certificate (either embedded, or even, probably irrelevant, externally-supplied).
	//
	// We don’t currently do that.
	//
	// At the very least, with Fulcio we require Rekor SETs to prove Rekor contains a log of the signature, and that
	// already contains the full certificate; so a SCT of the certificate is superfluous (assuming Rekor allowed searching by
	// certificate subject, which, well…). That argument might go away if we add support for RFC 3161 timestamps instead of Rekor.
	//
	// Secondarily, assuming a trusted Fulcio server (which, to be fair, might not be the case for the public one) SCT is not clearly
	// better than the Fulcio server maintaining an audit log; a SCT can only reveal a misissuance if there is some other authoritative
	// log of approved Fulcio invocations, and it’s not clear where that would come from, especially human users manually
	// logging in using OpenID are not going to maintain a record of those actions.
	//
	// Also, the SCT does not help reveal _what_ was maliciously signed, nor does it protect against malicious signatures
	// by correctly-issued certificates.
	//
	// So, pragmatically, the ideal design seem to be to only do signatures from a trusted build system (which is, by definition,
	// the arbiter of desired vs. malicious signatures) that maintains an audit log of performed signature operations; and that seems to
	// make the SCT (and all of Rekor apart from the trusted timestamp) unnecessary.

	// == Validate the recorded OIDC issuer
	oidcIssuer, err := fulcioIssuerInCertificate(untrustedCertificate)
	if err != nil {
		return nil, err
	}
	if oidcIssuer != f.oidcIssuer {
		return nil, internal.NewInvalidSignatureError(fmt.Sprintf("Unexpected Fulcio OIDC issuer %q", oidcIssuer))
	}

	// == Validate the OIDC subject
	if !slices.Contains(untrustedCertificate.EmailAddresses, f.subjectEmail) {
		return nil, internal.NewInvalidSignatureError(fmt.Sprintf("Required email %q not found (got %q)",
			f.subjectEmail,
			untrustedCertificate.EmailAddresses))
	}
	// FIXME: Match more subject types? Cosign does:
	// - .DNSNames (can’t be issued by Fulcio)
	// - .IPAddresses (can’t be issued by Fulcio)
	// - .URIs (CAN be issued by Fulcio)
	// - OtherName values in SAN (CAN be issued by Fulcio)
	// - Various values about GitHub workflows (CAN be issued by Fulcio)
	// What does it… mean to get an OAuth2 identity for an IP address?
	// FIXME: How far into Turing-completeness for the issuer/subject do we need to get? Simultaneously accepted alternatives, for
	// issuers and/or subjects and/or combinations? Regexps? More?

	return untrustedCertificate.PublicKey, nil
}

func parseLeafCertFromPEM(untrustedCertificateBytes []byte) (*x509.Certificate, error) {
	untrustedLeafCerts, err := cryptoutils.UnmarshalCertificatesFromPEM(untrustedCertificateBytes)
	if err != nil {
		return nil, internal.NewInvalidSignatureError(fmt.Sprintf("parsing leaf certificate: %v", err))
	}
	switch len(untrustedLeafCerts) {
	case 0:
		return nil, internal.NewInvalidSignatureError("no certificate found in signature certificate data")
	case 1: // OK
		return untrustedLeafCerts[0], nil
	default:
		return nil, internal.NewInvalidSignatureError("unexpected multiple certificates present in signature certificate data")
	}
}

func verifyRekorFulcio(rekorPublicKeys []*ecdsa.PublicKey, fulcioTrustRoot *fulcioTrustRoot, untrustedRekorSET []byte,
	untrustedCertificateBytes []byte, untrustedIntermediateChainBytes []byte, untrustedBase64Signature string,
	untrustedPayloadBytes []byte) (crypto.PublicKey, error) {
	rekorSETTime, err := internal.VerifyRekorSET(rekorPublicKeys, untrustedRekorSET, untrustedCertificateBytes,
		untrustedBase64Signature, untrustedPayloadBytes)
	if err != nil {
		return nil, err
	}
	return fulcioTrustRoot.verifyFulcioCertificateAtTime(rekorSETTime, untrustedCertificateBytes, untrustedIntermediateChainBytes)
}
