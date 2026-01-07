package impl

import (
	"context"

	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/types"
)

// DoesNotAffectLayerInfosForCopy implements LayerInfosForCopy() that returns nothing.
type DoesNotAffectLayerInfosForCopy struct{}

// LayerInfosForCopy returns either nil (meaning the values in the manifest are fine), or updated values for the layer
// blobsums that are listed in the image's manifest.  If values are returned, they should be used when using GetBlob()
// to read the image's layers.
// If instanceDigest is not nil, it contains a digest of the specific manifest instance to retrieve BlobInfos for
// (when the primary manifest is a manifest list); this never happens if the primary manifest is not a manifest list
// (e.g. if the source never returns manifest lists).
// The Digest field is guaranteed to be provided; Size may be -1.
// WARNING: The list may contain duplicates, and they are semantically relevant.
func (stub DoesNotAffectLayerInfosForCopy) LayerInfosForCopy(ctx context.Context, instanceDigest *digest.Digest) ([]types.BlobInfo, error) {
	return nil, nil
}
