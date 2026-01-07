package config

import "fmt"

// PodExitPolicies includes the supported pod exit policies.
var PodExitPolicies = []string{string(PodExitPolicyContinue), string(PodExitPolicyStop)}

// PodExitPolicy determines a pod's exit and stop behaviour.
type PodExitPolicy string

const (
	// PodExitPolicyContinue instructs the pod to continue running when the
	// last container has exited.
	PodExitPolicyContinue PodExitPolicy = "continue"
	// PodExitPolicyStop instructs the pod to stop when the last container
	// has exited.
	PodExitPolicyStop = "stop"
	// PodExitPolicyUnsupported implies an internal error.
	// Negative for backwards compat.
	PodExitPolicyUnsupported = "invalid"

	defaultPodExitPolicy = PodExitPolicyContinue
)

// ParsePodExitPolicy parses the specified policy and returns an error if it is
// invalid.
func ParsePodExitPolicy(policy string) (PodExitPolicy, error) {
	switch policy {
	case "", string(PodExitPolicyContinue):
		return PodExitPolicyContinue, nil
	case string(PodExitPolicyStop):
		return PodExitPolicyStop, nil
	default:
		return PodExitPolicyUnsupported, fmt.Errorf("invalid pod exit policy: %q", policy)
	}
}
