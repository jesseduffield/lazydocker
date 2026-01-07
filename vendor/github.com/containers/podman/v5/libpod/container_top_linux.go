//go:build !remote && linux && cgo

package libpod

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/rootless"
	"github.com/containers/psgo"
	"github.com/google/shlex"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/reexec"
	"golang.org/x/sys/unix"
)

/*
#include <stdlib.h>
void fork_exec_ps();
void create_argv(int len);
void set_argv(int pos, char *arg);
void set_userns();
*/
import "C"

const (
	// podmanTopCommand is the reexec key to safely setup the environment for ps to be executed
	podmanTopCommand = "podman-top"

	// podmanTopExitCode is a special exec code to signal that podman failed to to something in
	// reexec command not ps. This is used to give a better error.
	podmanTopExitCode = 255
)

func init() {
	reexec.Register(podmanTopCommand, podmanTopMain)
}

// podmanTopMain - main function for the reexec
func podmanTopMain() {
	if err := podmanTopInner(); err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(podmanTopExitCode)
	}
	os.Exit(0)
}

// podmanTopInner os.Args = {command name} {pid} {userns(1/0)} {psPath} [args...]
// We are rexxec'd in a new mountns, then we need to set some security settings in order
// to safely execute ps in the container pid namespace. Most notably make sure podman and
// ps are read only to prevent a process from overwriting it.
func podmanTopInner() error {
	if len(os.Args) < 4 {
		return fmt.Errorf("internal error, need at least three arguments")
	}

	// We have to lock the thread as we a) switch namespace below and b) use PR_SET_PDEATHSIG
	// Also do not unlock as this thread should not be reused by go we exit anyway at the end.
	runtime.LockOSThread()

	if err := unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0); err != nil {
		return fmt.Errorf("PR_SET_PDEATHSIG: %w", err)
	}
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("PR_SET_DUMPABLE: %w", err)
	}

	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("PR_SET_NO_NEW_PRIVS: %w", err)
	}

	if err := unix.Mount("none", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make / mount private: %w", err)
	}

	psPath := os.Args[3]

	// try to mount everything read only
	if err := unix.MountSetattr(0, "/", unix.AT_RECURSIVE, &unix.MountAttr{
		Attr_set: unix.MOUNT_ATTR_RDONLY,
	}); err != nil {
		if err != unix.ENOSYS {
			return fmt.Errorf("mount_setattr / readonly: %w", err)
		}
		// old kernel without mount_setattr, i.e. on RHEL 8.8
		// Bind mount the directories readonly for both podman and ps.
		psPath, err = remountReadOnly(psPath)
		if err != nil {
			return err
		}
		_, err = remountReadOnly(reexec.Self())
		if err != nil {
			return err
		}
	}

	// extra safety check make sure the ps path is actually read only
	err := unix.Access(psPath, unix.W_OK)
	if err == nil {
		return fmt.Errorf("%q was not mounted read only, this can be dangerous so we will not execute it", psPath)
	}

	pid := os.Args[1]
	// join the pid namespace of pid
	pidFD, err := os.Open(fmt.Sprintf("/proc/%s/ns/pid", pid))
	if err != nil {
		return fmt.Errorf("open pidns: %w", err)
	}
	if err := unix.Setns(int(pidFD.Fd()), unix.CLONE_NEWPID); err != nil {
		return fmt.Errorf("setns NEWPID: %w", err)
	}
	pidFD.Close()

	userns := os.Args[2]
	if userns == "1" {
		C.set_userns()
	}

	args := []string{psPath}
	args = append(args, os.Args[4:]...)

	C.create_argv(C.int(len(args)))
	for i, arg := range args {
		cArg := C.CString(arg)
		C.set_argv(C.int(i), cArg)
		defer C.free(unsafe.Pointer(cArg))
	}

	// Now try to close open fds except std streams
	// While golang open everything O_CLOEXEC it could still leak fds from
	// the parent, i.e. bash. In this case an attacker might be able to
	// read/write from them.
	// Do this as last step, it has to happen before to fork because the child
	// will be immediately in pid namespace so we cannot close them in the child.
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return err
	}
	for _, e := range entries {
		i, err := strconv.Atoi(e.Name())
		// IsFdInherited checks the we got the fd from a parent process and only close them,
		// when we close all that would include the ones from the go runtime which
		// then can panic because of that.
		if err == nil && i > unix.Stderr && rootless.IsFdInherited(i) {
			_ = unix.Close(i)
		}
	}

	// this function will always exit for us
	C.fork_exec_ps()
	return nil
}

