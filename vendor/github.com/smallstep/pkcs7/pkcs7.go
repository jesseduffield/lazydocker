// Package pkcs7 implements parsing and generation of some PKCS#7 structures.
package pkcs7

import (
	"bytes"
	"crypto"
	"crypto/dsa"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"sort"
	"sync"

	_ "crypto/sha1" // for crypto.SHA1

	legacyx509 "github.com/smallstep/pkcs7/internal/legacy/x509"
)

// PKCS7 Represents a PKCS7 structure
type PKCS7 struct {
	Content      []byte
	Certificates []*x509.Certificate
	CRLs         []pkix.CertificateList
	Signers      []signerInfo
	raw          interface{}
}

type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

// ErrUnsupportedContentType is returned when a PKCS7 content type is not supported.
// Currently only Data (1.2.840.113549.1.7.1), Signed Data (1.2.840.113549.1.7.2),
// and Enveloped Data are supported (1.2.840.113549.1.7.3)
var ErrUnsupportedContentType = errors.New("pkcs7: cannot parse data: unimplemented content type")

type unsignedData []byte

var (
	// Signed Data OIDs
	OIDData                   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	OIDSignedData             = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	OIDEnvelopedData          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 3}
	OIDEncryptedData          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 6}
	OIDAttributeContentType   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	OIDAttributeMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	OIDAttributeSigningTime   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}

	// Digest Algorithms
	OIDDigestAlgorithmSHA1   = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	OIDDigestAlgorithmSHA256 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	OIDDigestAlgorithmSHA384 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	OIDDigestAlgorithmSHA512 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
	OIDDigestAlgorithmSHA224 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 4}

	OIDDigestAlgorithmDSA     = asn1.ObjectIdentifier{1, 2, 840, 10040, 4, 1}
	OIDDigestAlgorithmDSASHA1 = asn1.ObjectIdentifier{1, 2, 840, 10040, 4, 3}

	OIDDigestAlgorithmECDSASHA1   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 1}
	OIDDigestAlgorithmECDSASHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	OIDDigestAlgorithmECDSASHA384 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	OIDDigestAlgorithmECDSASHA512 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}

	// Signature Algorithms
	OIDEncryptionAlgorithmRSAMD5    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 4}  // see https://www.rfc-editor.org/rfc/rfc8017#appendix-A.2.4
	OIDEncryptionAlgorithmRSASHA1   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 5}  // ditto
	OIDEncryptionAlgorithmRSASHA256 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11} // ditto
	OIDEncryptionAlgorithmRSASHA384 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12} // ditto
	OIDEncryptionAlgorithmRSASHA512 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13} // ditto
	OIDEncryptionAlgorithmRSASHA224 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 14} // ditto

	OIDEncryptionAlgorithmECDSAP256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 7}
	OIDEncryptionAlgorithmECDSAP384 = asn1.ObjectIdentifier{1, 3, 132, 0, 34}
	OIDEncryptionAlgorithmECDSAP521 = asn1.ObjectIdentifier{1, 3, 132, 0, 35}

	// Asymmetric Encryption Algorithms
	OIDEncryptionAlgorithmRSA       = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1} // see https://www.rfc-editor.org/rfc/rfc8017#appendix-A.2.2
	OIDEncryptionAlgorithmRSAESOAEP = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 7} // see https://www.rfc-editor.org/rfc/rfc8017#appendix-A.2.1

	// Symmetric Encryption Algorithms
	OIDEncryptionAlgorithmDESCBC     = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 7}               // see https://www.rfc-editor.org/rfc/rfc8018.html#appendix-B.2.1
	OIDEncryptionAlgorithmDESEDE3CBC = asn1.ObjectIdentifier{1, 2, 840, 113549, 3, 7}         // see https://www.rfc-editor.org/rfc/rfc8018.html#appendix-B.2.2
	OIDEncryptionAlgorithmAES256CBC  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 42} // see https://www.rfc-editor.org/rfc/rfc3565.html#section-4.1
	OIDEncryptionAlgorithmAES128GCM  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 6}  // see https://www.rfc-editor.org/rfc/rfc5084.html#section-3.2
	OIDEncryptionAlgorithmAES128CBC  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 2}  // see https://www.rfc-editor.org/rfc/rfc8018.html#appendix-B.2.5
	OIDEncryptionAlgorithmAES256GCM  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 46} // see https://www.rfc-editor.org/rfc/rfc5084.html#section-3.2
)

