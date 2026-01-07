//go:build linux

package specgen

import (
	"fmt"

	spec "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

// statBlkDev returns path's major and minor, or an error, if path is not a block device.
func statBlkDev(path string) (int64, int64, error) {
	var stat unix.Stat_t

	if err := unix.Stat(path, &stat); err != nil {
		return 0, 0, fmt.Errorf("could not parse device %s: %w", path, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFBLK {
		return 0, 0, fmt.Errorf("%s: not a block device", path)
	}
	rdev := uint64(stat.Rdev) //nolint:unconvert // Some architectures have different type.
	return int64(unix.Major(rdev)), int64(unix.Minor(rdev)), nil
}

// fillThrottleDev fills in dev.Major and dev.Minor fields based on path to a block device.
func fillThrottleDev(path string, dev *spec.LinuxThrottleDevice) error {
	major, minor, err := statBlkDev(path)
	if err != nil {
		return err
	}

	dev.Major, dev.Minor = major, minor

	return nil
}

// FinishThrottleDevices takes the temporary representation of the throttle
// devices in the specgen, fills in major and minor numbers, and amends the
// specgen with the specified devices. It returns an error if any device
// specified doesn't exist or is not a block device.
func FinishThrottleDevices(s *SpecGenerator) error {
	if len(s.ThrottleReadBpsDevice)+len(s.ThrottleWriteBpsDevice)+len(s.ThrottleReadIOPSDevice)+len(s.ThrottleWriteIOPSDevice) == 0 {
		return nil
	}

	if s.ResourceLimits == nil {
		s.ResourceLimits = &spec.LinuxResources{}
	}
	if s.ResourceLimits.BlockIO == nil {
		s.ResourceLimits.BlockIO = &spec.LinuxBlockIO{}
	}

	for k, v := range s.ThrottleReadBpsDevice {
		if err := fillThrottleDev(k, &v); err != nil {
			return err
		}
		s.ResourceLimits.BlockIO.ThrottleReadBpsDevice = append(s.ResourceLimits.BlockIO.ThrottleReadBpsDevice, v)
	}

	for k, v := range s.ThrottleWriteBpsDevice {
		if err := fillThrottleDev(k, &v); err != nil {
			return err
		}
		s.ResourceLimits.BlockIO.ThrottleWriteBpsDevice = append(s.ResourceLimits.BlockIO.ThrottleWriteBpsDevice, v)
	}

	for k, v := range s.ThrottleReadIOPSDevice {
		if err := fillThrottleDev(k, &v); err != nil {
			return err
		}
		s.ResourceLimits.BlockIO.ThrottleReadIOPSDevice = append(s.ResourceLimits.BlockIO.ThrottleReadIOPSDevice, v)
	}

	for k, v := range s.ThrottleWriteIOPSDevice {
		if err := fillThrottleDev(k, &v); err != nil {
			return err
		}
		s.ResourceLimits.BlockIO.ThrottleWriteIOPSDevice = append(s.ResourceLimits.BlockIO.ThrottleWriteIOPSDevice, v)
	}

	return nil
}

func WeightDevices(specgen *SpecGenerator) error {
	if len(specgen.WeightDevice) == 0 {
		return nil
	}

	if specgen.ResourceLimits == nil {
		specgen.ResourceLimits = &spec.LinuxResources{}
	}
	if specgen.ResourceLimits.BlockIO == nil {
		specgen.ResourceLimits.BlockIO = &spec.LinuxBlockIO{}
	}

	for k, v := range specgen.WeightDevice {
		major, minor, err := statBlkDev(k)
		if err != nil {
			return fmt.Errorf("bad --blkio-weight-device: %w", err)
		}
		specgen.ResourceLimits.BlockIO.WeightDevice = append(specgen.ResourceLimits.BlockIO.WeightDevice,
			spec.LinuxWeightDevice{
				LinuxBlockIODevice: spec.LinuxBlockIODevice{
					Major: major,
					Minor: minor,
				},
				Weight: v.Weight,
			})
	}

	return nil
}
