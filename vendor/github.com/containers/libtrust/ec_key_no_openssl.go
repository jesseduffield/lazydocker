// +build !libtrust_openssl

package libtrust

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
)

func (k *ecPrivateKey) sign(data io.Reader, hashID crypto.Hash) (r, s *big.Int, err error) {
	hasher := k.signatureAlgorithm.HashID().New()
	_, err = io.Copy(hasher, data)
	if err != nil {
		return nil, nil, fmt.Errorf("error reading data to sign: %s", err)
	}
	hash := hasher.Sum(nil)

	return ecdsa.Sign(rand.Reader, k.PrivateKey, hash)
}
