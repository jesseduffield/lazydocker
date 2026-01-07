//go:build !linux && !freebsd && !darwin

package open

func inChroot(requests requests) results {
	return results{Err: "open-in-chroot not available on this platform"}
}
