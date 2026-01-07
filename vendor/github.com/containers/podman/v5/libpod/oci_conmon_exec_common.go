//go:build !remote && (linux || freebsd)

package libpod

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/errorhandling"
	"github.com/containers/podman/v5/pkg/lookup"
	"github.com/containers/podman/v5/pkg/pidhandle"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/common/pkg/config"
	"go.podman.io/common/pkg/detach"
	"go.podman.io/common/pkg/resize"
	"golang.org/x/sys/unix"
)

// ExecContainer executes a command in a running container
func (r *ConmonOCIRuntime) ExecContainer(c *Container, sessionID string, options *ExecOptions, streams *define.AttachStreams, newSize *resize.TerminalSize) (int, chan error, error) {
	if options == nil {
		return -1, nil, fmt.Errorf("must provide an ExecOptions struct to ExecContainer: %w", define.ErrInvalidArg)
	}
	if len(options.Cmd) == 0 {
		return -1, nil, fmt.Errorf("must provide a command to execute: %w", define.ErrInvalidArg)
	}

	if sessionID == "" {
		return -1, nil, fmt.Errorf("must provide a session ID for exec: %w", define.ErrEmptyID)
	}

	// TODO: Should we default this to false?
	// Or maybe make streams mandatory?
	attachStdin := true
	if streams != nil {
		attachStdin = streams.AttachInput
	}

	var ociLog string
	if logrus.GetLevel() != logrus.DebugLevel && r.supportsJSON {
		ociLog = c.execOCILog(sessionID)
	}

	execCmd, pipes, err := r.startExec(c, sessionID, options, attachStdin, ociLog)
	if err != nil {
		return -1, nil, err
	}

	// Only close sync pipe. Start and attach are consumed in the attach
	// goroutine.
	defer func() {
		if pipes.syncPipe != nil && !pipes.syncClosed {
			errorhandling.CloseQuiet(pipes.syncPipe)
			pipes.syncClosed = true
		}
	}()

	// TODO Only create if !detach
	// Attach to the container before starting it
	attachChan := make(chan error)
	go func() {
		// attachToExec is responsible for closing pipes
		attachChan <- c.attachToExec(streams, options.DetachKeys, sessionID, pipes.startPipe, pipes.attachPipe, newSize)
		close(attachChan)
	}()

	if err := execCmd.Wait(); err != nil {
		return -1, nil, fmt.Errorf("cannot run conmon: %w", err)
	}

	pid, err := readConmonPipeData(r.name, pipes.syncPipe, ociLog)

	return pid, attachChan, err
}

// ExecContainerHTTP executes a new command in an existing container and
// forwards its standard streams over an attach
func (r *ConmonOCIRuntime) ExecContainerHTTP(ctr *Container, sessionID string, options *ExecOptions, req *http.Request, w http.ResponseWriter,
	streams *HTTPAttachStreams, cancel <-chan bool, hijackDone chan<- bool, holdConnOpen <-chan bool, newSize *resize.TerminalSize) (int, chan error, error) {
	if streams != nil {
		if !streams.Stdin && !streams.Stdout && !streams.Stderr {
			return -1, nil, fmt.Errorf("must provide at least one stream to attach to: %w", define.ErrInvalidArg)
		}
	}

	if options == nil {
		return -1, nil, fmt.Errorf("must provide exec options to ExecContainerHTTP: %w", define.ErrInvalidArg)
	}

	detachString := config.DefaultDetachKeys
	if options.DetachKeys != nil {
		detachString = *options.DetachKeys
	}
	detachKeys, err := processDetachKeys(detachString)
	if err != nil {
		return -1, nil, err
	}

	// TODO: Should we default this to false?
	// Or maybe make streams mandatory?
	attachStdin := true
	if streams != nil {
		attachStdin = streams.Stdin
	}

	var ociLog string
	if logrus.GetLevel() != logrus.DebugLevel && r.supportsJSON {
		ociLog = ctr.execOCILog(sessionID)
	}

	execCmd, pipes, err := r.startExec(ctr, sessionID, options, attachStdin, ociLog)
	if err != nil {
		return -1, nil, err
	}

	// Only close sync pipe. Start and attach are consumed in the attach
	// goroutine.
	defer func() {
		if pipes.syncPipe != nil && !pipes.syncClosed {
			errorhandling.CloseQuiet(pipes.syncPipe)
			pipes.syncClosed = true
		}
	}()

	attachChan := make(chan error)
	conmonPipeDataChan := make(chan conmonPipeData)
	go func() {
		// attachToExec is responsible for closing pipes
		attachChan <- attachExecHTTP(ctr, sessionID, req, w, streams, pipes, detachKeys, options.Terminal, cancel, hijackDone, holdConnOpen, execCmd, conmonPipeDataChan, ociLog, newSize, r.name)
		close(attachChan)
	}()

	// NOTE: the channel is needed to communicate conmon's data.  In case
	// of an error, the error will be written on the hijacked http
	// connection such that remote clients will receive the error.
	pipeData := <-conmonPipeDataChan

	return pipeData.pid, attachChan, pipeData.err
}

