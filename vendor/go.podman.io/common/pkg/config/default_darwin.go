package config

func getDefaultCgroupsMode() string {
	return "enabled"
}

func getDefaultLockType() string {
	return "shm"
}

func getLibpodTmpDir() string {
	return "/run/libpod"
}

// getDefaultMachineVolumes returns default mounted volumes (possibly with env vars, which will be expanded).
func getDefaultMachineVolumes() []string {
	return []string{
		"/Users:/Users",
		"/private:/private",
		"/var/folders:/var/folders",
	}
}

func getDefaultComposeProviders() []string {
	return []string{
		"docker-compose",
		"$HOME/.docker/cli-plugins/docker-compose",
		"/opt/homebrew/bin/docker-compose",
		"/usr/local/bin/docker-compose",
		"/Applications/Docker.app/Contents/Resources/cli-plugins/docker-compose",
		"podman-compose",
	}
}
