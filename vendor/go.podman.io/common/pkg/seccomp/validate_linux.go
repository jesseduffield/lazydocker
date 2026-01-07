//go:build seccomp

package seccomp

import (
	"encoding/json"
	"fmt"
)

// ValidateProfile does a basic validation for the provided seccomp profile
// string.
func ValidateProfile(content string) error {
	profile := &Seccomp{}
	if err := json.Unmarshal([]byte(content), &profile); err != nil {
		return fmt.Errorf("decoding seccomp profile: %w", err)
	}

	spec, err := setupSeccomp(profile, nil)
	if err != nil {
		return fmt.Errorf("create seccomp spec: %w", err)
	}

	if _, err := BuildFilter(spec); err != nil {
		return fmt.Errorf("build seccomp filter: %w", err)
	}

	return nil
}