// conmonPipeData contains the data when reading from conmon's pipe.
type conmonPipeData struct {
	pid int
	err error
}

// ExecContainerDetached executes a command in a running container, but does
// not attach to it.
func (r *ConmonOCIRuntime) ExecContainerDetached(ctr *Container, sessionID string, options *ExecOptions, stdin bool) (int, error) {
	if options == nil {
		return -1, fmt.Errorf("must provide exec options to ExecContainerHTTP: %w", define.ErrInvalidArg)
	}

	var ociLog string
	if logrus.GetLevel() != logrus.DebugLevel && r.supportsJSON {
		ociLog = ctr.execOCILog(sessionID)
	}

	execCmd, pipes, err := r.startExec(ctr, sessionID, options, stdin, ociLog)
	if err != nil {
		return -1, err
	}

	defer func() {
		pipes.cleanup()
	}()

	// Wait for Conmon to tell us we're ready to attach.
	// We aren't actually *going* to attach, but this means that we're good
	// to proceed.
	if _, err := readConmonPipeData(r.name, pipes.attachPipe, ""); err != nil {
		return -1, err
	}

	// Start the exec session
	if err := writeConmonPipeData(pipes.startPipe); err != nil {
		return -1, err
	}

	// Wait for conmon to succeed, when return.
	if err := execCmd.Wait(); err != nil {
		return -1, fmt.Errorf("cannot run conmon: %w", err)
	}

	pid, err := readConmonPipeData(r.name, pipes.syncPipe, ociLog)

	return pid, err
}

// ExecAttachResize resizes the TTY of the given exec session.
func (r *ConmonOCIRuntime) ExecAttachResize(ctr *Container, sessionID string, newSize resize.TerminalSize) error {
	controlFile, err := openControlFile(ctr, ctr.execBundlePath(sessionID))
	if err != nil {
		return err
	}
	defer controlFile.Close()

	if _, err = fmt.Fprintf(controlFile, "%d %d %d\n", 1, newSize.Height, newSize.Width); err != nil {
		return fmt.Errorf("failed to write to ctl file to resize terminal: %w", err)
	}

	return nil
}

