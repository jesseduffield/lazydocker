//go:build containers_image_docker_daemon_stub

package alltransports

import "go.podman.io/image/v5/transports"

func init() {
	transports.Register(transports.NewStubTransport("docker-daemon"))
}
