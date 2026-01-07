package unparsedimage

import (
	"context"

	"go.podman.io/image/v5/internal/private"
	"go.podman.io/image/v5/internal/signature"
	"go.podman.io/image/v5/types"
)

// wrapped provides the private.UnparsedImage operations
// for an object that only implements types.UnparsedImage
type wrapped struct {
	types.UnparsedImage
}

// FromPublic(unparsed) returns an object that provides the private.UnparsedImage API
func FromPublic(unparsed types.UnparsedImage) private.UnparsedImage {
	if unparsed2, ok := unparsed.(private.UnparsedImage); ok {
		return unparsed2
	}
	return &wrapped{
		UnparsedImage: unparsed,
	}
}

// UntrustedSignatures is like ImageSource.GetSignaturesWithFormat, but the result is cached; it is OK to call this however often you need.
func (w *wrapped) UntrustedSignatures(ctx context.Context) ([]signature.Signature, error) {
	sigs, err := w.Signatures(ctx)
	if err != nil {
		return nil, err
	}
	res := []signature.Signature{}
	for _, sig := range sigs {
		res = append(res, signature.SimpleSigningFromBlob(sig))
	}
	return res, nil
}
