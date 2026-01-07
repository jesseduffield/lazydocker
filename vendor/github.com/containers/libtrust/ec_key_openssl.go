// +build libtrust_openssl

package libtrust

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
)

func (k *ecPrivateKey) sign(data io.Reader, hashID crypto.Hash) (r, s *big.Int, err error) {
	hId := k.signatureAlgorithm.HashID()
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(data)
	if err != nil {
		return nil, nil, fmt.Errorf("error reading data: %s", err)
	}

	return ecdsa.HashSign(rand.Reader, k.PrivateKey, buf.Bytes(), hId)
}