// ExecStopContainer stops a given exec session in a running container.
func (r *ConmonOCIRuntime) ExecStopContainer(ctr *Container, sessionID string, timeout uint) error {
	pid, pidData, err := ctr.getExecSessionPID(sessionID)
	if err != nil {
		return err
	}

	logrus.Debugf("Going to stop container %s exec session %s", ctr.ID(), sessionID)

	pidHandle, err := pidhandle.NewPIDHandleFromString(pid, pidData)
	if err != nil {
		return fmt.Errorf("getting the PID handle for pid %d from '%s': %w", pid, pidData, err)
	}
	defer pidHandle.Close()

	// Is the session dead?
	sessionAlive, err := pidHandle.IsAlive()
	if err != nil {
		return fmt.Errorf("getting the process status for pid %d: %w", pid, err)
	}
	if !sessionAlive {
		return nil
	}

	if timeout > 0 {
		// Use SIGTERM by default, then SIGSTOP after timeout.
		logrus.Debugf("Killing exec session %s (PID %d) of container %s with SIGTERM", sessionID, pid, ctr.ID())
		if err := pidHandle.Kill(unix.SIGTERM); err != nil {
			if err == unix.ESRCH {
				return nil
			}
			return fmt.Errorf("killing container %s exec session %s PID %d with SIGTERM: %w", ctr.ID(), sessionID, pid, err)
		}

		// Wait for the PID to stop
		if err := waitPidStop(pid, time.Duration(timeout)*time.Second); err != nil {
			logrus.Infof("Timed out waiting for container %s exec session %s to stop, resorting to SIGKILL: %v", ctr.ID(), sessionID, err)
		} else {
			// No error, container is dead
			return nil
		}
	}

	// SIGTERM did not work. On to SIGKILL.
	logrus.Debugf("Killing exec session %s (PID %d) of container %s with SIGKILL", sessionID, pid, ctr.ID())
	if err := pidHandle.Kill(unix.SIGKILL); err != nil {
		if err == unix.ESRCH {
			return nil
		}
		return fmt.Errorf("killing container %s exec session %s PID %d with SIGKILL: %w", ctr.ID(), sessionID, pid, err)
	}

	// Wait for the PID to stop
	if err := waitPidStop(pid, killContainerTimeout); err != nil {
		return fmt.Errorf("timed out waiting for container %s exec session %s PID %d to stop after SIGKILL: %w", ctr.ID(), sessionID, pid, err)
	}

	return nil
}

// ExecUpdateStatus checks if the given exec session is still running.
func (r *ConmonOCIRuntime) ExecUpdateStatus(ctr *Container, sessionID string) (bool, error) {
	pid, pidData, err := ctr.getExecSessionPID(sessionID)
	if err != nil {
		return false, err
	}

	logrus.Debugf("Checking status of container %s exec session %s", ctr.ID(), sessionID)

	pidHandle, err := pidhandle.NewPIDHandleFromString(pid, pidData)
	if err != nil {
		return false, fmt.Errorf("getting the PID handle for pid %d from '%s': %w", pid, pidData, err)
	}
	defer pidHandle.Close()

	// Is the session dead?
	sessionAlive, err := pidHandle.IsAlive()
	if err != nil {
		return false, fmt.Errorf("getting the process status for pid %d: %w", pid, err)
	}

	return sessionAlive, nil
}

// ExecAttachSocketPath is the path to a container's exec session attach socket.
func (r *ConmonOCIRuntime) ExecAttachSocketPath(ctr *Container, sessionID string) (string, error) {
	// We don't even use container, so don't validity check it
	if sessionID == "" {
		return "", fmt.Errorf("must provide a valid session ID to get attach socket path: %w", define.ErrInvalidArg)
	}

	return filepath.Join(ctr.execBundlePath(sessionID), "attach"), nil
}

// This contains pipes used by the exec API.
type execPipes struct {
	syncPipe     *os.File
	syncClosed   bool
	startPipe    *os.File
	startClosed  bool
	attachPipe   *os.File
	attachClosed bool
}

func (p *execPipes) cleanup() {
	if p.syncPipe != nil && !p.syncClosed {
		errorhandling.CloseQuiet(p.syncPipe)
		p.syncClosed = true
	}
	if p.startPipe != nil && !p.startClosed {
		errorhandling.CloseQuiet(p.startPipe)
		p.startClosed = true
	}
	if p.attachPipe != nil && !p.attachClosed {
		errorhandling.CloseQuiet(p.attachPipe)
		p.attachClosed = true
	}
}