// remountReadOnly remounts the parent directory of the given path read only
// return the resolved path or an error. The path can then be used to exec the
// binary as we know it is on a read only mount now.
func remountReadOnly(path string) (string, error) {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve symlink for %s: %w", path, err)
	}
	dir := filepath.Dir(resolvedPath)
	// create mount point
	if err := unix.Mount(dir, dir, "", unix.MS_BIND, ""); err != nil {
		return "", fmt.Errorf("mount %s read only: %w", dir, err)
	}
	// remount readonly
	if err := unix.Mount(dir, dir, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
		return "", fmt.Errorf("mount %s read only: %w", dir, err)
	}
	return resolvedPath, nil
}

// Top gathers statistics about the running processes in a container. It returns a
// []string for output
func (c *Container) Top(descriptors []string) ([]string, error) {
	if c.config.NoCgroups {
		return nil, fmt.Errorf("cannot run top on container %s as it did not create a cgroup: %w", c.ID(), define.ErrNoCgroups)
	}

	conStat, err := c.State()
	if err != nil {
		return nil, fmt.Errorf("unable to look up state for %s: %w", c.ID(), err)
	}
	if conStat != define.ContainerStateRunning {
		return nil, errors.New("top can only be used on running containers")
	}

	// Also support comma-separated input.
	psgoDescriptors := []string{}
	for _, d := range descriptors {
		for s := range strings.SplitSeq(d, ",") {
			if s != "" {
				psgoDescriptors = append(psgoDescriptors, s)
			}
		}
	}

	// If we encountered an ErrUnknownDescriptor error, fallback to executing
	// ps(1). This ensures backwards compatibility to users depending on ps(1)
	// and makes sure we're ~compatible with docker.
	output, psgoErr := c.GetContainerPidInformation(psgoDescriptors)
	if psgoErr == nil {
		return output, nil
	}
	if !errors.Is(psgoErr, psgo.ErrUnknownDescriptor) {
		return nil, psgoErr
	}

	psDescriptors := descriptors
	if len(descriptors) == 1 {
		// Note that the descriptors to ps(1) must be shlexed (see #12452).
		psDescriptors = make([]string, 0, len(descriptors))
		shSplit, err := shlex.Split(descriptors[0])
		if err != nil {
			return nil, fmt.Errorf("parsing ps args: %w", err)
		}
		for _, s := range shSplit {
			if s != "" {
				psDescriptors = append(psDescriptors, s)
			}
		}
	}

	// Only use ps(1) from the host when we know the container was not started with CAP_SYS_PTRACE,
	// with it the container can access /proc/$pid/ files and potentially escape the container fs.
	if c.config.Spec.Process.Capabilities != nil &&
		!slices.Contains(c.config.Spec.Process.Capabilities.Effective, "CAP_SYS_PTRACE") {
		var retry bool
		output, retry, err = c.execPS(psDescriptors)
		if err != nil {
			if !retry {
				return nil, err
			}
			logrus.Warnf("Falling back to container ps(1), could not execute ps(1) from the host: %v", err)
			output, err = c.execPSinContainer(psDescriptors)
			if err != nil {
				return nil, fmt.Errorf("executing ps(1) in container: %w", err)
			}
		}
	} else {
		output, err = c.execPSinContainer(psDescriptors)
		if err != nil {
			return nil, fmt.Errorf("executing ps(1) in container: %w", err)
		}
	}

	// Trick: filter the ps command from the output instead of
	// checking/requiring PIDs in the output.
	filtered := []string{}
	cmd := strings.Join(descriptors, " ")
	for _, line := range output {
		if !strings.Contains(line, cmd) {
			filtered = append(filtered, line)
		}
	}

	return filtered, nil
}

