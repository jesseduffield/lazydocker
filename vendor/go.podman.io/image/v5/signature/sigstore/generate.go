package sigstore

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
)

// GenerateKeyPairResult is a struct to ensure the private and public parts can not be confused by the caller.
type GenerateKeyPairResult struct {
	PublicKey  []byte
	PrivateKey []byte
}

// GenerateKeyPair generates a public/private key pair usable for signing images using the sigstore format,
// and returns key representations suitable for storing in long-term files (with the private key encrypted using the provided passphrase).
// The specific key kind (e.g. algorithm, size), as well as the file format, are unspecified by this API,
// and can change with best practices over time.
func GenerateKeyPair(passphrase []byte) (*GenerateKeyPairResult, error) {
	// https://github.com/sigstore/cosign/blob/main/specs/SIGNATURE_SPEC.md#signature-schemes
	// only requires ECDSA-P256 to be supported, so that’s what we must use.
	rawKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		// Coverage: This can fail only if the randomness source fails
		return nil, err
	}
	private, public, err := marshalKeyPair(rawKey, rawKey.Public(), passphrase)
	if err != nil {
		return nil, err
	}
	return &GenerateKeyPairResult{
		PublicKey:  public,
		PrivateKey: private,
	}, nil
}
