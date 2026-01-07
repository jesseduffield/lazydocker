//go:build !exclude_graphdriver_overlay && linux

package register

import (
	// register the overlay graphdriver
	_ "go.podman.io/storage/drivers/overlay"
)