// Start an exec session's conmon parent from the given options.
func (r *ConmonOCIRuntime) startExec(c *Container, sessionID string, options *ExecOptions, attachStdin bool, ociLog string) (_ *exec.Cmd, _ *execPipes, deferredErr error) {
	pipes := new(execPipes)

	if options == nil {
		return nil, nil, fmt.Errorf("must provide an ExecOptions struct to ExecContainer: %w", define.ErrInvalidArg)
	}
	if len(options.Cmd) == 0 {
		return nil, nil, fmt.Errorf("must provide a command to execute: %w", define.ErrInvalidArg)
	}

	if sessionID == "" {
		return nil, nil, fmt.Errorf("must provide a session ID for exec: %w", define.ErrEmptyID)
	}

	// create sync pipe to receive the pid
	parentSyncPipe, childSyncPipe, err := newPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("creating socket pair: %w", err)
	}
	pipes.syncPipe = parentSyncPipe

	defer func() {
		if deferredErr != nil {
			pipes.cleanup()
		}
	}()

	// create start pipe to set the cgroup before running
	// attachToExec is responsible for closing parentStartPipe
	childStartPipe, parentStartPipe, err := newPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("creating socket pair: %w", err)
	}
	pipes.startPipe = parentStartPipe

	// create the attach pipe to allow attach socket to be created before
	// $RUNTIME exec starts running. This is to make sure we can capture all output
	// from the process through that socket, rather than half reading the log, half attaching to the socket
	// attachToExec is responsible for closing parentAttachPipe
	parentAttachPipe, childAttachPipe, err := newPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("creating socket pair: %w", err)
	}
	pipes.attachPipe = parentAttachPipe

	childrenClosed := false
	defer func() {
		if !childrenClosed {
			errorhandling.CloseQuiet(childSyncPipe)
			errorhandling.CloseQuiet(childAttachPipe)
			errorhandling.CloseQuiet(childStartPipe)
		}
	}()

	finalEnv := make([]string, 0, len(options.Env))
	for k, v := range options.Env {
		finalEnv = append(finalEnv, fmt.Sprintf("%s=%s", k, v))
	}

	processFile, err := c.prepareProcessExec(options, finalEnv, sessionID)
	if err != nil {
		return nil, nil, err
	}
	defer processFile.Close()

	args, err := r.sharedConmonArgs(c, sessionID, c.execBundlePath(sessionID), c.execPidPath(sessionID), c.execLogPath(sessionID), c.execExitFileDir(sessionID), c.execPersistDir(sessionID), ociLog, define.NoLogging, c.config.LogTag)
	if err != nil {
		return nil, nil, err
	}

	preserveFDs, filesToClose, extraFiles, err := getPreserveFdExtraFiles(options.PreserveFD, options.PreserveFDs)
	if err != nil {
		return nil, nil, err
	}

	if preserveFDs > 0 {
		args = append(args, formatRuntimeOpts("--preserve-fds", strconv.FormatUint(uint64(preserveFDs), 10))...)
	}

	if options.Terminal {
		args = append(args, "-t")
	}

	if attachStdin {
		args = append(args, "-i")
	}

	// Append container ID and command
	args = append(args, "-e")
	// TODO make this optional when we can detach
	args = append(args, "--exec-attach")
	args = append(args, "--exec-process-spec", processFile.Name())

	if len(options.ExitCommand) > 0 {
		args = append(args, "--exit-command", options.ExitCommand[0])
		for _, arg := range options.ExitCommand[1:] {
			args = append(args, []string{"--exit-command-arg", arg}...)
		}
		if options.ExitCommandDelay > 0 {
			args = append(args, []string{"--exit-delay", strconv.FormatUint(uint64(options.ExitCommandDelay), 10)}...)
		}
	}

	logrus.WithFields(logrus.Fields{
		"args": args,
	}).Debugf("running conmon: %s", r.conmonPath)
	execCmd := exec.Command(r.conmonPath, args...)

	// TODO: This is commented because it doesn't make much sense in HTTP
	// attach, and I'm not certain it does for non-HTTP attach as well.
	// if streams != nil {
	// 	// Don't add the InputStream to the execCmd. Instead, the data should be passed
	// 	// through CopyDetachable
	// 	if streams.AttachOutput {
	// 		execCmd.Stdout = options.Streams.OutputStream
	// 	}
	// 	if streams.AttachError {
	// 		execCmd.Stderr = options.Streams.ErrorStream
	// 	}
	// }

	conmonEnv, err := r.configureConmonEnv()
	if err != nil {
		return nil, nil, fmt.Errorf("configuring conmon env: %w", err)
	}

	execCmd.ExtraFiles = extraFiles

	// we don't want to step on users fds they asked to preserve
	// Since 0-2 are used for stdio, start the fds we pass in at preserveFDs+3
	execCmd.Env = r.conmonEnv
	execCmd.Env = append(execCmd.Env, fmt.Sprintf("_OCI_SYNCPIPE=%d", preserveFDs+3), fmt.Sprintf("_OCI_STARTPIPE=%d", preserveFDs+4), fmt.Sprintf("_OCI_ATTACHPIPE=%d", preserveFDs+5))
	execCmd.Env = append(execCmd.Env, conmonEnv...)

	execCmd.ExtraFiles = append(execCmd.ExtraFiles, childSyncPipe, childStartPipe, childAttachPipe)
	execCmd.Dir = c.execBundlePath(sessionID)
	execCmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	err = execCmd.Start()

	// We don't need children pipes  on the parent side
	errorhandling.CloseQuiet(childSyncPipe)
	errorhandling.CloseQuiet(childAttachPipe)
	errorhandling.CloseQuiet(childStartPipe)
	childrenClosed = true

	if err != nil {
		return nil, nil, fmt.Errorf("cannot start container %s: %w", c.ID(), err)
	}
	if err := r.moveConmonToCgroupAndSignal(c, execCmd, parentStartPipe); err != nil {
		return nil, nil, err
	}

	// These fds were passed down to the runtime.  Close them
	// and not interfere
	for _, f := range filesToClose {
		errorhandling.CloseQuiet(f)
	}

	return execCmd, pipes, nil
}

