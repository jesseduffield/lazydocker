//go:build !containers_image_storage_stub

package alltransports

import (
	// Register the storage transport
	_ "go.podman.io/image/v5/storage"
)
