package etchosts

import (
	"fmt"

	securejoin "github.com/cyphar/filepath-securejoin"
	"go.podman.io/common/pkg/config"
)

// GetBaseHostFile return the hosts file which should be used as base.
// The first param should be the config value config.Containers.BaseHostsFile
// The second param should be the root path to the mounted image. This is
// required when the user conf value is set to "image".
func GetBaseHostFile(confValue, imageRoot string) (string, error) {
	switch confValue {
	case "":
		return config.DefaultHostsFile, nil
	case "none":
		return "", nil
	case "image":
		// use secure join to prevent problems with symlinks
		path, err := securejoin.SecureJoin(imageRoot, config.DefaultHostsFile)
		if err != nil {
			return "", fmt.Errorf("failed to get /etc/hosts path in image: %w", err)
		}
		return path, nil
	default:
		return confValue, nil
	}
}
