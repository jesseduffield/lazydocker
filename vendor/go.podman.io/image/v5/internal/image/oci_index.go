package image

import (
	"context"
	"fmt"

	"go.podman.io/image/v5/internal/manifest"
	"go.podman.io/image/v5/types"
)

func manifestOCI1FromImageIndex(ctx context.Context, sys *types.SystemContext, src types.ImageSource, manblob []byte) (genericManifest, error) {
	index, err := manifest.OCI1IndexFromManifest(manblob)
	if err != nil {
		return nil, fmt.Errorf("parsing OCI1 index: %w", err)
	}
	targetManifestDigest, err := index.ChooseInstance(sys)
	if err != nil {
		return nil, fmt.Errorf("choosing image instance: %w", err)
	}
	manblob, mt, err := src.GetManifest(ctx, &targetManifestDigest)
	if err != nil {
		return nil, fmt.Errorf("fetching target platform image selected from image index: %w", err)
	}

	matches, err := manifest.MatchesDigest(manblob, targetManifestDigest)
	if err != nil {
		return nil, fmt.Errorf("computing manifest digest: %w", err)
	}
	if !matches {
		return nil, fmt.Errorf("Image manifest does not match selected manifest digest %s", targetManifestDigest)
	}

	return manifestInstanceFromBlob(ctx, sys, src, manblob, mt)
}
