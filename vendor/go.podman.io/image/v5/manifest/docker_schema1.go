package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/docker/docker/api/types/versions"
	"github.com/opencontainers/go-digest"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/internal/manifest"
	"go.podman.io/image/v5/internal/set"
	compressiontypes "go.podman.io/image/v5/pkg/compression/types"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/regexp"
)

// Schema1FSLayers is an entry of the "fsLayers" array in docker/distribution schema 1.
type Schema1FSLayers struct {
	BlobSum digest.Digest `json:"blobSum"`
}

// Schema1History is an entry of the "history" array in docker/distribution schema 1.
type Schema1History struct {
	V1Compatibility string `json:"v1Compatibility"`
}

// Schema1 is a manifest in docker/distribution schema 1.
type Schema1 struct {
	Name                     string                   `json:"name"`
	Tag                      string                   `json:"tag"`
	Architecture             string                   `json:"architecture"`
	FSLayers                 []Schema1FSLayers        `json:"fsLayers"`
	History                  []Schema1History         `json:"history"` // Keep this in sync with ExtractedV1Compatibility!
	ExtractedV1Compatibility []Schema1V1Compatibility `json:"-"`       // Keep this in sync with History! Does not contain the full config (Schema2V1Image)
	SchemaVersion            int                      `json:"schemaVersion"`
}

type schema1V1CompatibilityContainerConfig struct {
	Cmd []string
}

// Schema1V1Compatibility is a v1Compatibility in docker/distribution schema 1.
type Schema1V1Compatibility struct {
	ID              string                                `json:"id"`
	Parent          string                                `json:"parent,omitempty"`
	Comment         string                                `json:"comment,omitempty"`
	Created         time.Time                             `json:"created"`
	ContainerConfig schema1V1CompatibilityContainerConfig `json:"container_config,omitempty"`
	Author          string                                `json:"author,omitempty"`
	ThrowAway       bool                                  `json:"throwaway,omitempty"`
}

// Schema1FromManifest creates a Schema1 manifest instance from a manifest blob.
// (NOTE: The instance is not necessary a literal representation of the original blob,
// layers with duplicate IDs are eliminated.)
func Schema1FromManifest(manifestBlob []byte) (*Schema1, error) {
	s1 := Schema1{}
	if err := json.Unmarshal(manifestBlob, &s1); err != nil {
		return nil, err
	}
	if s1.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported schema version %d", s1.SchemaVersion)
	}
	if err := manifest.ValidateUnambiguousManifestFormat(manifestBlob, DockerV2Schema1SignedMediaType,
		manifest.AllowedFieldFSLayers|manifest.AllowedFieldHistory); err != nil {
		return nil, err
	}
	if err := s1.initialize(); err != nil {
		return nil, err
	}
	if err := s1.fixManifestLayers(); err != nil {
		return nil, err
	}
	return &s1, nil
}

// Schema1FromComponents creates an Schema1 manifest instance from the supplied data.
func Schema1FromComponents(ref reference.Named, fsLayers []Schema1FSLayers, history []Schema1History, architecture string) (*Schema1, error) {
	var name, tag string
	if ref != nil { // Well, what to do if it _is_ nil? Most consumers actually don't use these fields nowadays, so we might as well try not supplying them.
		name = reference.Path(ref)
		if tagged, ok := ref.(reference.NamedTagged); ok {
			tag = tagged.Tag()
		}
	}
	s1 := Schema1{
		Name:          name,
		Tag:           tag,
		Architecture:  architecture,
		FSLayers:      fsLayers,
		History:       history,
		SchemaVersion: 1,
	}
	if err := s1.initialize(); err != nil {
		return nil, err
	}
	return &s1, nil
}

// Schema1Clone creates a copy of the supplied Schema1 manifest.
func Schema1Clone(src *Schema1) *Schema1 {
	copy := *src
	return &copy
}

// initialize initializes ExtractedV1Compatibility and verifies invariants, so that the rest of this code can assume a minimally healthy manifest.
func (m *Schema1) initialize() error {
	if len(m.FSLayers) != len(m.History) {
		return errors.New("length of history not equal to number of layers")
	}
	if len(m.FSLayers) == 0 {
		return errors.New("no FSLayers in manifest")
	}
	m.ExtractedV1Compatibility = make([]Schema1V1Compatibility, len(m.History))
	for i, h := range m.History {
		if err := json.Unmarshal([]byte(h.V1Compatibility), &m.ExtractedV1Compatibility[i]); err != nil {
			return fmt.Errorf("parsing v2s1 history entry %d: %w", i, err)
		}
	}
	return nil
}

// ConfigInfo returns a complete BlobInfo for the separate config object, or a BlobInfo{Digest:""} if there isn't a separate object.
func (m *Schema1) ConfigInfo() types.BlobInfo {
	return types.BlobInfo{}
}

