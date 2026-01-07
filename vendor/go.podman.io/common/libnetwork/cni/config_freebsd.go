//go:build (linux || freebsd) && cni

package cni

import (
	"os/exec"

	"github.com/sirupsen/logrus"
)

func deleteLink(name string) {
	if output, err := exec.Command("ifconfig", name, "destroy").CombinedOutput(); err != nil {
		// only log the error, it is not fatal
		logrus.Infof("Failed to remove network interface %s: %v: %s", name, err, output)
	}
}
