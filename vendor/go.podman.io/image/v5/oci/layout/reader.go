package layout

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/types"
)

// This file is named reader.go for consistency with other transports’
// handling of “image containers”, but we don’t actually need a stateful reader object.

// ListResult wraps the image reference and the manifest for loading
type ListResult struct {
	Reference          types.ImageReference
	ManifestDescriptor imgspecv1.Descriptor
}

// List returns a slice of manifests included in the archive
func List(dir string) ([]ListResult, error) {
	var res []ListResult

	indexJSON, err := os.ReadFile(filepath.Join(dir, imgspecv1.ImageIndexFile))
	if err != nil {
		return nil, err
	}
	var index imgspecv1.Index
	if err := json.Unmarshal(indexJSON, &index); err != nil {
		return nil, err
	}

	for manifestIndex, md := range index.Manifests {
		refName := md.Annotations[imgspecv1.AnnotationRefName]
		index := -1
		if refName == "" {
			index = manifestIndex
		}
		ref, err := newReference(dir, refName, index)
		if err != nil {
			return nil, fmt.Errorf("error creating image reference: %w", err)
		}
		reference := ListResult{
			Reference:          ref,
			ManifestDescriptor: md,
		}
		res = append(res, reference)
	}
	return res, nil
}
