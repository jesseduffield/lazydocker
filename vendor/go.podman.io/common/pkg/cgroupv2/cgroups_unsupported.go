//go:build !linux

package cgroupv2

// Enabled returns whether we are running on cgroup v2.
func Enabled() (bool, error) {
	return false, nil
}
