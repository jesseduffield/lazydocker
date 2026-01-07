// Package exec provides utilities for executing Open Container Initiative runtime hooks.
package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	osexec "os/exec"
	"time"

	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

// DefaultPostKillTimeout is the recommended default post-kill timeout.
const DefaultPostKillTimeout = time.Duration(10) * time.Second

type RunOptions struct {
	// The hook to run
	Hook *rspec.Hook
	// The workdir to change when invoking the hook
	Dir string
	// The container state data to pass into the hook process
	State []byte
	// Stdout from the hook process
	Stdout io.Writer
	// Stderr from the hook process
	Stderr io.Writer
	// Timeout for waiting process killed
	PostKillTimeout time.Duration
}

// Run executes the hook and waits for it to complete or for the
// context or hook-specified timeout to expire.
//
// Deprecated: Too many arguments, has been refactored and replaced by RunWithOptions instead.
func Run(ctx context.Context, hook *rspec.Hook, state []byte, stdout io.Writer, stderr io.Writer, postKillTimeout time.Duration) (hookErr, err error) {
	return RunWithOptions(
		ctx,
		RunOptions{
			Hook:            hook,
			State:           state,
			Stdout:          stdout,
			Stderr:          stderr,
			PostKillTimeout: postKillTimeout,
		},
	)
}

// RunWithOptions executes the hook and waits for it to complete or for the
// context or hook-specified timeout to expire.
func RunWithOptions(ctx context.Context, options RunOptions) (hookErr, err error) {
	hook := options.Hook
	cmd := osexec.Cmd{
		Path:   hook.Path,
		Args:   hook.Args,
		Env:    hook.Env,
		Dir:    options.Dir,
		Stdin:  bytes.NewReader(options.State),
		Stdout: options.Stdout,
		Stderr: options.Stderr,
	}
	if cmd.Env == nil {
		cmd.Env = []string{}
	}

	if hook.Timeout != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*hook.Timeout)*time.Second)
		defer cancel()
	}

	err = cmd.Start()
	if err != nil {
		return err, err
	}
	exit := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			err = fmt.Errorf("executing %v: %w", cmd.Args, err)
		}
		exit <- err
	}()

	select {
	case err = <-exit:
		return err, err
	case <-ctx.Done():
		if err := cmd.Process.Kill(); err != nil {
			logrus.Errorf("Failed to kill pid %v", cmd.Process)
		}
		timer := time.NewTimer(options.PostKillTimeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			err = fmt.Errorf("failed to reap process within %s of the kill signal", options.PostKillTimeout)
		case err = <-exit:
		}
		return err, ctx.Err()
	}
}
