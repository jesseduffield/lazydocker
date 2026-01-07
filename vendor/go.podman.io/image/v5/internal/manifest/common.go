package manifest

import (
	"encoding/json"
	"fmt"
)

// AllowedManifestFields is a bit mask of “essential” manifest fields that ValidateUnambiguousManifestFormat
// can expect to be present.
type AllowedManifestFields int

const (
	AllowedFieldConfig AllowedManifestFields = 1 << iota
	AllowedFieldFSLayers
	AllowedFieldHistory
	AllowedFieldLayers
	AllowedFieldManifests
	AllowedFieldFirstUnusedBit // Keep this at the end!
)

// ValidateUnambiguousManifestFormat rejects manifests (incl. multi-arch) that look like more than
// one kind we currently recognize, i.e. if they contain any of the known “essential” format fields
// other than the ones the caller specifically allows.
// expectedMIMEType is used only for diagnostics.
// NOTE: The caller should do the non-heuristic validations (e.g. check for any specified format
// identification/version, or other “magic numbers”) before calling this, to cleanly reject unambiguous
// data that just isn’t what was expected, as opposed to actually ambiguous data.
func ValidateUnambiguousManifestFormat(manifest []byte, expectedMIMEType string,
	allowed AllowedManifestFields) error {
	if allowed >= AllowedFieldFirstUnusedBit {
		return fmt.Errorf("internal error: invalid allowedManifestFields value %#v", allowed)
	}
	// Use a private type to decode, not just a map[string]any, because we want
	// to also reject case-insensitive matches (which would be used by Go when really decoding
	// the manifest).
	// (It is expected that as manifest formats are added or extended over time, more fields will be added
	// here.)
	detectedFields := struct {
		Config    any `json:"config"`
		FSLayers  any `json:"fsLayers"`
		History   any `json:"history"`
		Layers    any `json:"layers"`
		Manifests any `json:"manifests"`
	}{}
	if err := json.Unmarshal(manifest, &detectedFields); err != nil {
		// The caller was supposed to already validate version numbers, so this should not happen;
		// let’s not bother with making this error “nice”.
		return err
	}
	unexpected := []string{}
	// Sadly this isn’t easy to automate in Go, without reflection. So, copy&paste.
	if detectedFields.Config != nil && (allowed&AllowedFieldConfig) == 0 {
		unexpected = append(unexpected, "config")
	}
	if detectedFields.FSLayers != nil && (allowed&AllowedFieldFSLayers) == 0 {
		unexpected = append(unexpected, "fsLayers")
	}
	if detectedFields.History != nil && (allowed&AllowedFieldHistory) == 0 {
		unexpected = append(unexpected, "history")
	}
	if detectedFields.Layers != nil && (allowed&AllowedFieldLayers) == 0 {
		unexpected = append(unexpected, "layers")
	}
	if detectedFields.Manifests != nil && (allowed&AllowedFieldManifests) == 0 {
		unexpected = append(unexpected, "manifests")
	}
	if len(unexpected) != 0 {
		return fmt.Errorf(`rejecting ambiguous manifest, unexpected fields %#v in supposedly %s`,
			unexpected, expectedMIMEType)
	}
	return nil
}
