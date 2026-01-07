//go:build !static

package linkmode

// Linkmode returns the linking mode (static/dynamic) for the build.
func Linkmode() string {
	return "dynamic"
}
