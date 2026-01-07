package transports

import (
	"fmt"

	"go.podman.io/image/v5/types"
)

// stubTransport is an implementation of types.ImageTransport which has a name, but rejects any references with “the transport $name: is not supported in this build”.
type stubTransport string

// NewStubTransport returns an implementation of types.ImageTransport which has a name, but rejects any references with “the transport $name: is not supported in this build”.
func NewStubTransport(name string) types.ImageTransport {
	return stubTransport(name)
}

// Name returns the name of the transport, which must be unique among other transports.
func (s stubTransport) Name() string {
	return string(s)
}

// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an ImageReference.
func (s stubTransport) ParseReference(reference string) (types.ImageReference, error) {
	return nil, fmt.Errorf(`The transport "%s:" is not supported in this build`, string(s))
}

// ValidatePolicyConfigurationScope checks that scope is a valid name for a signature.PolicyTransportScopes keys
// (i.e. a valid PolicyConfigurationIdentity() or PolicyConfigurationNamespaces() return value).
// It is acceptable to allow an invalid value which will never be matched, it can "only" cause user confusion.
// scope passed to this function will not be "", that value is always allowed.
func (s stubTransport) ValidatePolicyConfigurationScope(scope string) error {
	// Allowing any reference in here allows tools with some transports stubbed-out to still
	// use signature verification policies which refer to these stubbed-out transports.
	// See also the treatment of unknown transports in policyTransportScopesWithTransport.UnmarshalJSON .
	return nil
}