// Attach to a container over HTTP
func attachExecHTTP(c *Container, sessionID string, r *http.Request, w http.ResponseWriter, streams *HTTPAttachStreams, pipes *execPipes, detachKeys []byte, isTerminal bool, cancel <-chan bool, hijackDone chan<- bool, holdConnOpen <-chan bool, execCmd *exec.Cmd, conmonPipeDataChan chan<- conmonPipeData, ociLog string, newSize *resize.TerminalSize, runtimeName string) (deferredErr error) {
	// NOTE: As you may notice, the attach code is quite complex.
	// Many things happen concurrently and yet are interdependent.
	// If you ever change this function, make sure to write to the
	// conmonPipeDataChan in case of an error.

	if pipes == nil || pipes.startPipe == nil || pipes.attachPipe == nil {
		err := fmt.Errorf("must provide a start and attach pipe to finish an exec attach: %w", define.ErrInvalidArg)
		conmonPipeDataChan <- conmonPipeData{-1, err}
		return err
	}

	defer func() {
		if !pipes.startClosed {
			errorhandling.CloseQuiet(pipes.startPipe)
			pipes.startClosed = true
		}
		if !pipes.attachClosed {
			errorhandling.CloseQuiet(pipes.attachPipe)
			pipes.attachClosed = true
		}
	}()

	logrus.Debugf("Attaching to container %s exec session %s", c.ID(), sessionID)

	// set up the socket path, such that it is the correct length and location for exec
	sockPath, err := c.execAttachSocketPath(sessionID)
	if err != nil {
		conmonPipeDataChan <- conmonPipeData{-1, err}
		return err
	}

	// 2: read from attachFd that the parent process has set up the console socket
	if _, err := readConmonPipeData(runtimeName, pipes.attachPipe, ""); err != nil {
		conmonPipeDataChan <- conmonPipeData{-1, err}
		return err
	}

	// resize before we start the container process
	if newSize != nil {
		err = c.ociRuntime.ExecAttachResize(c, sessionID, *newSize)
		if err != nil {
			logrus.Warnf("Resize failed: %v", err)
		}
	}

	// 2: then attach
	conn, err := openUnixSocket(sockPath)
	if err != nil {
		conmonPipeDataChan <- conmonPipeData{-1, err}
		return fmt.Errorf("failed to connect to container's attach socket: %v: %w", sockPath, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			logrus.Errorf("Unable to close socket: %q", err)
		}
	}()

	attachStdout := true
	attachStderr := true
	attachStdin := true
	if streams != nil {
		attachStdout = streams.Stdout
		attachStderr = streams.Stderr
		attachStdin = streams.Stdin
	}

	// Perform hijack
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		conmonPipeDataChan <- conmonPipeData{-1, err}
		return errors.New("unable to hijack connection")
	}

	httpCon, httpBuf, err := hijacker.Hijack()
	if err != nil {
		conmonPipeDataChan <- conmonPipeData{-1, err}
		return fmt.Errorf("hijacking connection: %w", err)
	}

	hijackDone <- true

	// Write a header to let the client know what happened
	writeHijackHeader(r, httpBuf, isTerminal)

	// Force a flush after the header is written.
	if err := httpBuf.Flush(); err != nil {
		conmonPipeDataChan <- conmonPipeData{-1, err}
		return fmt.Errorf("flushing HTTP hijack header: %w", err)
	}

	go func() {
		// Wait for conmon to succeed, when return.
		if err := execCmd.Wait(); err != nil {
			conmonPipeDataChan <- conmonPipeData{-1, err}
		} else {
			pid, err := readConmonPipeData(runtimeName, pipes.syncPipe, ociLog)
			if err != nil {
				hijackWriteError(err, c.ID(), isTerminal, httpBuf)
				conmonPipeDataChan <- conmonPipeData{pid, err}
			} else {
				conmonPipeDataChan <- conmonPipeData{pid, err}
			}
		}
		// We need to hold the connection open until the complete exec
		// function has finished. This channel will be closed in a defer
		// in that function, so we can wait for it here.
		// Can't be a defer, because this would block the function from
		// returning.
		<-holdConnOpen
		hijackWriteErrorAndClose(deferredErr, c.ID(), isTerminal, httpCon, httpBuf)
	}()

	stdoutChan := make(chan error)
	stdinChan := make(chan error)

	// Next, STDIN. Avoid entirely if attachStdin unset.
	if attachStdin {
		go func() {
			logrus.Debugf("Beginning STDIN copy")
			_, err := detach.Copy(conn, httpBuf, detachKeys)
			logrus.Debugf("STDIN copy completed")
			stdinChan <- err
		}()
	}

	// 4: send start message to child
	if err := writeConmonPipeData(pipes.startPipe); err != nil {
		return err
	}

	// Handle STDOUT/STDERR *after* start message is sent
	go func() {
		var err error
		if isTerminal {
			// Hack: return immediately if attachStdout not set to
			// emulate Docker.
			// Basically, when terminal is set, STDERR goes nowhere.
			// Everything does over STDOUT.
			// Therefore, if not attaching STDOUT - we'll never copy
			// anything from here.
			logrus.Debugf("Performing terminal HTTP attach for container %s", c.ID())
			if attachStdout {
				err = httpAttachTerminalCopy(conn, httpBuf, c.ID())
			}
		} else {
			logrus.Debugf("Performing non-terminal HTTP attach for container %s", c.ID())
			err = httpAttachNonTerminalCopy(conn, httpBuf, c.ID(), attachStdin, attachStdout, attachStderr)
		}
		stdoutChan <- err
		logrus.Debugf("STDOUT/ERR copy completed")
	}()

	for {
		select {
		case err := <-stdoutChan:
			if err != nil {
				return err
			}

			return nil
		case err := <-stdinChan:
			if err != nil {
				return err
			}
			// copy stdin is done, close it
			if connErr := socketCloseWrite(conn); connErr != nil {
				logrus.Errorf("Unable to close conn: %v", connErr)
			}
		case <-cancel:
			return nil
		}
	}
}

