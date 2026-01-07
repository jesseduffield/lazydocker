package copy

import (
	"context"
	"fmt"

	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/private"
	internalsig "go.podman.io/image/v5/internal/signature"
	internalSigner "go.podman.io/image/v5/internal/signer"
	"go.podman.io/image/v5/signature/sigstore"
	"go.podman.io/image/v5/signature/simplesigning"
	"go.podman.io/image/v5/transports"
)

// setupSigners initializes c.signers.
func (c *copier) setupSigners() error {
	c.signers = append(c.signers, c.options.Signers...)
	// c.signersToClose is intentionally not updated with c.options.Signers.

	// We immediately append created signers to c.signers, and we rely on c.close() to clean them up; so we don’t need
	// to clean up any created signers on failure.

	if c.options.SignBy != "" {
		opts := []simplesigning.Option{
			simplesigning.WithKeyFingerprint(c.options.SignBy),
		}
		if c.options.SignPassphrase != "" {
			opts = append(opts, simplesigning.WithPassphrase(c.options.SignPassphrase))
		}
		signer, err := simplesigning.NewSigner(opts...)
		if err != nil {
			return err
		}
		c.signers = append(c.signers, signer)
		c.signersToClose = append(c.signersToClose, signer)
	}

	if c.options.SignBySigstorePrivateKeyFile != "" {
		signer, err := sigstore.NewSigner(
			sigstore.WithPrivateKeyFile(c.options.SignBySigstorePrivateKeyFile, c.options.SignSigstorePrivateKeyPassphrase),
		)
		if err != nil {
			return err
		}
		c.signers = append(c.signers, signer)
		c.signersToClose = append(c.signersToClose, signer)
	}

	return nil
}

// sourceSignatures returns signatures from unparsedSource,
// and verifies that they can be used (to avoid copying a large image when we
// can tell in advance that it would ultimately fail)
func (c *copier) sourceSignatures(ctx context.Context, unparsed private.UnparsedImage,
	gettingSignaturesMessage, checkingDestMessage string) ([]internalsig.Signature, error) {
	var sigs []internalsig.Signature
	if c.options.RemoveSignatures {
		sigs = []internalsig.Signature{}
	} else {
		c.Printf("%s\n", gettingSignaturesMessage)
		s, err := unparsed.UntrustedSignatures(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading signatures: %w", err)
		}
		sigs = s
	}
	if len(sigs) != 0 {
		c.Printf("%s\n", checkingDestMessage)
		if err := c.dest.SupportsSignatures(ctx); err != nil {
			return nil, fmt.Errorf("Can not copy signatures to %s: %w", transports.ImageName(c.dest.Reference()), err)
		}
	}
	return sigs, nil
}

// createSignatures creates signatures for manifest and an optional identity.
func (c *copier) createSignatures(ctx context.Context, manifest []byte, identity reference.Named) ([]internalsig.Signature, error) {
	if len(c.signers) == 0 {
		// We must exit early here, otherwise copies with no Docker reference wouldn’t be possible.
		return nil, nil
	}

	if identity != nil {
		if reference.IsNameOnly(identity) {
			return nil, fmt.Errorf("Sign identity must be a fully specified reference %s", identity.String())
		}
	} else {
		identity = c.dest.Reference().DockerReference()
		if identity == nil {
			return nil, fmt.Errorf("Cannot determine canonical Docker reference for destination %s", transports.ImageName(c.dest.Reference()))
		}
	}

	res := make([]internalsig.Signature, 0, len(c.signers))
	for signerIndex, signer := range c.signers {
		msg := internalSigner.ProgressMessage(signer)
		if len(c.signers) == 1 {
			c.Printf("Creating signature: %s\n", msg)
		} else {
			c.Printf("Creating signature %d: %s\n", signerIndex+1, msg)
		}
		newSig, err := internalSigner.SignImageManifest(ctx, signer, manifest, identity)
		if err != nil {
			if len(c.signers) == 1 {
				return nil, fmt.Errorf("creating signature: %w", err)
			} else {
				return nil, fmt.Errorf("creating signature %d: %w", signerIndex+1, err)
			}
		}
		res = append(res, newSig)
	}
	return res, nil
}
