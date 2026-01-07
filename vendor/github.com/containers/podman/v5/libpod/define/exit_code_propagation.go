package define

import "fmt"

// KubeExitCodePropagation defines an exit policy of kube workloads.
type KubeExitCodePropagation int

const (
	// Invalid exit policy for a proper type system.
	KubeExitCodePropagationInvalid KubeExitCodePropagation = iota
	// Exit 0 regardless of any failed containers.
	KubeExitCodePropagationNone
	// Exit non-zero if all containers failed.
	KubeExitCodePropagationAll
	// Exit non-zero if any container failed.
	KubeExitCodePropagationAny

	// String representations.
	strKubeECPInvalid = "invalid"
	strKubeECPNone    = "none"
	strKubeECPAll     = "all"
	strKubeECPAny     = "any"
)

// Parse the specified kube exit-code propagation. Return an error if an
// unsupported value is specified.
func ParseKubeExitCodePropagation(value string) (KubeExitCodePropagation, error) {
	switch value {
	case strKubeECPNone, "":
		return KubeExitCodePropagationNone, nil
	case strKubeECPAll:
		return KubeExitCodePropagationAll, nil
	case strKubeECPAny:
		return KubeExitCodePropagationAny, nil
	default:
		return KubeExitCodePropagationInvalid, fmt.Errorf("unsupported exit-code propagation %q", value)
	}
}

// Return the string representation of the KubeExitCodePropagation.
func (k KubeExitCodePropagation) String() string {
	switch k {
	case KubeExitCodePropagationNone:
		return strKubeECPNone
	case KubeExitCodePropagationAll:
		return strKubeECPAll
	case KubeExitCodePropagationAny:
		return strKubeECPAny
	case KubeExitCodePropagationInvalid:
		return strKubeECPInvalid
	default:
		return "unknown value"
	}
}
