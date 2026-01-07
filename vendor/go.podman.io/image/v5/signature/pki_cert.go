package signature

import (
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"slices"

	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"go.podman.io/image/v5/signature/internal"
)

type pkiTrustRoot struct {
	caRootsCertificates        *x509.CertPool
	caIntermediateCertificates *x509.CertPool
	subjectEmail               string
	subjectHostname            string
}

func (p *pkiTrustRoot) validate() error {
	if p.subjectEmail == "" && p.subjectHostname == "" {
		return errors.New("Internal inconsistency: PKI use set up without subject email or subject hostname")
	}
	return nil
}

func verifyPKI(pkiTrustRoot *pkiTrustRoot, untrustedCertificateBytes []byte, untrustedIntermediateChainBytes []byte) (crypto.PublicKey, error) {
	var untrustedIntermediatePool *x509.CertPool
	if pkiTrustRoot.caIntermediateCertificates != nil {
		untrustedIntermediatePool = pkiTrustRoot.caIntermediateCertificates.Clone()
	} else {
		untrustedIntermediatePool = x509.NewCertPool()
	}
	if len(untrustedIntermediateChainBytes) > 0 {
		untrustedIntermediateChain, err := cryptoutils.UnmarshalCertificatesFromPEM(untrustedIntermediateChainBytes)
		if err != nil {
			return nil, internal.NewInvalidSignatureError(fmt.Sprintf("loading certificate chain: %v", err))
		}
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

	if _, err := untrustedCertificate.Verify(x509.VerifyOptions{
		Intermediates: untrustedIntermediatePool,
		Roots:         pkiTrustRoot.caRootsCertificates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	}); err != nil {
		return nil, internal.NewInvalidSignatureError(fmt.Sprintf("veryfing leaf certificate failed: %v", err))
	}

	if pkiTrustRoot.subjectEmail != "" {
		if !slices.Contains(untrustedCertificate.EmailAddresses, pkiTrustRoot.subjectEmail) {
			return nil, internal.NewInvalidSignatureError(fmt.Sprintf("Required email %q not found (got %q)",
				pkiTrustRoot.subjectEmail,
				untrustedCertificate.EmailAddresses))
		}
	}
	if pkiTrustRoot.subjectHostname != "" {
		if err = untrustedCertificate.VerifyHostname(pkiTrustRoot.subjectHostname); err != nil {
			return nil, internal.NewInvalidSignatureError(fmt.Sprintf("Unexpected subject hostname: %v", err))
		}
	}

	return untrustedCertificate.PublicKey, nil
}
