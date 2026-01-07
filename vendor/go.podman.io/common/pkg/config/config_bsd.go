//go:build freebsd || netbsd || openbsd

package config

const (
	// overrideContainersConfig holds the default config path overridden by the root user.
	overrideContainersConfig = "/usr/local/etc/" + _configPath

	// defaultContainersConfig holds the default containers config path.
	defaultContainersConfig = "/usr/local/share/" + _configPath

	// DefaultSignaturePolicyPath is the default value for the
	// policy.json file.
	DefaultSignaturePolicyPath = "/usr/local/etc/containers/policy.json"
)

var defaultHelperBinariesDir = []string{
	"/usr/local/bin",
	"/usr/local/libexec/podman",
	"/usr/local/lib/podman",
	"/usr/local/libexec/podman",
	"/usr/local/lib/podman",
}
