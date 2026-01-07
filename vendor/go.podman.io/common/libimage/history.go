//go:build !remote

package libimage

import (
	"context"
	"fmt"
	"time"
)

// ImageHistory contains the history information of an image.
type ImageHistory struct {
	ID        string     `json:"id"`
	Created   *time.Time `json:"created"`
	CreatedBy string     `json:"createdBy"`
	Size      int64      `json:"size"`
	Comment   string     `json:"comment"`
	Tags      []string   `json:"tags"`
}

// History computes the image history of the image including all of its parents.
func (i *Image) History(ctx context.Context) ([]ImageHistory, error) {
	ociImage, err := i.toOCI(ctx)
	if err != nil {
		return nil, err
	}

	layerTree, err := i.runtime.newFreshLayerTree()
	if err != nil {
		return nil, err
	}

	var nextNode *layerNode
	if i.TopLayer() != "" {
		layer, err := i.runtime.store.Layer(i.TopLayer())
		if err != nil {
			return nil, err
		}
		nextNode = layerTree.node(layer.ID)
	}

	// Iterate in reverse order over the history entries, and lookup the
	// corresponding image ID, size.  If it's a non-empty history entry,
	// pick the next "storage" layer by walking the layer tree.
	var allHistory []ImageHistory
	numHistories := len(ociImage.History) - 1
	usedIDs := make(map[string]bool) // prevents assigning images IDs more than once
	for x := numHistories; x >= 0; x-- {
		history := ImageHistory{
			ID:        "<missing>", // may be overridden below
			Created:   ociImage.History[x].Created,
			CreatedBy: ociImage.History[x].CreatedBy,
			Comment:   ociImage.History[x].Comment,
		}

		if nextNode != nil && len(nextNode.images) > 0 {
			id := nextNode.images[0].ID() // always use the first one
			if _, used := usedIDs[id]; !used {
				history.ID = id
				usedIDs[id] = true
			}
			for i := range nextNode.images {
				history.Tags = append(history.Tags, nextNode.images[i].Names()...)
			}
		}

		if !ociImage.History[x].EmptyLayer {
			if nextNode == nil { // If no layer's left, something's wrong.
				return nil, fmt.Errorf("no layer left for non-empty history entry: %v", history)
			}

			history.Size = nextNode.layer.UncompressedSize

			nextNode = nextNode.parent
		}

		allHistory = append(allHistory, history)
	}

	return allHistory, nil
}
