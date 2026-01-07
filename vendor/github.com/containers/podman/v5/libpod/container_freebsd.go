//go:build !remote

package libpod

func networkDisabled(c *Container) (bool, error) {
	if c.config.CreateNetNS {
		return false, nil
	}
	if !c.config.PostConfigureNetNS {
		return c.state.NetNS != "", nil
	}
	return false, nil
}
