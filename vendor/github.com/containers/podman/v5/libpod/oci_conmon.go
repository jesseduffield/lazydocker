//go:build !remote

package libpod

const (
	// cgroupSplit is the cgroup mode for reusing the current cgroup both
	// for conmon and for the container payload.
	cgroupSplit = "split"
)
