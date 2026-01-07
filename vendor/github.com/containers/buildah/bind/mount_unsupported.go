//go:build !linux

package bind

import (
	"github.com/opencontainers/runtime-spec/specs-go"
)

// SetupIntermediateMountNamespace returns a no-op unmountAll() and no error.
func SetupIntermediateMountNamespace(spec *specs.Spec, bundlePath string) (unmountAll func() error, err error) {
	stripNoBindOption(spec)
	return func() error { return nil }, nil
}
