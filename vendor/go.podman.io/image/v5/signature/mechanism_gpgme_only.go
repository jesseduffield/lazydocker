//go:build !containers_image_openpgp && !containers_image_sequoia

package signature

import (
	"os"

	"github.com/proglottis/gpgme"
)

// newEphemeralGPGSigningMechanism returns a new GPG/OpenPGP signing mechanism which
// recognizes _only_ public keys from the supplied blobs, and returns the identities
// of these keys.
// The caller must call .Close() on the returned SigningMechanism.
func newEphemeralGPGSigningMechanism(blobs [][]byte) (signingMechanismWithPassphrase, []string, error) {
	dir, err := os.MkdirTemp("", "containers-ephemeral-gpg-")
	if err != nil {
		return nil, nil, err
	}
	removeDir := true
	defer func() {
		if removeDir {
			os.RemoveAll(dir)
		}
	}()
	ctx, err := newGPGMEContext(dir)
	if err != nil {
		return nil, nil, err
	}
	keyIdentities := []string{}
	for _, blob := range blobs {
		ki, err := importKeysFromBytes(ctx, blob)
		if err != nil {
			return nil, nil, err
		}
		keyIdentities = append(keyIdentities, ki...)
	}

	mech := newGPGMESigningMechanism(ctx, dir)
	removeDir = false
	return mech, keyIdentities, nil
}

// importKeysFromBytes imports public keys from the supplied blob and returns their identities.
// The blob is assumed to have an appropriate format (the caller is expected to know which one).
// NOTE: This may modify long-term state (e.g. key storage in a directory underlying the mechanism);
// but we do not make this public, it can only be used through newEphemeralGPGSigningMechanism.
func importKeysFromBytes(ctx *gpgme.Context, blob []byte) ([]string, error) {
	inputData, err := gpgme.NewDataBytes(blob)
	if err != nil {
		return nil, err
	}
	res, err := ctx.Import(inputData)
	if err != nil {
		return nil, err
	}
	keyIdentities := []string{}
	for _, i := range res.Imports {
		if i.Result == nil {
			keyIdentities = append(keyIdentities, i.Fingerprint)
		}
	}
	return keyIdentities, nil
}
