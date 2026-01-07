package pkcs7

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
)

type envelopedData struct {
	Version              int
	RecipientInfos       []recipientInfo `asn1:"set"`
	EncryptedContentInfo encryptedContentInfo
}

type encryptedData struct {
	Version              int
	EncryptedContentInfo encryptedContentInfo
}

type recipientInfo struct {
	Version                int
	IssuerAndSerialNumber  issuerAndSerial
	KeyEncryptionAlgorithm pkix.AlgorithmIdentifier
	EncryptedKey           []byte
}

type encryptedContentInfo struct {
	ContentType                asn1.ObjectIdentifier
	ContentEncryptionAlgorithm pkix.AlgorithmIdentifier
	EncryptedContent           asn1.RawValue `asn1:"tag:0,optional"`
}

const (
	// EncryptionAlgorithmDESCBC is the DES CBC encryption algorithm
	EncryptionAlgorithmDESCBC = iota

	// EncryptionAlgorithmAES128CBC is the AES 128 bits with CBC encryption algorithm
	// Avoid this algorithm unless required for interoperability; use AES GCM instead.
	EncryptionAlgorithmAES128CBC

	// EncryptionAlgorithmAES256CBC is the AES 256 bits with CBC encryption algorithm
	// Avoid this algorithm unless required for interoperability; use AES GCM instead.
	EncryptionAlgorithmAES256CBC

	// EncryptionAlgorithmAES128GCM is the AES 128 bits with GCM encryption algorithm
	EncryptionAlgorithmAES128GCM

	// EncryptionAlgorithmAES256GCM is the AES 256 bits with GCM encryption algorithm
	EncryptionAlgorithmAES256GCM
)

// ContentEncryptionAlgorithm determines the algorithm used to encrypt the
// plaintext message. Change the value of this variable to change which
// algorithm is used in the Encrypt() function.
var ContentEncryptionAlgorithm = EncryptionAlgorithmDESCBC

// ErrUnsupportedEncryptionAlgorithm is returned when attempting to encrypt
// content with an unsupported algorithm.
var ErrUnsupportedEncryptionAlgorithm = errors.New("pkcs7: cannot encrypt content: only DES-CBC, AES-CBC, and AES-GCM supported")

// KeyEncryptionAlgorithm determines the algorithm used to encrypt a
// content key. Change the value of this variable to change which
// algorithm is used in the Encrypt() function.
var KeyEncryptionAlgorithm = OIDEncryptionAlgorithmRSA

// ErrUnsupportedKeyEncryptionAlgorithm is returned when an
// unsupported key encryption algorithm OID is provided.
var ErrUnsupportedKeyEncryptionAlgorithm = errors.New("pkcs7: unsupported key encryption algorithm provided")

// KeyEncryptionHash determines the crypto.Hash algorithm to use
// when encrypting a content key. Change the value of this variable
// to change which algorithm is used in the Encrypt() function.
var KeyEncryptionHash = crypto.SHA256

// ErrUnsupportedKeyEncryptionHash is returned when an
// unsupported key encryption hash is provided.
var ErrUnsupportedKeyEncryptionHash = errors.New("pkcs7: unsupported key encryption hash provided")

// ErrPSKNotProvided is returned when attempting to encrypt
// using a PSK without actually providing the PSK.
var ErrPSKNotProvided = errors.New("pkcs7: cannot encrypt content: PSK not provided")

const nonceSize = 12

type aesGCMParameters struct {
	Nonce  []byte `asn1:"tag:4"`
	ICVLen int
}

