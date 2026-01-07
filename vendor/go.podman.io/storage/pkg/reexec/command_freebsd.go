//go:build freebsd

package reexec

import (
	"context"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

// Self returns the path to the current process's binary.
// Uses sysctl.
func Self() string {
	path, err := unix.SysctlArgs("kern.proc.pathname", -1)
	if err == nil {
		return path
	}
	return os.Args[0]
}

// Command returns *exec.Cmd which has Path as current binary.
// For example if current binary is "docker" at "/usr/bin/", then cmd.Path will
// be set to "/usr/bin/docker".
func Command(args ...string) *exec.Cmd {
	cmd := exec.Command(Self())
	cmd.Args = args
	return cmd
}

// CommandContext returns *exec.Cmd which has Path as current binary.
func CommandContext(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, Self())
	cmd.Args = args
	return cmd
}
