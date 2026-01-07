package legacyx509

import "fmt"

// legacyGodebugSetting is a type mimicking Go's internal godebug package
// settings, which are used to enable / disable certain functionalities at
// build time.
type legacyGodebugSetting int

func (s legacyGodebugSetting) Value() string {
	return fmt.Sprintf("%d", s)
}

func (s legacyGodebugSetting) IncNonDefault() {}