// LayerInfos returns a list of LayerInfos of layers referenced by this image, in order (the root layer first, and then successive layered layers).
// The Digest field is guaranteed to be provided; Size may be -1.
// WARNING: The list may contain duplicates, and they are semantically relevant.
func (m *Schema1) LayerInfos() []LayerInfo {
	layers := make([]LayerInfo, 0, len(m.FSLayers))
	for i, layer := range slices.Backward(m.FSLayers) { // NOTE: This includes empty layers (where m.History.V1Compatibility->ThrowAway)
		layers = append(layers, LayerInfo{
			BlobInfo:   types.BlobInfo{Digest: layer.BlobSum, Size: -1},
			EmptyLayer: m.ExtractedV1Compatibility[i].ThrowAway,
		})
	}
	return layers
}

const fakeSchema1MIMEType = DockerV2Schema2LayerMediaType // Used only in schema1CompressionMIMETypeSets
var schema1CompressionMIMETypeSets = []compressionMIMETypeSet{
	{
		mtsUncompressed:                    fakeSchema1MIMEType,
		compressiontypes.GzipAlgorithmName: fakeSchema1MIMEType,
		compressiontypes.ZstdAlgorithmName: mtsUnsupportedMIMEType,
	},
}

// UpdateLayerInfos replaces the original layers with the specified BlobInfos (size+digest+urls), in order (the root layer first, and then successive layered layers)
func (m *Schema1) UpdateLayerInfos(layerInfos []types.BlobInfo) error {
	// Our LayerInfos includes empty layers (where m.ExtractedV1Compatibility[].ThrowAway), so expect them to be included here as well.
	if len(m.FSLayers) != len(layerInfos) {
		return fmt.Errorf("Error preparing updated manifest: layer count changed from %d to %d", len(m.FSLayers), len(layerInfos))
	}
	m.FSLayers = make([]Schema1FSLayers, len(layerInfos))
	for i, info := range layerInfos {
		// There are no MIME types in schema1, but we do a “conversion” here to reject unsupported compression algorithms,
		// in a way that is consistent with the other schema implementations.
		if _, err := updatedMIMEType(schema1CompressionMIMETypeSets, fakeSchema1MIMEType, info); err != nil {
			return fmt.Errorf("preparing updated manifest, layer %q: %w", info.Digest, err)
		}
		// (docker push) sets up m.ExtractedV1Compatibility[].{Id,Parent} based on values of info.Digest,
		// but (docker pull) ignores them in favor of computing DiffIDs from uncompressed data, except verifying the child->parent links and uniqueness.
		// So, we don't bother recomputing the IDs in m.History.V1Compatibility.
		m.FSLayers[(len(layerInfos)-1)-i].BlobSum = info.Digest
		if info.CryptoOperation != types.PreserveOriginalCrypto {
			return fmt.Errorf("encryption change (for layer %q) is not supported in schema1 manifests", info.Digest)
		}
	}
	return nil
}

// Serialize returns the manifest in a blob format.
// NOTE: Serialize() does not in general reproduce the original blob if this object was loaded from one, even if no modifications were made!
func (m *Schema1) Serialize() ([]byte, error) {
	// docker/distribution requires a signature even if the incoming data uses the nominally unsigned DockerV2Schema1MediaType.
	unsigned, err := json.Marshal(*m)
	if err != nil {
		return nil, err
	}
	return AddDummyV2S1Signature(unsigned)
}

// fixManifestLayers, after validating the supplied manifest
// (to use correctly-formatted IDs, and to not have non-consecutive ID collisions in m.History),
// modifies manifest to only have one entry for each layer ID in m.History (deleting the older duplicates,
// both from m.History and m.FSLayers).
// Note that even after this succeeds, m.FSLayers may contain duplicate entries
// (for Dockerfile operations which change the configuration but not the filesystem).
func (m *Schema1) fixManifestLayers() error {
	// m.initialize() has verified that len(m.FSLayers) == len(m.History)
	for _, compat := range m.ExtractedV1Compatibility {
		if err := validateV1ID(compat.ID); err != nil {
			return err
		}
	}
	if m.ExtractedV1Compatibility[len(m.ExtractedV1Compatibility)-1].Parent != "" {
		return errors.New("Invalid parent ID in the base layer of the image")
	}
	// check general duplicates to error instead of a deadlock
	idmap := set.New[string]()
	var lastID string
	for _, img := range m.ExtractedV1Compatibility {
		// skip IDs that appear after each other, we handle those later
		if img.ID != lastID && idmap.Contains(img.ID) {
			return fmt.Errorf("ID %+v appears multiple times in manifest", img.ID)
		}
		lastID = img.ID
		idmap.Add(lastID)
	}
	// backwards loop so that we keep the remaining indexes after removing items
	for i := len(m.ExtractedV1Compatibility) - 2; i >= 0; i-- {
		if m.ExtractedV1Compatibility[i].ID == m.ExtractedV1Compatibility[i+1].ID { // repeated ID. remove and continue
			m.FSLayers = slices.Delete(m.FSLayers, i, i+1)
			m.History = slices.Delete(m.History, i, i+1)
			m.ExtractedV1Compatibility = slices.Delete(m.ExtractedV1Compatibility, i, i+1)
		} else if m.ExtractedV1Compatibility[i].Parent != m.ExtractedV1Compatibility[i+1].ID {
			return fmt.Errorf("Invalid parent ID. Expected %v, got %q", m.ExtractedV1Compatibility[i+1].ID, m.ExtractedV1Compatibility[i].Parent)
		}
	}
	return nil
}

