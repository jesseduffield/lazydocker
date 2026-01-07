package sigstore

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/secure-systems-lab/go-securesystemslib/encrypted"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"
)

// The following code was copied from github.com/sigstore.
// FIXME: Eliminate that duplication.

// Copyright 2021 The Sigstore Authors.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

const (
	// from sigstore/cosign/pkg/cosign.CosignPrivateKeyPemType.
	cosignPrivateKeyPemType = "ENCRYPTED COSIGN PRIVATE KEY"
	// from sigstore/cosign/pkg/cosign.SigstorePrivateKeyPemType.
	sigstorePrivateKeyPemType = "ENCRYPTED SIGSTORE PRIVATE KEY"
)

// from sigstore/cosign/pkg/cosign.loadPrivateKey
// FIXME: Do we need all of these key formats?
func loadPrivateKey(key []byte, pass []byte) (signature.SignerVerifier, error) {
	// Decrypt first
	p, _ := pem.Decode(key)
	if p == nil {
		return nil, errors.New("invalid pem block")
	}
	if p.Type != sigstorePrivateKeyPemType && p.Type != cosignPrivateKeyPemType {
		return nil, fmt.Errorf("unsupported pem type: %s", p.Type)
	}

	x509Encoded, err := encrypted.Decrypt(p.Bytes, pass)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	pk, err := x509.ParsePKCS8PrivateKey(x509Encoded)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}
	switch pk := pk.(type) {
	case *rsa.PrivateKey:
		return signature.LoadRSAPKCS1v15SignerVerifier(pk, crypto.SHA256)
	case *ecdsa.PrivateKey:
		return signature.LoadECDSASignerVerifier(pk, crypto.SHA256)
	case ed25519.PrivateKey:
		return signature.LoadED25519SignerVerifier(pk)
	default:
		return nil, errors.New("unsupported key type")
	}
}

// simplified from sigstore/cosign/pkg/cosign.marshalKeyPair
// loadPrivateKey always requires a encryption, so this always requires a passphrase.
func marshalKeyPair(privateKey crypto.PrivateKey, publicKey crypto.PublicKey, password []byte) (_privateKey []byte, _publicKey []byte, err error) {
	x509Encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("x509 encoding private key: %w", err)
	}

	encBytes, err := encrypted.Encrypt(x509Encoded, password)
	if err != nil {
		return nil, nil, err
	}

	// store in PEM format
	privBytes := pem.EncodeToMemory(&pem.Block{
		Bytes: encBytes,
		// Use the older “COSIGN” type name; as of 2023-03-30 cosign’s main branch generates “SIGSTORE” types,
		// but a version of cosign that can accept them has not yet been released.
		Type: cosignPrivateKeyPemType,
	})

	// Now do the public key
	pubBytes, err := cryptoutils.MarshalPublicKeyToPEM(publicKey)
	if err != nil {
		return nil, nil, err
	}

	return privBytes, pubBytes, nil
}