func encryptAESGCM(content []byte, key []byte) ([]byte, *encryptedContentInfo, error) {
	var keyLen int
	var algID asn1.ObjectIdentifier
	switch ContentEncryptionAlgorithm {
	case EncryptionAlgorithmAES128GCM:
		keyLen = 16
		algID = OIDEncryptionAlgorithmAES128GCM
	case EncryptionAlgorithmAES256GCM:
		keyLen = 32
		algID = OIDEncryptionAlgorithmAES256GCM
	default:
		return nil, nil, fmt.Errorf("invalid ContentEncryptionAlgorithm in encryptAESGCM: %d", ContentEncryptionAlgorithm)
	}
	if key == nil {
		// Create AES key
		key = make([]byte, keyLen)

		_, err := rand.Read(key)
		if err != nil {
			return nil, nil, err
		}
	}

	// Create nonce
	nonce := make([]byte, nonceSize)

	_, err := rand.Read(nonce)
	if err != nil {
		return nil, nil, err
	}

	// Encrypt content
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, content, nil)

	// Prepare ASN.1 Encrypted Content Info
	paramSeq := aesGCMParameters{
		Nonce:  nonce,
		ICVLen: gcm.Overhead(),
	}

	paramBytes, err := asn1.Marshal(paramSeq)
	if err != nil {
		return nil, nil, err
	}

	eci := encryptedContentInfo{
		ContentType: OIDData,
		ContentEncryptionAlgorithm: pkix.AlgorithmIdentifier{
			Algorithm: algID,
			Parameters: asn1.RawValue{
				Tag:   asn1.TagSequence,
				Bytes: paramBytes,
			},
		},
		EncryptedContent: marshalEncryptedContent(ciphertext),
	}

	return key, &eci, nil
}

func encryptDESCBC(content []byte, key []byte) ([]byte, *encryptedContentInfo, error) {
	if key == nil {
		// Create DES key
		key = make([]byte, 8)

		_, err := rand.Read(key)
		if err != nil {
			return nil, nil, err
		}
	}

	// Create CBC IV
	iv := make([]byte, des.BlockSize)
	_, err := rand.Read(iv)
	if err != nil {
		return nil, nil, err
	}

	// Encrypt padded content
	block, err := des.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	mode := cipher.NewCBCEncrypter(block, iv)
	plaintext, err := pad(content, mode.BlockSize())
	if err != nil {
		return nil, nil, err
	}
	cyphertext := make([]byte, len(plaintext))
	mode.CryptBlocks(cyphertext, plaintext)

	// Prepare ASN.1 Encrypted Content Info
	eci := encryptedContentInfo{
		ContentType: OIDData,
		ContentEncryptionAlgorithm: pkix.AlgorithmIdentifier{
			Algorithm:  OIDEncryptionAlgorithmDESCBC,
			Parameters: asn1.RawValue{Tag: 4, Bytes: iv},
		},
		EncryptedContent: marshalEncryptedContent(cyphertext),
	}

	return key, &eci, nil
}

func encryptAESCBC(content []byte, key []byte) ([]byte, *encryptedContentInfo, error) {
	var keyLen int
	var algID asn1.ObjectIdentifier
	switch ContentEncryptionAlgorithm {
	case EncryptionAlgorithmAES128CBC:
		keyLen = 16
		algID = OIDEncryptionAlgorithmAES128CBC
	case EncryptionAlgorithmAES256CBC:
		keyLen = 32
		algID = OIDEncryptionAlgorithmAES256CBC
	default:
		return nil, nil, fmt.Errorf("invalid ContentEncryptionAlgorithm in encryptAESCBC: %d", ContentEncryptionAlgorithm)
	}

	if key == nil {
		// Create AES key
		key = make([]byte, keyLen)

		_, err := rand.Read(key)
		if err != nil {
			return nil, nil, err
		}
	}

	// Create CBC IV
	iv := make([]byte, aes.BlockSize)
	_, err := rand.Read(iv)
	if err != nil {
		return nil, nil, err
	}

	// Encrypt padded content
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	mode := cipher.NewCBCEncrypter(block, iv)
	plaintext, err := pad(content, mode.BlockSize())
	if err != nil {
		return nil, nil, err
	}
	cyphertext := make([]byte, len(plaintext))
	mode.CryptBlocks(cyphertext, plaintext)

	// Prepare ASN.1 Encrypted Content Info
	eci := encryptedContentInfo{
		ContentType: OIDData,
		ContentEncryptionAlgorithm: pkix.AlgorithmIdentifier{
			Algorithm:  algID,
			Parameters: asn1.RawValue{Tag: 4, Bytes: iv},
		},
		EncryptedContent: marshalEncryptedContent(cyphertext),
	}

	return key, &eci, nil
}

