package define

import (
	"fmt"
)

// PullPolicy takes the value PullIfMissing, PullAlways, PullIfNewer, or PullNever.
// N.B.: the enumeration values for this type differ from those used by
// github.com/containers/common/pkg/config.PullPolicy (their zero values
// indicate different policies), so they are not interchangeable.
type PullPolicy int

const (
	// PullIfMissing is one of the values that BuilderOptions.PullPolicy
	// can take, signalling that the source image should be pulled from a
	// registry if a local copy of it is not already present.
	PullIfMissing PullPolicy = iota
	// PullAlways is one of the values that BuilderOptions.PullPolicy can
	// take, signalling that a fresh, possibly updated, copy of the image
	// should be pulled from a registry before the build proceeds.
	PullAlways
	// PullIfNewer is one of the values that BuilderOptions.PullPolicy
	// can take, signalling that the source image should only be pulled
	// from a registry if a local copy is not already present or if a
	// newer version the image is present on the repository.
	PullIfNewer
	// PullNever is one of the values that BuilderOptions.PullPolicy can
	// take, signalling that the source image should not be pulled from a
	// registry.
	PullNever
)

// String converts a PullPolicy into a string.
func (p PullPolicy) String() string {
	switch p {
	case PullIfMissing:
		return "missing"
	case PullAlways:
		return "always"
	case PullIfNewer:
		return "ifnewer"
	case PullNever:
		return "never"
	}
	return fmt.Sprintf("unrecognized policy %d", p)
}

var PolicyMap = map[string]PullPolicy{
	"missing": PullIfMissing,
	"always":  PullAlways,
	"never":   PullNever,
	"ifnewer": PullIfNewer,
}
