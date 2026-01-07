//go:build !remote

package libpod

import (
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func networkDisabled(c *Container) (bool, error) {
	if c.config.CreateNetNS {
		return false, nil
	}
	if !c.config.PostConfigureNetNS {
		for _, ns := range c.config.Spec.Linux.Namespaces {
			if ns.Type == spec.NetworkNamespace {
				return ns.Path == "", nil
			}
		}
	}
	return false, nil
}