func getHashForOID(oid asn1.ObjectIdentifier) (crypto.Hash, error) {
	switch {
	case oid.Equal(OIDDigestAlgorithmSHA1), oid.Equal(OIDDigestAlgorithmECDSASHA1),
		oid.Equal(OIDDigestAlgorithmDSA), oid.Equal(OIDDigestAlgorithmDSASHA1),
		oid.Equal(OIDEncryptionAlgorithmRSA):
		return crypto.SHA1, nil
	case oid.Equal(OIDDigestAlgorithmSHA256), oid.Equal(OIDDigestAlgorithmECDSASHA256):
		return crypto.SHA256, nil
	case oid.Equal(OIDDigestAlgorithmSHA384), oid.Equal(OIDDigestAlgorithmECDSASHA384):
		return crypto.SHA384, nil
	case oid.Equal(OIDDigestAlgorithmSHA512), oid.Equal(OIDDigestAlgorithmECDSASHA512):
		return crypto.SHA512, nil
	}
	return crypto.Hash(0), ErrUnsupportedAlgorithm
}

// getDigestOIDForSignatureAlgorithm takes an x509.SignatureAlgorithm
// and returns the corresponding OID digest algorithm
func getDigestOIDForSignatureAlgorithm(digestAlg x509.SignatureAlgorithm) (asn1.ObjectIdentifier, error) {
	switch digestAlg {
	case x509.SHA1WithRSA, x509.ECDSAWithSHA1:
		return OIDDigestAlgorithmSHA1, nil
	case x509.SHA256WithRSA, x509.ECDSAWithSHA256:
		return OIDDigestAlgorithmSHA256, nil
	case x509.SHA384WithRSA, x509.ECDSAWithSHA384:
		return OIDDigestAlgorithmSHA384, nil
	case x509.SHA512WithRSA, x509.ECDSAWithSHA512:
		return OIDDigestAlgorithmSHA512, nil
	}
	return nil, fmt.Errorf("pkcs7: cannot convert hash to oid, unknown hash algorithm")
}

// getOIDForEncryptionAlgorithm takes the public or private key type of the signer and
// the OID of a digest algorithm to return the appropriate signerInfo.DigestEncryptionAlgorithm
func getOIDForEncryptionAlgorithm(pkey interface{}, OIDDigestAlg asn1.ObjectIdentifier) (asn1.ObjectIdentifier, error) {
	switch k := pkey.(type) {
	case *rsa.PrivateKey, *rsa.PublicKey:
		switch {
		default:
			return OIDEncryptionAlgorithmRSA, nil
		case OIDDigestAlg.Equal(OIDEncryptionAlgorithmRSA):
			return OIDEncryptionAlgorithmRSA, nil
		case OIDDigestAlg.Equal(OIDDigestAlgorithmSHA1):
			return OIDEncryptionAlgorithmRSASHA1, nil
		case OIDDigestAlg.Equal(OIDDigestAlgorithmSHA256):
			return OIDEncryptionAlgorithmRSASHA256, nil
		case OIDDigestAlg.Equal(OIDDigestAlgorithmSHA384):
			return OIDEncryptionAlgorithmRSASHA384, nil
		case OIDDigestAlg.Equal(OIDDigestAlgorithmSHA512):
			return OIDEncryptionAlgorithmRSASHA512, nil
		}
	case *ecdsa.PrivateKey, *ecdsa.PublicKey:
		switch {
		case OIDDigestAlg.Equal(OIDDigestAlgorithmSHA1):
			return OIDDigestAlgorithmECDSASHA1, nil
		case OIDDigestAlg.Equal(OIDDigestAlgorithmSHA256):
			return OIDDigestAlgorithmECDSASHA256, nil
		case OIDDigestAlg.Equal(OIDDigestAlgorithmSHA384):
			return OIDDigestAlgorithmECDSASHA384, nil
		case OIDDigestAlg.Equal(OIDDigestAlgorithmSHA512):
			return OIDDigestAlgorithmECDSASHA512, nil
		}
	case *dsa.PrivateKey, *dsa.PublicKey:
		return OIDDigestAlgorithmDSA, nil
	case crypto.Signer:
		// This generic case is here to cover types from other packages. It
		// was specifically added to handle the private keyRSA type in the
		// github.com/go-piv/piv-go/piv package.
		return getOIDForEncryptionAlgorithm(k.Public(), OIDDigestAlg)
	}
	return nil, fmt.Errorf("pkcs7: cannot convert encryption algorithm to oid, unknown private key type %T", pkey)

}

// Parse decodes a DER encoded PKCS7 package
func Parse(data []byte) (p7 *PKCS7, err error) {
	if len(data) == 0 {
		return nil, errors.New("pkcs7: input data is empty")
	}
	var info contentInfo
	der, err := ber2der(data)
	if err != nil {
		return nil, err
	}
	rest, err := asn1.Unmarshal(der, &info)
	if len(rest) > 0 {
		err = asn1.SyntaxError{Msg: "trailing data"}
		return
	}
	if err != nil {
		return
	}

	// fmt.Printf("--> Content Type: %s", info.ContentType)
	switch {
	case info.ContentType.Equal(OIDSignedData):
		return parseSignedData(info.Content.Bytes)
	case info.ContentType.Equal(OIDEnvelopedData):
		return parseEnvelopedData(info.Content.Bytes)
	case info.ContentType.Equal(OIDEncryptedData):
		return parseEncryptedData(info.Content.Bytes)
	}
	return nil, ErrUnsupportedContentType
}

