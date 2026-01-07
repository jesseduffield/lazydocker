package define

const (
	// Default restart policy for generated unit files.
	DefaultRestartPolicy = "on-failure"

	// EnvVariable "PODMAN_SYSTEMD_UNIT" is set in all generated systemd units and
	// is set to the unit's (unique) name.
	EnvVariable = "PODMAN_SYSTEMD_UNIT"
)

// RestartPolicies includes all valid restart policies to be used in a unit
// file.
var RestartPolicies = []string{"no", "on-success", "on-failure", "on-abnormal", "on-watchdog", "on-abort", "always"}
