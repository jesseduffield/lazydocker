//go:build solaris || darwin

package reexec

import (
	"context"
	"os/exec"
)

// Self returns the path to the current process's binary.
// Uses os.Args[0].
func Self() string {
	return naiveSelf()
}

// Command returns *exec.Cmd which has Path as current binary.
// For example if current binary is "docker" at "/usr/bin/", then cmd.Path will
// be set to "/usr/bin/docker".
func Command(args ...string) *exec.Cmd {
	panicIfNotInitialized()
	cmd := exec.Command(Self())
	cmd.Args = args
	return cmd
}

// CommandContext returns *exec.Cmd which has Path as current binary.
func CommandContext(ctx context.Context, args ...string) *exec.Cmd {
	panicIfNotInitialized()
	cmd := exec.CommandContext(ctx, Self())
	cmd.Args = args
	return cmd
}
