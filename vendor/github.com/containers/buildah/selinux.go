//go:build linux

package buildah

import (
	"errors"
	"fmt"
	"os"

	"github.com/opencontainers/runtime-tools/generate"
	selinux "github.com/opencontainers/selinux/go-selinux"
)

func selinuxGetEnabled() bool {
	return selinux.GetEnabled()
}

func setupSelinux(g *generate.Generator, processLabel, mountLabel string) {
	if processLabel != "" && selinux.GetEnabled() {
		g.SetProcessSelinuxLabel(processLabel)
		g.SetLinuxMountLabel(mountLabel)
	}
}

func runLabelStdioPipes(stdioPipe [][]int, processLabel, mountLabel string) error {
	if !selinuxGetEnabled() || processLabel == "" || mountLabel == "" {
		// SELinux is completely disabled, or we're not doing anything at all with labeling
		return nil
	}
	pipeContext, err := selinux.ComputeCreateContext(processLabel, mountLabel, "fifo_file")
	if err != nil {
		return fmt.Errorf("computing file creation context for pipes: %w", err)
	}
	for i := range stdioPipe {
		pipeFdName := fmt.Sprintf("/proc/self/fd/%d", stdioPipe[i][0])
		if err := selinux.SetFileLabel(pipeFdName, pipeContext); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("setting file label on %q: %w", pipeFdName, err)
		}
	}
	return nil
}
