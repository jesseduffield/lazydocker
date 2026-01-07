package define

import "fmt"

// Strings used for --sdnotify option to podman
const (
	SdNotifyModeConmon    = "conmon"
	SdNotifyModeContainer = "container"
	SdNotifyModeHealthy   = "healthy"
	SdNotifyModeIgnore    = "ignore"
)

// ValidateSdNotifyMode validates the specified mode.
func ValidateSdNotifyMode(mode string) error {
	switch mode {
	case "", SdNotifyModeContainer, SdNotifyModeConmon, SdNotifyModeIgnore, SdNotifyModeHealthy:
		return nil
	default:
		return fmt.Errorf("%w: invalid sdnotify value %q: must be %s, %s, %s or %s", ErrInvalidArg, mode, SdNotifyModeConmon, SdNotifyModeContainer, SdNotifyModeHealthy, SdNotifyModeIgnore)
	}
}
