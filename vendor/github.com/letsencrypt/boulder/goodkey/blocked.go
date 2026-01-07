package goodkey

import (
	"crypto"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"

	"github.com/letsencrypt/boulder/core"
	"github.com/letsencrypt/boulder/strictyaml"
)

// blockedKeys is a type for maintaining a map of SHA256 hashes
// of SubjectPublicKeyInfo's that should be considered blocked.
// blockedKeys are created by using loadBlockedKeysList.
type blockedKeys map[core.Sha256Digest]bool

var ErrWrongDecodedSize = errors.New("not enough bytes decoded for sha256 hash")

// blocked checks if the given public key is considered administratively
// blocked based on a SHA256 hash of the SubjectPublicKeyInfo.
// Important: blocked should not be called except on a blockedKeys instance
// returned from loadBlockedKeysList.
// function should not be used until after `loadBlockedKeysList` has returned.
func (b blockedKeys) blocked(key crypto.PublicKey) (bool, error) {
	hash, err := core.KeyDigest(key)
	if err != nil {
		// the bool result should be ignored when err is != nil but to be on the
		// paranoid side return true anyway so that a key we can't compute the
		// digest for will always be blocked even if a caller foolishly discards the
		// err result.
		return true, err
	}
	return b[hash], nil
}

// loadBlockedKeysList creates a blockedKeys object that can be used to check if
// a key is blocked. It creates a lookup map from a list of
// SHA256 hashes of SubjectPublicKeyInfo's in the input YAML file
// with the expected format:
//
//	blocked:
//	  - cuwGhNNI6nfob5aqY90e7BleU6l7rfxku4X3UTJ3Z7M=
//	  <snipped>
//	  - Qebc1V3SkX3izkYRGNJilm9Bcuvf0oox4U2Rn+b4JOE=
//
// If no hashes are found in the input YAML an error is returned.
func loadBlockedKeysList(filename string) (*blockedKeys, error) {
	yamlBytes, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var list struct {
		BlockedHashes    []string `yaml:"blocked"`
		BlockedHashesHex []string `yaml:"blockedHashesHex"`
	}
	err = strictyaml.Unmarshal(yamlBytes, &list)
	if err != nil {
		return nil, err
	}

	if len(list.BlockedHashes) == 0 && len(list.BlockedHashesHex) == 0 {
		return nil, errors.New("no blocked hashes in YAML")
	}

	blockedKeys := make(blockedKeys, len(list.BlockedHashes)+len(list.BlockedHashesHex))
	for _, b64Hash := range list.BlockedHashes {
		decoded, err := base64.StdEncoding.DecodeString(b64Hash)
		if err != nil {
			return nil, err
		}
		if len(decoded) != sha256.Size {
			return nil, ErrWrongDecodedSize
		}
		var sha256Digest core.Sha256Digest
		copy(sha256Digest[:], decoded[0:sha256.Size])
		blockedKeys[sha256Digest] = true
	}
	for _, hexHash := range list.BlockedHashesHex {
		decoded, err := hex.DecodeString(hexHash)
		if err != nil {
			return nil, err
		}
		if len(decoded) != sha256.Size {
			return nil, ErrWrongDecodedSize
		}
		var sha256Digest core.Sha256Digest
		copy(sha256Digest[:], decoded[0:sha256.Size])
		blockedKeys[sha256Digest] = true
	}
	return &blockedKeys, nil
}
