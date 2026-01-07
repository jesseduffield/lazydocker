package goodkey

// This file defines a basic method for testing if a given RSA public key is on one of
// the Debian weak key lists and is therefore considered compromised. Instead of
// directly loading the hash suffixes from the individual lists we flatten them all
// into a single JSON list using cmd/weak-key-flatten for ease of use.

import (
	"crypto/rsa"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

type truncatedHash [10]byte

type WeakRSAKeys struct {
	suffixes map[truncatedHash]struct{}
}

func LoadWeakRSASuffixes(path string) (*WeakRSAKeys, error) {
	f, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var suffixList []string
	err = json.Unmarshal(f, &suffixList)
	if err != nil {
		return nil, err
	}

	wk := &WeakRSAKeys{suffixes: make(map[truncatedHash]struct{})}
	for _, suffix := range suffixList {
		err := wk.addSuffix(suffix)
		if err != nil {
			return nil, err
		}
	}
	return wk, nil
}

func (wk *WeakRSAKeys) addSuffix(str string) error {
	var suffix truncatedHash
	decoded, err := hex.DecodeString(str)
	if err != nil {
		return err
	}
	if len(decoded) != 10 {
		return fmt.Errorf("unexpected suffix length of %d", len(decoded))
	}
	copy(suffix[:], decoded)
	wk.suffixes[suffix] = struct{}{}
	return nil
}

func (wk *WeakRSAKeys) Known(key *rsa.PublicKey) bool {
	// Hash input is in the format "Modulus={upper-case hex of modulus}\n"
	hash := sha1.Sum([]byte(fmt.Sprintf("Modulus=%X\n", key.N.Bytes())))
	var suffix truncatedHash
	copy(suffix[:], hash[10:])
	_, present := wk.suffixes[suffix]
	return present
}
