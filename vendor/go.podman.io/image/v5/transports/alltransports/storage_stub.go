//go:build containers_image_storage_stub

package alltransports

import "go.podman.io/image/v5/transports"

func init() {
	transports.Register(transports.NewStubTransport("containers-storage"))
}
