//go:build linux

package chroot

import (
	"fmt"

	"github.com/opencontainers/runtime-spec/specs-go"
	selinux "github.com/opencontainers/selinux/go-selinux"
	"github.com/sirupsen/logrus"
)

// setSelinuxLabel sets the process label for child processes that we'll start.
func setSelinuxLabel(spec *specs.Spec) error {
	logrus.Debugf("setting selinux label")
	if spec.Process.SelinuxLabel != "" && selinux.GetEnabled() {
		if err := selinux.SetExecLabel(spec.Process.SelinuxLabel); err != nil {
			return fmt.Errorf("setting process label to %q: %w", spec.Process.SelinuxLabel, err)
		}
	}
	return nil
}
