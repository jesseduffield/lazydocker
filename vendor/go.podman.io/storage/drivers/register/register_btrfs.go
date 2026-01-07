//go:build !exclude_graphdriver_btrfs && linux

package register

import (
	// register the btrfs graphdriver
	_ "go.podman.io/storage/drivers/btrfs"
)
