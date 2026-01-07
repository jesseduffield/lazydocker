//go:build !remote

package store

import (
	"context"
	"encoding/json"

	specV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/types"
)

// unparsedArtifactImage is an interface based on the UnParsedImage and
// is used only for the commit of the manifest.
type unparsedArtifactImage struct {
	ir        types.ImageReference
	mannyfest specV1.Manifest
}

func (u unparsedArtifactImage) Reference() types.ImageReference {
	return u.ir
}

func (u unparsedArtifactImage) Manifest(_ context.Context) ([]byte, string, error) {
	b, err := json.Marshal(u.mannyfest)
	if err != nil {
		return nil, "", err
	}
	return b, specV1.MediaTypeImageIndex, nil
}

func (u unparsedArtifactImage) Signatures(_ context.Context) ([][]byte, error) {
	return [][]byte{}, nil
}

func newUnparsedArtifactImage(ir types.ImageReference, mannyfest specV1.Manifest) unparsedArtifactImage {
	return unparsedArtifactImage{
		ir:        ir,
		mannyfest: mannyfest,
	}
}
