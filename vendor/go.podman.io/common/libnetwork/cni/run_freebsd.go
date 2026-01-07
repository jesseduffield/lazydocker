package cni

import (
	"os/exec"
)

// FreeBSD vnet adds the lo0 interface automatically - we just need to
// add the default address. Note: this will also add ::1 as a side
// effect.
func setupLoopback(namespacePath string) error {
	// Try to run the command using ifconfig's -j flag (supported in 13.3 and later)
	if err := exec.Command("ifconfig", "-j", namespacePath, "lo0", "inet", "127.0.0.1").Run(); err == nil {
		return nil
	}

	// Fall back to using the jexec wrapper to run the ifconfig command
	// inside the jail.
	return exec.Command("jexec", namespacePath, "ifconfig", "lo0", "inet", "127.0.0.1").Run()
}
