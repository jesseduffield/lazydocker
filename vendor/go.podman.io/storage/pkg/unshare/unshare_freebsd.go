//go:build freebsd

package unshare

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"

	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/reexec"
)

// Cmd wraps an exec.Cmd created by the reexec package in unshare(),
// and one day might handle setting ID maps and other related setting*s
// by triggering initialization code in the child.
type Cmd struct {
	*exec.Cmd
	Setsid  bool
	Setpgrp bool
	Ctty    *os.File
	Hook    func(pid int) error
}

// Command creates a new Cmd which can be customized.
func Command(args ...string) *Cmd {
	cmd := reexec.Command(args...)
	return &Cmd{
		Cmd: cmd,
	}
}

func (c *Cmd) Start() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Set environment variables to tell the child to synchronize its startup.
	if c.Env == nil {
		c.Env = os.Environ()
	}

	// Create the pipe for reading the child's PID.
	pidRead, pidWrite, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("creating pid pipe: %w", err)
	}
	c.Env = append(c.Env, fmt.Sprintf("_Containers-pid-pipe=%d", len(c.ExtraFiles)+3))
	c.ExtraFiles = append(c.ExtraFiles, pidWrite)

	// Create the pipe for letting the child know to proceed.
	continueRead, continueWrite, err := os.Pipe()
	if err != nil {
		pidRead.Close()
		pidWrite.Close()
		return fmt.Errorf("creating continue read/write pipe: %w", err)
	}
	c.Env = append(c.Env, fmt.Sprintf("_Containers-continue-pipe=%d", len(c.ExtraFiles)+3))
	c.ExtraFiles = append(c.ExtraFiles, continueRead)

	// Pass along other instructions.
	if c.Setsid {
		c.Env = append(c.Env, "_Containers-setsid=1")
	}
	if c.Setpgrp {
		c.Env = append(c.Env, "_Containers-setpgrp=1")
	}
	if c.Ctty != nil {
		c.Env = append(c.Env, fmt.Sprintf("_Containers-ctty=%d", len(c.ExtraFiles)+3))
		c.ExtraFiles = append(c.ExtraFiles, c.Ctty)
	}

	// Make sure we clean up our pipes.
	defer func() {
		if pidRead != nil {
			pidRead.Close()
		}
		if pidWrite != nil {
			pidWrite.Close()
		}
		if continueRead != nil {
			continueRead.Close()
		}
		if continueWrite != nil {
			continueWrite.Close()
		}
	}()

	// Start the new process.
	err = c.Cmd.Start()
	if err != nil {
		return err
	}

	// Close the ends of the pipes that the parent doesn't need.
	continueRead.Close()
	continueRead = nil
	pidWrite.Close()
	pidWrite = nil

	// Read the child's PID from the pipe.
	pidString := ""
	b := new(bytes.Buffer)
	if _, err := io.Copy(b, pidRead); err != nil {
		return fmt.Errorf("reading child PID: %w", err)
	}
	pidString = b.String()
	pid, err := strconv.Atoi(pidString)
	if err != nil {
		fmt.Fprintf(continueWrite, "error parsing PID %q: %v", pidString, err)
		return fmt.Errorf("parsing PID %q: %w", pidString, err)
	}

	// Run any additional setup that we want to do before the child starts running proper.
	if c.Hook != nil {
		if err = c.Hook(pid); err != nil {
			fmt.Fprintf(continueWrite, "hook error: %v", err)
			return err
		}
	}

	return nil
}

func (c *Cmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

func (c *Cmd) CombinedOutput() ([]byte, error) {
	return nil, errors.New("unshare: CombinedOutput() not implemented")
}

func (c *Cmd) Output() ([]byte, error) {
	return nil, errors.New("unshare: Output() not implemented")
}

type Runnable interface {
	Run() error
}

// ExecRunnable runs the specified unshare command, captures its exit status,
// and exits with the same status.
func ExecRunnable(cmd Runnable, cleanup func()) {
	exit := func(status int) {
		if cleanup != nil {
			cleanup()
		}
		os.Exit(status)
	}
	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if exitError.ProcessState.Exited() {
				if waitStatus, ok := exitError.ProcessState.Sys().(syscall.WaitStatus); ok {
					if waitStatus.Exited() {
						logrus.Debugf("%v", exitError)
						exit(waitStatus.ExitStatus())
					}
					if waitStatus.Signaled() {
						logrus.Debugf("%v", exitError)
						exit(int(waitStatus.Signal()) + 128)
					}
				}
			}
		}
		logrus.Errorf("%v", err)
		logrus.Errorf("(Unable to determine exit status)")
		exit(1)
	}
	exit(0)
}