var validHex = regexp.Delayed(`^([a-f0-9]{64})$`)

func validateV1ID(id string) error {
	if ok := validHex.MatchString(id); !ok {
		return fmt.Errorf("image ID %q is invalid", id)
	}
	return nil
}

// Inspect returns various information for (skopeo inspect) parsed from the manifest and configuration.
func (m *Schema1) Inspect(_ func(types.BlobInfo) ([]byte, error)) (*types.ImageInspectInfo, error) {
	s1 := &Schema2V1Image{}
	if err := json.Unmarshal([]byte(m.History[0].V1Compatibility), s1); err != nil {
		return nil, err
	}
	layerInfos := m.LayerInfos()
	i := &types.ImageInspectInfo{
		Tag:           m.Tag,
		Created:       &s1.Created,
		DockerVersion: s1.DockerVersion,
		Architecture:  s1.Architecture,
		Variant:       s1.Variant,
		Os:            s1.OS,
		Layers:        layerInfosToStrings(layerInfos),
		LayersData:    imgInspectLayersFromLayerInfos(layerInfos),
		Author:        s1.Author,
	}
	if s1.Config != nil {
		i.Labels = s1.Config.Labels
		i.Env = s1.Config.Env
	}
	return i, nil
}

// ToSchema2Config builds a schema2-style configuration blob using the supplied diffIDs.
func (m *Schema1) ToSchema2Config(diffIDs []digest.Digest) ([]byte, error) {
	// Convert the schema 1 compat info into a schema 2 config, constructing some of the fields
	// that aren't directly comparable using info from the manifest.
	if len(m.History) == 0 {
		return nil, errors.New("image has no layers")
	}
	s1 := Schema2V1Image{}
	config := []byte(m.History[0].V1Compatibility)
	err := json.Unmarshal(config, &s1)
	if err != nil {
		return nil, fmt.Errorf("decoding configuration: %w", err)
	}
	// Images created with versions prior to 1.8.3 require us to re-encode the encoded object,
	// adding some fields that aren't "omitempty".
	if s1.DockerVersion != "" && versions.LessThan(s1.DockerVersion, "1.8.3") {
		config, err = json.Marshal(&s1)
		if err != nil {
			return nil, fmt.Errorf("re-encoding compat image config %#v: %w", s1, err)
		}
	}
	// Build the history.
	convertedHistory := []Schema2History{}
	for _, compat := range slices.Backward(m.ExtractedV1Compatibility) {
		hitem := Schema2History{
			Created:    compat.Created,
			CreatedBy:  strings.Join(compat.ContainerConfig.Cmd, " "),
			Author:     compat.Author,
			Comment:    compat.Comment,
			EmptyLayer: compat.ThrowAway,
		}
		convertedHistory = append(convertedHistory, hitem)
	}
	// Build the rootfs information.  We need the decompressed sums that we've been
	// calculating to fill in the DiffIDs.  It's expected (but not enforced by us)
	// that the number of diffIDs corresponds to the number of non-EmptyLayer
	// entries in the history.
	rootFS := &Schema2RootFS{
		Type:    "layers",
		DiffIDs: diffIDs,
	}
	// And now for some raw manipulation.
	raw := make(map[string]*json.RawMessage)
	err = json.Unmarshal(config, &raw)
	if err != nil {
		return nil, fmt.Errorf("re-decoding compat image config %#v: %w", s1, err)
	}
	// Drop some fields.
	delete(raw, "id")
	delete(raw, "parent")
	delete(raw, "parent_id")
	delete(raw, "layer_id")
	delete(raw, "throwaway")
	delete(raw, "Size")
	// Add the history and rootfs information.
	rootfs, err := json.Marshal(rootFS)
	if err != nil {
		return nil, fmt.Errorf("error encoding rootfs information %#v: %w", rootFS, err)
	}
	rawRootfs := json.RawMessage(rootfs)
	raw["rootfs"] = &rawRootfs
	history, err := json.Marshal(convertedHistory)
	if err != nil {
		return nil, fmt.Errorf("error encoding history information %#v: %w", convertedHistory, err)
	}
	rawHistory := json.RawMessage(history)
	raw["history"] = &rawHistory
	// Encode the result.
	config, err = json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("error re-encoding compat image config %#v: %w", s1, err)
	}
	return config, nil
}

// ImageID computes an ID which can uniquely identify this image by its contents.
func (m *Schema1) ImageID(diffIDs []digest.Digest) (string, error) {
	image, err := m.ToSchema2Config(diffIDs)
	if err != nil {
		return "", err
	}
	return digest.FromBytes(image).Encoded(), nil
}
