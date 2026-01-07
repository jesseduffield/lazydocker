//go:build linux || freebsd || netbsd || openbsd

package config

var defaultUnixComposeProviders = []string{
	"$HOME/.docker/cli-plugins/docker-compose",
	"/usr/local/lib/docker/cli-plugins/docker-compose",
	"/usr/local/libexec/docker/cli-plugins/docker-compose",
	"/usr/lib/docker/cli-plugins/docker-compose",
	"/usr/libexec/docker/cli-plugins/docker-compose",
	"docker-compose",
	"podman-compose",
}

func getDefaultComposeProviders() []string {
	return defaultUnixComposeProviders
}