// Encrypt creates and returns an envelope data PKCS7 structure with encrypted
// recipient keys for each recipient public key.
//
// The algorithm used to perform encryption is determined by the current value
// of the global ContentEncryptionAlgorithm package variable. By default, the
// value is EncryptionAlgorithmDESCBC. To use a different algorithm, change the
// value before calling Encrypt(). For example:
//
//	ContentEncryptionAlgorithm = EncryptionAlgorithmAES256GCM
//
// TODO(fullsailor): Add support for encrypting content with other algorithms
func Encrypt(content []byte, recipients []*x509.Certificate) ([]byte, error) {
	var eci *encryptedContentInfo
	var key []byte
	var err error

	// Apply chosen symmetric encryption method
	switch ContentEncryptionAlgorithm {
	case EncryptionAlgorithmDESCBC:
		key, eci, err = encryptDESCBC(content, nil)
	case EncryptionAlgorithmAES128CBC:
		fallthrough
	case EncryptionAlgorithmAES256CBC:
		key, eci, err = encryptAESCBC(content, nil)
	case EncryptionAlgorithmAES128GCM:
		fallthrough
	case EncryptionAlgorithmAES256GCM:
		key, eci, err = encryptAESGCM(content, nil)

	default:
		return nil, ErrUnsupportedEncryptionAlgorithm
	}

	if err != nil {
		return nil, err
	}

	// Prepare each recipient's encrypted cipher key
	recipientInfos := make([]recipientInfo, len(recipients))
	for i, recipient := range recipients {
		algorithm := KeyEncryptionAlgorithm
		hash := KeyEncryptionHash
		var kea pkix.AlgorithmIdentifier
		switch {
		case algorithm.Equal(OIDEncryptionAlgorithmRSAESOAEP):
			parameters, err := getParametersForKeyEncryptionAlgorithm(algorithm, hash)
			if err != nil {
				return nil, fmt.Errorf("failed to get parameters for key encryption: %v", err)
			}
			kea = pkix.AlgorithmIdentifier{
				Algorithm:  algorithm,
				Parameters: parameters,
			}
		case algorithm.Equal(OIDEncryptionAlgorithmRSA):
			kea = pkix.AlgorithmIdentifier{
				Algorithm: algorithm,
			}
		default:
			return nil, ErrUnsupportedKeyEncryptionAlgorithm
		}
		encrypted, err := encryptKey(key, recipient, algorithm, hash)
		if err != nil {
			return nil, err
		}
		ias, err := cert2issuerAndSerial(recipient)
		if err != nil {
			return nil, err
		}
		info := recipientInfo{
			Version:                0,
			IssuerAndSerialNumber:  ias,
			KeyEncryptionAlgorithm: kea,
			EncryptedKey:           encrypted,
		}
		recipientInfos[i] = info
	}

	// Prepare envelope content
	envelope := envelopedData{
		EncryptedContentInfo: *eci,
		Version:              0,
		RecipientInfos:       recipientInfos,
	}
	innerContent, err := asn1.Marshal(envelope)
	if err != nil {
		return nil, err
	}

	// Prepare outer payload structure
	wrapper := contentInfo{
		ContentType: OIDEnvelopedData,
		Content:     asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: innerContent},
	}

	return asn1.Marshal(wrapper)
}

