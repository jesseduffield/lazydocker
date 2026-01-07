//go:build !remote

package libpod

import (
	"fmt"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"go.podman.io/image/v5/docker"
	"go.podman.io/image/v5/pkg/shortnames"
	"go.podman.io/image/v5/transports/alltransports"
)

// Validate that the configuration of a container is valid.
func (c *Container) validate() error {
	imageIDSet := c.config.RootfsImageID != ""
	imageNameSet := c.config.RootfsImageName != ""
	rootfsSet := c.config.Rootfs != ""

	// If one of RootfsImageIDor RootfsImageName are set, both must be set.
	if (imageIDSet || imageNameSet) && (!imageIDSet || !imageNameSet) {
		return fmt.Errorf("both RootfsImageName and RootfsImageID must be set if either is set: %w", define.ErrInvalidArg)
	}

	// Cannot set RootfsImageID and Rootfs at the same time
	if imageIDSet && rootfsSet {
		return fmt.Errorf("cannot set both an image ID and rootfs for a container: %w", define.ErrInvalidArg)
	}

	// Must set at least one of RootfsImageID or Rootfs
	if !imageIDSet && !rootfsSet {
		return fmt.Errorf("must set root filesystem source to either image or rootfs: %w", define.ErrInvalidArg)
	}

	// A container cannot be marked as an infra and service container at
	// the same time.
	if c.IsInfra() && c.IsService() {
		return fmt.Errorf("cannot be infra and service container at the same time: %w", define.ErrInvalidArg)
	}

	// Cannot make a network namespace if we are joining another container's
	// network namespace
	if c.config.CreateNetNS && c.config.NetNsCtr != "" {
		return fmt.Errorf("cannot both create a network namespace and join another container's network namespace: %w", define.ErrInvalidArg)
	}

	if c.config.CgroupsMode == cgroupSplit && c.config.CgroupParent != "" {
		return fmt.Errorf("cannot specify --cgroup-mode=split with a cgroup-parent: %w", define.ErrInvalidArg)
	}

	// Not creating cgroups has a number of requirements, mostly related to
	// the PID namespace.
	if c.config.NoCgroups || c.config.CgroupsMode == "disabled" {
		if c.config.PIDNsCtr != "" {
			return fmt.Errorf("cannot join another container's PID namespace if not creating cgroups: %w", define.ErrInvalidArg)
		}

		if c.config.CgroupParent != "" {
			return fmt.Errorf("cannot set cgroup parent if not creating cgroups: %w", define.ErrInvalidArg)
		}

		// Ensure we have a PID namespace
		if c.config.Spec.Linux == nil {
			return fmt.Errorf("must provide Linux namespace configuration in OCI spec when using NoCgroups: %w", define.ErrInvalidArg)
		}
		foundPid := false
		for _, ns := range c.config.Spec.Linux.Namespaces {
			if ns.Type == spec.PIDNamespace {
				foundPid = true
				if ns.Path != "" {
					return fmt.Errorf("containers not creating Cgroups must create a private PID namespace - cannot use another: %w", define.ErrInvalidArg)
				}
				break
			}
		}
		if !foundPid {
			return fmt.Errorf("containers not creating Cgroups must create a private PID namespace: %w", define.ErrInvalidArg)
		}
	}

	// Can only set static IP or MAC is creating a network namespace.
	if !c.config.CreateNetNS && (c.config.StaticIP != nil || c.config.StaticMAC != nil) {
		return fmt.Errorf("cannot set static IP or MAC address if not creating a network namespace: %w", define.ErrInvalidArg)
	}

	// Cannot set static IP or MAC if joining >1 network.
	if len(c.config.Networks) > 1 && (c.config.StaticIP != nil || c.config.StaticMAC != nil) {
		return fmt.Errorf("cannot set static IP or MAC address if joining more than one network: %w", define.ErrInvalidArg)
	}

	// Using image resolv.conf conflicts with various DNS settings.
	if c.config.UseImageResolvConf &&
		(len(c.config.DNSSearch) > 0 || len(c.config.DNSServer) > 0 ||
			len(c.config.DNSOption) > 0) {
		return fmt.Errorf("cannot configure DNS options if using image's resolv.conf: %w", define.ErrInvalidArg)
	}

	if c.config.UseImageHosts && len(c.config.HostAdd) > 0 {
		return fmt.Errorf("cannot add to /etc/hosts if using image's /etc/hosts: %w", define.ErrInvalidArg)
	}

	// Check named volume, overlay volume and image volume destination conflist
	destinations := make(map[string]bool)
	for _, vol := range c.config.NamedVolumes {
		// Don't check if they already exist.
		// If they don't we will automatically create them.
		if _, ok := destinations[vol.Dest]; ok {
			return fmt.Errorf("two volumes found with destination %s: %w", vol.Dest, define.ErrInvalidArg)
		}
		destinations[vol.Dest] = true
	}
	for _, vol := range c.config.OverlayVolumes {
		// Don't check if they already exist.
		// If they don't we will automatically create them.
		if _, ok := destinations[vol.Dest]; ok {
			return fmt.Errorf("two volumes found with destination %s: %w", vol.Dest, define.ErrInvalidArg)
		}
		destinations[vol.Dest] = true
	}
	for _, vol := range c.config.ImageVolumes {
		// Don't check if they already exist.
		// If they don't we will automatically create them.
		if _, ok := destinations[vol.Dest]; ok {
			return fmt.Errorf("two volumes found with destination %s: %w", vol.Dest, define.ErrInvalidArg)
		}
		destinations[vol.Dest] = true
	}

	// If User in the OCI spec is set, require that c.config.User is set for
	// security reasons (a lot of our code relies on c.config.User).
	if c.config.User == "" && (c.config.Spec.Process.User.UID != 0 || c.config.Spec.Process.User.GID != 0) {
		return fmt.Errorf("please set User explicitly via WithUser() instead of in OCI spec directly: %w", define.ErrInvalidArg)
	}

	// Init-ctrs must be used inside a Pod.  Check if an init container type is
	// passed and if no pod is passed
	if len(c.config.InitContainerType) > 0 && len(c.config.Pod) < 1 {
		return fmt.Errorf("init containers must be created in a pod: %w", define.ErrInvalidArg)
	}

	if c.config.SdNotifyMode == define.SdNotifyModeIgnore && len(c.config.SdNotifySocket) > 0 {
		return fmt.Errorf("cannot set sd-notify socket %q with sd-notify mode %q", c.config.SdNotifySocket, c.config.SdNotifyMode)
	}

	if c.config.HealthCheckOnFailureAction != define.HealthCheckOnFailureActionNone && c.config.HealthCheckConfig == nil {
		return fmt.Errorf("cannot set on-failure action to %s without a health check", c.config.HealthCheckOnFailureAction.String())
	}

	if value, exists := c.config.Labels[define.AutoUpdateLabel]; exists {
		// TODO: we cannot reference pkg/autoupdate here due to
		// circular dependencies.  It's worth considering moving the
		// auto-update logic into the libpod package.
		if value == "registry" || value == "image" {
			if err := validateAutoUpdateImageReference(c.config.RawImageName); err != nil {
				return err
			}
		}
	}

	// Autoremoving image requires autoremoving the associated container
	if c.config.Spec.Annotations != nil {
		if c.config.Spec.Annotations[define.InspectAnnotationAutoremoveImage] == define.InspectResponseTrue {
			if c.config.Spec.Annotations[define.InspectAnnotationAutoremove] != define.InspectResponseTrue {
				return fmt.Errorf("autoremoving image requires autoremoving the container: %w", define.ErrInvalidArg)
			}
			if c.config.Rootfs != "" {
				return fmt.Errorf("autoremoving image is not possible when a rootfs is in use: %w", define.ErrInvalidArg)
			}
		}
	}

	// Cannot set startup HC without a healthcheck
	if c.config.HealthCheckConfig == nil && c.config.StartupHealthCheckConfig != nil {
		return fmt.Errorf("cannot set a startup healthcheck when there is no regular healthcheck: %w", define.ErrInvalidArg)
	}

	// Ensure all ports list a single protocol
	for _, p := range c.config.PortMappings {
		if strings.Contains(p.Protocol, ",") {
			return fmt.Errorf("each port mapping must define a single protocol, got a comma-separated list for container port %d (protocols requested %q): %w", p.ContainerPort, p.Protocol, define.ErrInvalidArg)
		}
	}

	if c.config.IsDefaultInfra && !c.config.IsInfra {
		return fmt.Errorf("default rootfs-based infra container is set for non-infra container")
	}

	return nil
}

// validateAutoUpdateImageReference checks if the specified imageName is a
// fully-qualified image reference to the docker transport. Such a reference
// includes a domain, name and tag (e.g., quay.io/podman/stable:latest).  The
// reference may also be prefixed with "docker://" explicitly indicating that
// it's a reference to the docker transport.
func validateAutoUpdateImageReference(imageName string) error {
	// Make sure the input image is a docker.
	imageRef, err := alltransports.ParseImageName(imageName)
	if err == nil && imageRef.Transport().Name() != docker.Transport.Name() {
		return fmt.Errorf("auto updates require the docker image transport but image is of transport %q", imageRef.Transport().Name())
	} else if err != nil {
		if shortnames.IsShortName(imageName) {
			return fmt.Errorf("short name: auto updates require fully-qualified image reference: %q", imageName)
		}
	}
	return nil
}
