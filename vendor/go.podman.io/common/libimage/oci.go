//go:build !remote

package libimage

import (
	"context"

	ociv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// toOCI returns the image as OCI v1 image.
func (i *Image) toOCI(ctx context.Context) (*ociv1.Image, error) {
	if i.cached.ociv1Image != nil {
		return i.cached.ociv1Image, nil
	}
	ref, err := i.StorageReference()
	if err != nil {
		return nil, err
	}

	img, err := ref.NewImage(ctx, i.runtime.systemContextCopy())
	if err != nil {
		return nil, err
	}
	defer img.Close()

	return img.OCIConfig(ctx)
}

// historiesMatch returns the number of entries in the histories which have the
// same contents.
func historiesMatch(a, b []ociv1.History) int {
	i := 0
	for i < len(a) && i < len(b) {
		if a[i].Created != nil && b[i].Created == nil {
			return i
		}
		if a[i].Created == nil && b[i].Created != nil {
			return i
		}
		if a[i].Created != nil && b[i].Created != nil {
			if !a[i].Created.Equal(*(b[i].Created)) {
				return i
			}
		}
		if a[i].CreatedBy != b[i].CreatedBy {
			return i
		}
		if a[i].Author != b[i].Author {
			return i
		}
		if a[i].Comment != b[i].Comment {
			return i
		}
		if a[i].EmptyLayer != b[i].EmptyLayer {
			return i
		}
		i++
	}
	return i
}

// areParentAndChild checks diff ID and history in the two images and return
// true if the second should be considered to be directly based on the first.
func areParentAndChild(parent, child *ociv1.Image) bool {
	// the child and candidate parent should share all of the
	// candidate parent's diff IDs, which together would have
	// controlled which layers were used

	// Both, child and parent, may be nil when the storage is left in an
	// incoherent state.  Issue #7444 describes such a case when a build
	// has been killed.
	if child == nil || parent == nil {
		return false
	}

	if len(parent.RootFS.DiffIDs) > len(child.RootFS.DiffIDs) {
		return false
	}
	childUsesCandidateDiffs := true
	for i := range parent.RootFS.DiffIDs {
		if child.RootFS.DiffIDs[i] != parent.RootFS.DiffIDs[i] {
			childUsesCandidateDiffs = false
			break
		}
	}
	if !childUsesCandidateDiffs {
		return false
	}
	// the child should have the same history as the parent, plus
	// one more entry
	if len(parent.History)+1 != len(child.History) {
		return false
	}
	if historiesMatch(parent.History, child.History) != len(parent.History) {
		return false
	}
	return true
}
