// The libpod library is not stable and we do not support use cases outside of
// this repository. The API can change at any time even with patch releases.
//
// If you need a stable interface Podman provides a HTTP API which follows semver,
// please see https://docs.podman.io/en/latest/markdown/podman-system-service.1.html
// to start the api service and https://docs.podman.io/en/latest/_static/api.html
// for the API reference.
//
// We also provide stable go bindings to talk to the api service from another go
// program, see the pkg/bindings directory.
package libpod
