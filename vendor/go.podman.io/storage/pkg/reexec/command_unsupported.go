//go:build !linux && !windows && !freebsd && !solaris && !darwin

package reexec

import (
	"context"
	"os/exec"
)

// Command is unsupported on operating systems apart from Linux, Windows, Solaris and Darwin.
func Command(args ...string) *exec.Cmd {
	panicIfNotInitialized()
	return nil
}

// CommandContext is unsupported on operating systems apart from Linux, Windows, Solaris and Darwin.
func CommandContext(ctx context.Context, args ...string) *exec.Cmd {
	panicIfNotInitialized()
	return nil
}