// GetContainerPidInformation returns process-related data of all processes in
// the container.  The output data can be controlled via the `descriptors`
// argument which expects format descriptors and supports all AIXformat
// descriptors of ps (1) plus some additional ones to for instance inspect the
// set of effective capabilities.  Each element in the returned string slice
// is a tab-separated string.
//
// For more details, please refer to github.com/containers/psgo.
func (c *Container) GetContainerPidInformation(descriptors []string) ([]string, error) {
	pid := strconv.Itoa(c.state.PID)
	// NOTE: psgo returns a [][]string to give users the ability to apply
	//       filters on the data.  We need to change the API here
	//       to return a [][]string if we want to make use of
	//       filtering.
	opts := psgo.JoinNamespaceOpts{FillMappings: rootless.IsRootless()}

	psgoOutput, err := psgo.JoinNamespaceAndProcessInfoWithOptions(pid, descriptors, &opts)
	if err != nil {
		return nil, err
	}
	res := []string{}
	for _, out := range psgoOutput {
		res = append(res, strings.Join(out, "\t"))
	}
	return res, nil
}

// execute ps(1) from the host within the container pid namespace
func (c *Container) execPS(psArgs []string) ([]string, bool, error) {
	rPipe, wPipe, err := os.Pipe()
	if err != nil {
		return nil, false, err
	}
	defer rPipe.Close()

	outErrChan := make(chan error)
	stdout := []string{}
	go func() {
		defer close(outErrChan)
		scanner := bufio.NewScanner(rPipe)
		for scanner.Scan() {
			stdout = append(stdout, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			outErrChan <- err
		}
	}()

	psPath, err := exec.LookPath("ps")
	if err != nil {
		wPipe.Close()
		return nil, true, err
	}

	// see podmanTopInner()
	userns := "0"
	if len(c.config.IDMappings.UIDMap) > 0 {
		userns = "1"
	}

	args := append([]string{podmanTopCommand, strconv.Itoa(c.state.PID), userns, psPath}, psArgs...)

	cmd := reexec.Command(args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Unshareflags: unix.CLONE_NEWNS,
	}
	var errBuf bytes.Buffer
	cmd.Stdout = wPipe
	cmd.Stderr = &errBuf
	// nil means use current env so explicitly unset all, to not leak any sensitive env vars
	cmd.Env = []string{fmt.Sprintf("HOME=%s", os.Getenv("HOME"))}

	retryContainerExec := true
	err = cmd.Run()
	wPipe.Close()
	if err != nil {
		exitError := &exec.ExitError{}
		if errors.As(err, &exitError) {
			if exitError.ExitCode() != podmanTopExitCode {
				// ps command failed
				err = fmt.Errorf("ps(1) failed with exit code %d: %s", exitError.ExitCode(), errBuf.String())
				// ps command itself failed: likely invalid args, no point in retrying.
				retryContainerExec = false
			} else {
				// podman-top reexec setup fails somewhere
				err = fmt.Errorf("could not execute ps(1) in the container pid namespace: %s", errBuf.String())
			}
		} else {
			err = fmt.Errorf("could not reexec podman-top command: %w", err)
		}
	}

	if err := <-outErrChan; err != nil {
		return nil, retryContainerExec, fmt.Errorf("failed to read ps stdout: %w", err)
	}
	return stdout, retryContainerExec, err
}

// execPS executes ps(1) with the specified args in the container via exec session.
// This should be a bit safer then execPS() but it requires ps(1) to be installed in the container.
func (c *Container) execPSinContainer(args []string) ([]string, error) {
	rPipe, wPipe, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer rPipe.Close()

	var errBuf bytes.Buffer
	streams := new(define.AttachStreams)
	streams.OutputStream = wPipe
	streams.ErrorStream = &errBuf
	streams.AttachOutput = true
	streams.AttachError = true

	outErrChan := make(chan error)
	stdout := []string{}
	go func() {
		defer close(outErrChan)
		scanner := bufio.NewScanner(rPipe)
		for scanner.Scan() {
			stdout = append(stdout, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			outErrChan <- err
		}
	}()

	cmd := append([]string{"ps"}, args...)
	config := new(ExecConfig)
	config.Command = cmd
	ec, err := c.Exec(config, streams, nil)
	wPipe.Close()
	if err != nil {
		return nil, err
	} else if ec != 0 {
		return nil, fmt.Errorf("runtime failed with exit status: %d and output: %s", ec, errBuf.String())
	}

	if logrus.GetLevel() >= logrus.DebugLevel {
		// If we're running in debug mode or higher, we might want to have a
		// look at stderr which includes debug logs from conmon.
		logrus.Debug(errBuf.String())
	}

	if err := <-outErrChan; err != nil {
		return nil, fmt.Errorf("failed to read ps stdout: %w", err)
	}
	return stdout, nil
}
