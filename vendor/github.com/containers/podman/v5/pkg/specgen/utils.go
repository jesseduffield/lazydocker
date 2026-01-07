//go:build !linux

package specgen

// FinishThrottleDevices cannot be called on non-linux OS' due to importing unix functions
func FinishThrottleDevices(_ *SpecGenerator) error {
	return nil
}

// WeightDevices cannot be called on non-linux OS' due to importing unix functions
func WeightDevices(_ *SpecGenerator) error {
	return nil
}
