package mkcw

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/containers/luksy"
)

// CheckLUKSPassphrase checks that the specified LUKS-encrypted file can be
// decrypted using the specified passphrase.
func CheckLUKSPassphrase(path, decryptionPassphrase string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	v1header, v2headerA, v2headerB, v2json, err := luksy.ReadHeaders(f, luksy.ReadHeaderOptions{})
	if err != nil {
		return err
	}
	if v1header != nil {
		_, _, _, _, err = v1header.Decrypt(decryptionPassphrase, f)
		return err
	}
	if v2headerA == nil && v2headerB == nil {
		return fmt.Errorf("no LUKS headers read from %q", path)
	}
	if v2headerA != nil {
		if _, _, _, _, err = v2headerA.Decrypt(decryptionPassphrase, f, *v2json); err != nil {
			return err
		}
	}
	if v2headerB != nil {
		if _, _, _, _, err = v2headerB.Decrypt(decryptionPassphrase, f, *v2json); err != nil {
			return err
		}
	}
	return nil
}

// GenerateDiskEncryptionPassphrase generates a random disk encryption password
func GenerateDiskEncryptionPassphrase() (string, error) {
	randomizedBytes := make([]byte, 32)
	if _, err := rand.Read(randomizedBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomizedBytes), nil
}
