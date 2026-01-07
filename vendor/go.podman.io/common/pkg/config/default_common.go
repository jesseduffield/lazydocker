//go:build !freebsd && !netbsd

package config

// DefaultInitPath is the default path to the container-init binary.
var DefaultInitPath = "/usr/libexec/podman/catatonit"