func parseEnvelopedData(data []byte) (*PKCS7, error) {
	var ed envelopedData
	if _, err := asn1.Unmarshal(data, &ed); err != nil {
		return nil, err
	}
	return &PKCS7{
		raw: ed,
	}, nil
}

func parseEncryptedData(data []byte) (*PKCS7, error) {
	var ed encryptedData
	if _, err := asn1.Unmarshal(data, &ed); err != nil {
		return nil, err
	}
	return &PKCS7{
		raw: ed,
	}, nil
}

// SetFallbackLegacyX509CertificateParserEnabled enables parsing certificates
// embedded in a PKCS7 message using the logic from crypto/x509 from before
// Go 1.23. Go 1.23 introduced a breaking change in case a certificate contains
// a critical authority key identifier, which is the correct thing to do based
// on RFC 5280, but it breaks Windows devices performing the Simple Certificate
// Enrolment Protocol (SCEP), as the certificates embedded in those requests
// apparently have authority key identifier extensions marked critical.
//
// See https://go-review.googlesource.com/c/go/+/562341 for the change in the
// Go source.
//
// When [SetFallbackLegacyX509CertificateParserEnabled] is called with true, it
// enables parsing using the legacy crypto/x509 certificate parser. It'll first
// try to parse the certificates using the regular Go crypto/x509 package, but
// if it fails on the above case, it'll retry parsing the certificates using a
// copy of the crypto/x509 package based on Go 1.23, but skips checking the
// authority key identifier extension being critical or not.
func SetFallbackLegacyX509CertificateParserEnabled(v bool) {
	legacyX509CertificateParser.Lock()
	legacyX509CertificateParser.enabled = v
	legacyX509CertificateParser.Unlock()
}

var legacyX509CertificateParser struct {
	sync.RWMutex
	enabled bool
}

func isLegacyX509ParserEnabled() bool {
	legacyX509CertificateParser.RLock()
	defer legacyX509CertificateParser.RUnlock()
	return legacyX509CertificateParser.enabled
}

func (raw rawCertificates) Parse() ([]*x509.Certificate, error) {
	if len(raw.Raw) == 0 {
		return nil, nil
	}

	var val asn1.RawValue
	if _, err := asn1.Unmarshal(raw.Raw, &val); err != nil {
		return nil, err
	}

	certificates, err := x509.ParseCertificates(val.Bytes)
	if err != nil && err.Error() == "x509: authority key identifier incorrectly marked critical" {
		if isLegacyX509ParserEnabled() {
			certificates, err = legacyx509.ParseCertificates(val.Bytes)
		}
	}

	return certificates, err
}

func isCertMatchForIssuerAndSerial(cert *x509.Certificate, ias issuerAndSerial) bool {
	return cert.SerialNumber.Cmp(ias.SerialNumber) == 0 && bytes.Equal(cert.RawIssuer, ias.IssuerName.FullBytes)
}

// Attribute represents a key value pair attribute. Value must be marshalable byte
// `encoding/asn1`
type Attribute struct {
	Type  asn1.ObjectIdentifier
	Value interface{}
}

type attributes struct {
	types  []asn1.ObjectIdentifier
	values []interface{}
}

// Add adds the attribute, maintaining insertion order
func (attrs *attributes) Add(attrType asn1.ObjectIdentifier, value interface{}) {
	attrs.types = append(attrs.types, attrType)
	attrs.values = append(attrs.values, value)
}

type sortableAttribute struct {
	SortKey   []byte
	Attribute attribute
}

type attributeSet []sortableAttribute

func (sa attributeSet) Len() int {
	return len(sa)
}

func (sa attributeSet) Less(i, j int) bool {
	return bytes.Compare(sa[i].SortKey, sa[j].SortKey) < 0
}

func (sa attributeSet) Swap(i, j int) {
	sa[i], sa[j] = sa[j], sa[i]
}

func (sa attributeSet) Attributes() []attribute {
	attrs := make([]attribute, len(sa))
	for i, attr := range sa {
		attrs[i] = attr.Attribute
	}
	return attrs
}

func (attrs *attributes) ForMarshalling() ([]attribute, error) {
	sortables := make(attributeSet, len(attrs.types))
	for i := range sortables {
		attrType := attrs.types[i]
		attrValue := attrs.values[i]
		asn1Value, err := asn1.Marshal(attrValue)
		if err != nil {
			return nil, err
		}
		attr := attribute{
			Type:  attrType,
			Value: asn1.RawValue{Tag: 17, IsCompound: true, Bytes: asn1Value}, // 17 == SET tag
		}
		encoded, err := asn1.Marshal(attr)
		if err != nil {
			return nil, err
		}
		sortables[i] = sortableAttribute{
			SortKey:   encoded,
			Attribute: attr,
		}
	}
	sort.Sort(sortables)
	return sortables.Attributes(), nil
}
