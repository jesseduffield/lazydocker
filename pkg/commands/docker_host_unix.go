//go:build !windows

package commands

const (
	defaultDockerHost = "unix:///var/run/docker.sock"
)
