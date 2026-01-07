package define

// PlatformPolicy controls the behavior of image-platform matching.
type PlatformPolicy int

const (
	// Only debug log if an image does not match the expected platform.
	PlatformPolicyDefault PlatformPolicy = iota
	// Warn if an image does not match the expected platform.
	PlatformPolicyWarn
)
