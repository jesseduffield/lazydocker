# pkcs7

[![Go Reference](https://pkg.go.dev/badge/github.com/smallstep/pkcs7.svg)](https://pkg.go.dev/github.com/smallstep/pkcs7)
[![Build Status](https://github.com/smallstep/pkcs7/workflows/CI/badge.svg?query=branch%3Amain+event%3Apush)](https://github.com/smallstep/pkcs7/actions/workflows/ci.yml?query=branch%3Amain+event%3Apush)

pkcs7 implements parsing and creating signed and enveloped messages.

```go
package main

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

    "github.com/smallstep/pkcs7"
)

func SignAndDetach(content []byte, cert *x509.Certificate, privkey *rsa.PrivateKey) (signed []byte, err error) {
	toBeSigned, err := NewSignedData(content)
	if err != nil {
		return fmt.Errorf("Cannot initialize signed data: %w", err)
	}
	if err = toBeSigned.AddSigner(cert, privkey, SignerInfoConfig{}); err != nil {
		return fmt.Errorf("Cannot add signer: %w", err)
	}

	// Detach signature, omit if you want an embedded signature
	toBeSigned.Detach()

	signed, err = toBeSigned.Finish()
	if err != nil {
		return fmt.Errorf("Cannot finish signing data: %w", err)
	}

	// Verify the signature
	pem.Encode(os.Stdout, &pem.Block{Type: "PKCS7", Bytes: signed})
	p7, err := pkcs7.Parse(signed)
	if err != nil {
		return fmt.Errorf("Cannot parse our signed data: %w", err)
	}

	// since the signature was detached, reattach the content here
	p7.Content = content

	if bytes.Compare(content, p7.Content) != 0 {
		return fmt.Errorf("Our content was not in the parsed data:\n\tExpected: %s\n\tActual: %s", content, p7.Content)
	}
	if err = p7.Verify(); err != nil {
		return fmt.Errorf("Cannot verify our signed data: %w", err)
	}

	return signed, nil
}
```


## Credits

This is a fork of [mozilla-services/pkcs7](https://github.com/mozilla-services/pkcs7) which, itself, was a fork of [fullsailor/pkcs7](https://github.com/fullsailor/pkcs7).
