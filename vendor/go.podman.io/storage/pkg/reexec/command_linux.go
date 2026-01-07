//go:build linux

package reexec

import (
	"context"
	"os/exec"
)

// Self returns the path to the current process's binary.
// Returns "/proc/self/exe".
func Self() string {
	return "/proc/self/exe"
}

// Command returns *exec.Cmd which has Path as current binary.
// This will use the in-memory version (/proc/self/exe) of the current binary,
// it is thus safe to delete or replace the on-disk binary (os.Args[0]).
func Command(args ...string) *exec.Cmd {
	panicIfNotInitialized()
	cmd := exec.Command(Self())
	cmd.Args = args
	return cmd
}

// CommandContext returns *exec.Cmd which has Path as current binary.
// This will use the in-memory version (/proc/self/exe) of the current binary,
// it is thus safe to delete or replace the on-disk binary (os.Args[0]).
func CommandContext(ctx context.Context, args ...string) *exec.Cmd {
	panicIfNotInitialized()
	cmd := exec.CommandContext(ctx, Self())
	cmd.Args = args
	return cmd
}
