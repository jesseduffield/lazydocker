//go:build !windows && !linux

package timezone

// O_PATH value on freebsd. We must define O_PATH ourselves
// until https://github.com/golang/go/issues/54355 is fixed.
const O_PATH = 0x00400000 //nolint:staticcheck // ST1003: should not use ALL_CAPS
