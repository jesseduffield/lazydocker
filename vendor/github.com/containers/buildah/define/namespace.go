package define

import (
	"fmt"
)

// NamespaceOption controls how we set up a namespace when launching processes.
type NamespaceOption struct {
	// Name specifies the type of namespace, typically matching one of the
	// ...Namespace constants defined in
	// github.com/opencontainers/runtime-spec/specs-go.
	Name string
	// Host is used to force our processes to use the host's namespace of
	// this type.
	Host bool
	// Path is the path of the namespace to attach our process to, if Host
	// is not set.  If Host is not set and Path is also empty, a new
	// namespace will be created for the process that we're starting.
	// If Name is specs.NetworkNamespace, if Path doesn't look like an
	// absolute path, it is treated as a comma-separated list of CNI
	// configuration names which will be selected from among all of the CNI
	// network configurations which we find.
	Path string
}

// NamespaceOptions provides some helper methods for a slice of NamespaceOption
// structs.
type NamespaceOptions []NamespaceOption

// Find the configuration for the namespace of the given type.  If there are
// duplicates, find the _last_ one of the type, since we assume it was appended
// more recently.
func (n *NamespaceOptions) Find(namespace string) *NamespaceOption {
	for i := range *n {
		j := len(*n) - 1 - i
		if (*n)[j].Name == namespace {
			return &((*n)[j])
		}
	}
	return nil
}

// AddOrReplace either adds or replaces the configuration for a given namespace.
func (n *NamespaceOptions) AddOrReplace(options ...NamespaceOption) {
nextOption:
	for _, option := range options {
		for i := range *n {
			j := len(*n) - 1 - i
			if (*n)[j].Name == option.Name {
				(*n)[j] = option
				continue nextOption
			}
		}
		*n = append(*n, option)
	}
}

// NetworkConfigurationPolicy takes the value NetworkDefault, NetworkDisabled,
// or NetworkEnabled.
type NetworkConfigurationPolicy int

const (
	// NetworkDefault is one of the values that BuilderOptions.ConfigureNetwork
	// can take, signalling that the default behavior should be used.
	NetworkDefault NetworkConfigurationPolicy = iota
	// NetworkDisabled is one of the values that BuilderOptions.ConfigureNetwork
	// can take, signalling that network interfaces should NOT be configured for
	// newly-created network namespaces.
	NetworkDisabled
	// NetworkEnabled is one of the values that BuilderOptions.ConfigureNetwork
	// can take, signalling that network interfaces should be configured for
	// newly-created network namespaces.
	NetworkEnabled
)

// String formats a NetworkConfigurationPolicy as a string.
func (p NetworkConfigurationPolicy) String() string {
	switch p {
	case NetworkDefault:
		return "NetworkDefault"
	case NetworkDisabled:
		return "NetworkDisabled"
	case NetworkEnabled:
		return "NetworkEnabled"
	}
	return fmt.Sprintf("unknown NetworkConfigurationPolicy %d", p)
}
