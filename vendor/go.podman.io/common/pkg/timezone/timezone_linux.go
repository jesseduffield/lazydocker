package timezone

import (
	"golang.org/x/sys/unix"
)

// O_PATH value on linux.
const O_PATH = unix.O_PATH //nolint:staticcheck // ST1003: should not use ALL_CAPS
