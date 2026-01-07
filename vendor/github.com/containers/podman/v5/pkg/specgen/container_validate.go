package specgen

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
)

var (
	// ErrInvalidSpecConfig describes an error that the given SpecGenerator is invalid
	ErrInvalidSpecConfig = errors.New("invalid configuration")
	// SystemDValues describes the only values that SystemD can be
	SystemDValues = []string{"true", "false", "always"}
	// ImageVolumeModeValues describes the only values that ImageVolumeMode can be
	ImageVolumeModeValues = []string{"ignore", define.TypeTmpfs, "anonymous"}
)

func exclusiveOptions(opt1, opt2 string) error {
	return fmt.Errorf("%s and %s are mutually exclusive options", opt1, opt2)
}

// Validate verifies that the given SpecGenerator is valid and satisfies required
// input for creating a container.
func (s *SpecGenerator) Validate() error {
	// Containers being added to a pod cannot have certain network attributes
	// associated with them because those should be on the infra container.
	if len(s.Pod) > 0 && s.NetNS.NSMode == FromPod {
		if len(s.Networks) > 0 {
			return fmt.Errorf("networks must be defined when the pod is created: %w", define.ErrNetworkOnPodContainer)
		}
		if len(s.PortMappings) > 0 || (s.PublishExposedPorts != nil && *s.PublishExposedPorts) {
			return fmt.Errorf("published or exposed ports must be defined when the pod is created: %w", define.ErrNetworkOnPodContainer)
		}
		if len(s.HostAdd) > 0 {
			return fmt.Errorf("extra host entries must be specified on the pod: %w", define.ErrNetworkOnPodContainer)
		}
	}

	if s.NetNS.IsContainer() && len(s.HostAdd) > 0 {
		return fmt.Errorf("cannot set extra host entries when the container is joined to another containers network namespace: %w", ErrInvalidSpecConfig)
	}

	//
	// ContainerBasicConfig
	//
	// Rootfs and Image cannot both populated
	if len(s.ContainerStorageConfig.Image) > 0 && len(s.ContainerStorageConfig.Rootfs) > 0 {
		return fmt.Errorf("both image and rootfs cannot be simultaneously: %w", ErrInvalidSpecConfig)
	}
	// Cannot set hostname and utsns
	if len(s.ContainerBasicConfig.Hostname) > 0 && !s.ContainerBasicConfig.UtsNS.IsPrivate() {
		if s.ContainerBasicConfig.UtsNS.IsPod() {
			return fmt.Errorf("cannot set hostname when joining the pod UTS namespace: %w", ErrInvalidSpecConfig)
		}

		return fmt.Errorf("cannot set hostname when running in the host UTS namespace: %w", ErrInvalidSpecConfig)
	}
	// systemd values must be true, false, or always
	if len(s.ContainerBasicConfig.Systemd) > 0 && !slices.Contains(SystemDValues, strings.ToLower(s.ContainerBasicConfig.Systemd)) {
		return fmt.Errorf("--systemd values must be one of %q: %w", strings.Join(SystemDValues, ", "), ErrInvalidSpecConfig)
	}

	if err := define.ValidateSdNotifyMode(s.ContainerBasicConfig.SdNotifyMode); err != nil {
		return err
	}

	//
	// ContainerStorageConfig
	//
	// rootfs and image cannot both be set
	if len(s.ContainerStorageConfig.Image) > 0 && len(s.ContainerStorageConfig.Rootfs) > 0 {
		return exclusiveOptions("rootfs", "image")
	}
	// imagevolumemode must be one of ignore, tmpfs, or anonymous if given
	if len(s.ContainerStorageConfig.ImageVolumeMode) > 0 && !slices.Contains(ImageVolumeModeValues, strings.ToLower(s.ContainerStorageConfig.ImageVolumeMode)) {
		return fmt.Errorf("invalid ImageVolumeMode %q, value must be one of %s",
			s.ContainerStorageConfig.ImageVolumeMode, strings.Join(ImageVolumeModeValues, ","))
	}
	// shmsize conflicts with IPC namespace
	if s.ContainerStorageConfig.ShmSize != nil && (s.ContainerStorageConfig.IpcNS.IsHost() || s.ContainerStorageConfig.IpcNS.IsNone()) {
		return fmt.Errorf("cannot set shmsize when running in the %s IPC Namespace", s.ContainerStorageConfig.IpcNS)
	}

	//
	// ContainerSecurityConfig
	//
	// userns and idmappings conflict
	if s.UserNS.IsPrivate() && s.IDMappings == nil {
		return fmt.Errorf("IDMappings are required when not creating a User namespace: %w", ErrInvalidSpecConfig)
	}

	//
	// ContainerCgroupConfig
	//
	//
	// None for now

	//
	// ContainerNetworkConfig
	//
	// useimageresolveconf conflicts with dnsserver, dnssearch, dnsoption
	if s.UseImageResolvConf != nil && *s.UseImageResolvConf {
		if len(s.DNSServers) > 0 {
			return exclusiveOptions("UseImageResolvConf", "DNSServer")
		}
		if len(s.DNSSearch) > 0 {
			return exclusiveOptions("UseImageResolvConf", "DNSSearch")
		}
		if len(s.DNSOptions) > 0 {
			return exclusiveOptions("UseImageResolvConf", "DNSOption")
		}
	}
	// UseImageHosts and HostAdd are exclusive
	if (s.UseImageHosts != nil && *s.UseImageHosts) && len(s.HostAdd) > 0 {
		return exclusiveOptions("UseImageHosts", "HostAdd")
	}

	// TODO the specgen does not appear to handle this?  Should it
	// switch config.Cgroup.Cgroups {
	// case "disabled":
	//	if addedResources {
	//		return errors.New("cannot specify resource limits when cgroups are disabled is specified")
	//	}
	//	configSpec.Linux.Resources = &spec.LinuxResources{}
	// case "enabled", "no-conmon", "":
	//	// Do nothing
	// default:
	//	return errors.New("unrecognized option for cgroups; supported are 'default', 'disabled', 'no-conmon'")
	// }
	// Namespaces
	if err := s.UtsNS.validate(); err != nil {
		return err
	}
	if err := validateIPCNS(&s.IpcNS); err != nil {
		return err
	}
	if err := s.PidNS.validate(); err != nil {
		return err
	}
	if err := s.CgroupNS.validate(); err != nil {
		return err
	}
	if err := validateUserNS(&s.UserNS); err != nil {
		return err
	}

	if err := validateNetNS(&s.NetNS); err != nil {
		return err
	}
	if s.NetNS.NSMode != Bridge && len(s.Networks) > 0 {
		// Note that we also get the ip and mac in the networks map
		return errors.New("networks and static ip/mac address can only be used with Bridge mode networking")
	}

	return nil
}