func getParametersForKeyEncryptionAlgorithm(algorithm asn1.ObjectIdentifier, hash crypto.Hash) (asn1.RawValue, error) {
	if !algorithm.Equal(OIDEncryptionAlgorithmRSAESOAEP) {
		return asn1.RawValue{}, nil // return empty; not used
	}

	params := rsaOAEPAlgorithmParameters{}
	switch hash {
	case crypto.SHA1:
		params.HashFunc = pkix.AlgorithmIdentifier{Algorithm: OIDDigestAlgorithmSHA1}
	case crypto.SHA224:
		params.HashFunc = pkix.AlgorithmIdentifier{Algorithm: OIDDigestAlgorithmSHA224}
	case crypto.SHA256:
		params.HashFunc = pkix.AlgorithmIdentifier{Algorithm: OIDDigestAlgorithmSHA256}
	case crypto.SHA384:
		params.HashFunc = pkix.AlgorithmIdentifier{Algorithm: OIDDigestAlgorithmSHA384}
	case crypto.SHA512:
		params.HashFunc = pkix.AlgorithmIdentifier{Algorithm: OIDDigestAlgorithmSHA512}
	default:
		return asn1.RawValue{}, ErrUnsupportedAlgorithm
	}

	b, err := asn1.Marshal(params)
	if err != nil {
		return asn1.RawValue{}, fmt.Errorf("failed marshaling key encryption parameters: %v", err)
	}

	return asn1.RawValue{
		FullBytes: b,
	}, nil
}

// EncryptUsingPSK creates and returns an encrypted data PKCS7 structure,
// encrypted using caller provided pre-shared secret.
func EncryptUsingPSK(content []byte, key []byte) ([]byte, error) {
	var eci *encryptedContentInfo
	var err error

	if key == nil {
		return nil, ErrPSKNotProvided
	}

	// Apply chosen symmetric encryption method
	switch ContentEncryptionAlgorithm {
	case EncryptionAlgorithmDESCBC:
		_, eci, err = encryptDESCBC(content, key)

	case EncryptionAlgorithmAES128GCM:
		fallthrough
	case EncryptionAlgorithmAES256GCM:
		_, eci, err = encryptAESGCM(content, key)

	default:
		return nil, ErrUnsupportedEncryptionAlgorithm
	}

	if err != nil {
		return nil, err
	}

	// Prepare encrypted-data content
	ed := encryptedData{
		Version:              0,
		EncryptedContentInfo: *eci,
	}
	innerContent, err := asn1.Marshal(ed)
	if err != nil {
		return nil, err
	}

	// Prepare outer payload structure
	wrapper := contentInfo{
		ContentType: OIDEncryptedData,
		Content:     asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: innerContent},
	}

	return asn1.Marshal(wrapper)
}

func marshalEncryptedContent(content []byte) asn1.RawValue {
	return asn1.RawValue{Bytes: content, Class: 2, IsCompound: false}
}

func encryptKey(key []byte, recipient *x509.Certificate, algorithm asn1.ObjectIdentifier, hash crypto.Hash) ([]byte, error) {
	pub, ok := recipient.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, ErrUnsupportedKeyType
	}

	switch {
	case algorithm.Equal(OIDEncryptionAlgorithmRSA):
		return rsa.EncryptPKCS1v15(rand.Reader, pub, key)
	case algorithm.Equal(OIDEncryptionAlgorithmRSAESOAEP):
		return rsa.EncryptOAEP(hash.New(), rand.Reader, pub, key, nil)
	default:
		return nil, ErrUnsupportedKeyEncryptionAlgorithm
	}
}

func pad(data []byte, blocklen int) ([]byte, error) {
	if blocklen < 1 {
		return nil, fmt.Errorf("invalid blocklen %d", blocklen)
	}
	padlen := blocklen - (len(data) % blocklen)
	if padlen == 0 {
		padlen = blocklen
	}
	pad := bytes.Repeat([]byte{byte(padlen)}, padlen)
	return append(data, pad...), nil
}