// prepareProcessExec returns the path of the process.json used in runc exec -p
// caller is responsible to close the returned *os.File if needed.
func (c *Container) prepareProcessExec(options *ExecOptions, env []string, sessionID string) (*os.File, error) {
	f, err := os.CreateTemp(c.execBundlePath(sessionID), "exec-process-")
	if err != nil {
		return nil, err
	}
	pspec := new(spec.Process)
	if err := JSONDeepCopy(c.config.Spec.Process, pspec); err != nil {
		return nil, err
	}
	pspec.SelinuxLabel = c.config.ProcessLabel
	pspec.Args = options.Cmd

	// We need to default this to false else it will inherit terminal as true
	// from the container.
	pspec.Terminal = false
	if options.Terminal {
		pspec.Terminal = true
	}
	if len(env) > 0 {
		pspec.Env = append(pspec.Env, env...)
	}

	// Add secret envs if they exist
	manager, err := c.runtime.SecretsManager()
	if err != nil {
		return nil, err
	}
	for name, secr := range c.config.EnvSecrets {
		_, data, err := manager.LookupSecretData(secr.Name)
		if err != nil {
			return nil, err
		}
		pspec.Env = append(pspec.Env, fmt.Sprintf("%s=%s", name, string(data)))
	}

	if options.Cwd != "" {
		pspec.Cwd = options.Cwd
	}

	// if the user is empty, we should inherit the user that the container is currently running with
	user := options.User
	if user == "" {
		logrus.Debugf("Set user to %s", c.config.User)
		user = c.config.User
	}

	overrides := c.getUserOverrides()
	execUser, err := lookup.GetUserGroupInfo(c.state.Mountpoint, user, overrides)
	if err != nil {
		return nil, err
	}

	// The additional groups must always contain the user's primary group.
	sgids := []uint32{uint32(execUser.Gid)}

	for _, sgid := range execUser.Sgids {
		sgids = append(sgids, uint32(sgid))
	}

	// Always add the groups added through --group-add, no matter the exec UID:GID.
	if len(c.config.Groups) > 0 {
		additionalSgids, err := lookup.GetContainerGroups(c.config.Groups, c.state.Mountpoint, overrides)
		if err != nil {
			return nil, fmt.Errorf("looking up supplemental groups for container %s exec session %s: %w", c.ID(), sessionID, err)
		}
		sgids = append(sgids, additionalSgids...)
	}

	// Avoid duplicates
	slices.Sort(sgids)
	sgids = slices.Compact(sgids)

	processUser := spec.User{
		UID:            uint32(execUser.Uid),
		GID:            uint32(execUser.Gid),
		AdditionalGids: sgids,
	}
	pspec.User = processUser

	if c.config.Umask != "" {
		umask, err := c.umask()
		if err != nil {
			return nil, err
		}
		pspec.User.Umask = &umask
	}

	if err := c.setProcessCapabilitiesExec(options, user, execUser, pspec); err != nil {
		return nil, err
	}

	hasHomeSet := false
	for _, s := range pspec.Env {
		if strings.HasPrefix(s, "HOME=") {
			hasHomeSet = true
			break
		}
	}
	if !hasHomeSet {
		pspec.Env = append(pspec.Env, fmt.Sprintf("HOME=%s", execUser.Home))
	}

	processJSON, err := json.Marshal(pspec)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(f.Name(), processJSON, 0o644); err != nil {
		return nil, err
	}
	return f, nil
}
