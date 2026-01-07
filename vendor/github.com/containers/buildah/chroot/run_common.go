//go:build linux || freebsd

package chroot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/containers/buildah/bind"
	"github.com/containers/buildah/internal/pty"
	"github.com/containers/buildah/util"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/pkg/reexec"
	"go.podman.io/storage/pkg/unshare"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const (
	// runUsingChrootCommand is a command we use as a key for reexec
	runUsingChrootCommand = "buildah-chroot-runtime"
	// runUsingChrootExec is a command we use as a key for reexec
	runUsingChrootExecCommand = "buildah-chroot-exec"
	// containersConfEnv is an environment variable that we need to pass down except for the command itself
	containersConfEnv = "CONTAINERS_CONF"
)

func init() {
	reexec.Register(runUsingChrootCommand, runUsingChrootMain)
	reexec.Register(runUsingChrootExecCommand, runUsingChrootExecMain)
	for limitName, limitNumber := range rlimitsMap {
		rlimitsReverseMap[limitNumber] = limitName
	}
}

type runUsingChrootExecSubprocOptions struct {
	Spec       *specs.Spec
	BundlePath string
	NoPivot    bool
}

// RunUsingChroot runs a chrooted process, using some of the settings from the
// passed-in spec, and using the specified bundlePath to hold temporary files,
// directories, and mountpoints.
func RunUsingChroot(spec *specs.Spec, bundlePath, homeDir string, stdin io.Reader, stdout, stderr io.Writer, noPivot bool) (err error) {
	var confwg sync.WaitGroup
	var homeFound bool
	for _, env := range spec.Process.Env {
		if strings.HasPrefix(env, "HOME=") {
			homeFound = true
			break
		}
	}
	if !homeFound {
		spec.Process.Env = append(spec.Process.Env, fmt.Sprintf("HOME=%s", homeDir))
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Write the runtime configuration, mainly for debugging.
	specbytes, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	if err = ioutils.AtomicWriteFile(filepath.Join(bundlePath, "config.json"), specbytes, 0o600); err != nil {
		return fmt.Errorf("storing runtime configuration: %w", err)
	}
	logrus.Debugf("config = %v", string(specbytes))

	// Default to using stdin/stdout/stderr if we weren't passed objects to use.
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	// Create a pipe for passing configuration down to the next process.
	preader, pwriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("creating configuration pipe: %w", err)
	}
	config, conferr := json.Marshal(runUsingChrootSubprocOptions{
		Spec:       spec,
		BundlePath: bundlePath,
		NoPivot:    noPivot,
	})
	if conferr != nil {
		return fmt.Errorf("encoding configuration for %q: %w", runUsingChrootCommand, conferr)
	}

	// Set our terminal's mode to raw, to pass handling of special
	// terminal input to the terminal in the container.
	if spec.Process.Terminal && term.IsTerminal(unix.Stdin) {
		state, err := term.MakeRaw(unix.Stdin)
		if err != nil {
			logrus.Warnf("error setting terminal state: %v", err)
		} else {
			defer func() {
				if err = term.Restore(unix.Stdin, state); err != nil {
					logrus.Errorf("unable to restore terminal state: %v", err)
				}
			}()
		}
	}

	// Raise any resource limits that are higher than they are now, before
	// we drop any more privileges.
	if err = setRlimits(spec, false, true); err != nil {
		return err
	}

	// Start the grandparent subprocess.
	cmd := unshare.Command(runUsingChrootCommand)
	setPdeathsig(cmd.Cmd)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	cmd.Dir = "/"
	cmd.Env = []string{fmt.Sprintf("LOGLEVEL=%d", logrus.GetLevel())}
	if _, ok := os.LookupEnv(containersConfEnv); ok {
		cmd.Env = append(cmd.Env, containersConfEnv+"="+os.Getenv(containersConfEnv))
	}

	interrupted := make(chan os.Signal, 100)
	cmd.Hook = func(int) error {
		signal.Notify(interrupted, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			for receivedSignal := range interrupted {
				if err := cmd.Process.Signal(receivedSignal); err != nil {
					logrus.Infof("%v while attempting to forward %v to child process", err, receivedSignal)
				}
			}
		}()
		return nil
	}

	logrus.Debugf("Running %#v in %#v", cmd.Cmd, cmd)
	confwg.Add(1)
	go func() {
		_, conferr = io.Copy(pwriter, bytes.NewReader(config))
		pwriter.Close()
		confwg.Done()
	}()
	cmd.ExtraFiles = append([]*os.File{preader}, cmd.ExtraFiles...)
	err = cmd.Run()
	confwg.Wait()
	signal.Stop(interrupted)
	close(interrupted)
	if err == nil {
		return conferr
	}
	return err
}

// main() for grandparent subprocess.  Its main job is to shuttle stdio back
// and forth, managing a pseudo-terminal if we want one, for our child, the
// parent subprocess.
func runUsingChrootMain() {
	var options runUsingChrootSubprocOptions

	runtime.LockOSThread()

	// Set logging.
	if level := os.Getenv("LOGLEVEL"); level != "" {
		if ll, err := strconv.Atoi(level); err == nil {
			logrus.SetLevel(logrus.Level(ll))
		}
		os.Unsetenv("LOGLEVEL")
	}

	// Unpack our configuration.
	confPipe := os.NewFile(3, "confpipe")
	if confPipe == nil {
		fmt.Fprintf(os.Stderr, "error reading options pipe\n")
		os.Exit(1)
	}
	defer confPipe.Close()
	if err := json.NewDecoder(confPipe).Decode(&options); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding options: %v\n", err)
		os.Exit(1)
	}

	if options.Spec == nil || options.Spec.Process == nil {
		fmt.Fprintf(os.Stderr, "invalid options spec in runUsingChrootMain\n")
		os.Exit(1)
	}
	noPivot := options.NoPivot

	// Prepare to shuttle stdio back and forth.
	rootUID32, rootGID32, err := util.GetHostRootIDs(options.Spec)
	if err != nil {
		logrus.Errorf("error determining ownership for container stdio")
		os.Exit(1)
	}
	rootUID := int(rootUID32)
	rootGID := int(rootGID32)
	relays := make(map[int]int)
	closeOnceRunning := []*os.File{}
	var ctty *os.File
	var stdin io.Reader
	var stdinCopy io.WriteCloser
	var stdout io.Writer
	var stderr io.Writer
	fdDesc := make(map[int]string)
	if options.Spec.Process.Terminal {
		ptyMasterFd, ptyFd, err := pty.GetPtyDescriptors()
		if err != nil {
			logrus.Errorf("error opening PTY descriptors: %v", err)
			os.Exit(1)
		}
		// Make notes about what's going where.
		relays[ptyMasterFd] = unix.Stdout
		relays[unix.Stdin] = ptyMasterFd
		fdDesc[ptyMasterFd] = "container terminal"
		fdDesc[unix.Stdin] = "stdin"
		fdDesc[unix.Stdout] = "stdout"
		winsize := &unix.Winsize{}
		// Set the pseudoterminal's size to the configured size, or our own.
		if options.Spec.Process.ConsoleSize != nil {
			// Use configured sizes.
			winsize.Row = uint16(options.Spec.Process.ConsoleSize.Height)
			winsize.Col = uint16(options.Spec.Process.ConsoleSize.Width)
		} else {
			if term.IsTerminal(unix.Stdin) {
				// Use the size of our terminal.
				winsize, err = unix.IoctlGetWinsize(unix.Stdin, unix.TIOCGWINSZ)
				if err != nil {
					logrus.Debugf("error reading current terminal's size")
					winsize.Row = 0
					winsize.Col = 0
				}
			}
		}
		if winsize.Row != 0 && winsize.Col != 0 {
			if err = unix.IoctlSetWinsize(ptyFd, unix.TIOCSWINSZ, winsize); err != nil {
				logrus.Warnf("error setting terminal size for pty")
			}
			// FIXME - if we're connected to a terminal, we should
			// be passing the updated terminal size down when we
			// receive a SIGWINCH.
		}
		// Open an *os.File object that we can pass to our child.
		ctty = os.NewFile(uintptr(ptyFd), "/dev/tty")
		// Set ownership for the PTY.
		if err = ctty.Chown(rootUID, rootGID); err != nil {
			var cttyInfo unix.Stat_t
			err2 := unix.Fstat(ptyFd, &cttyInfo)
			from := ""
			op := "setting"
			if err2 == nil {
				op = "changing"
				from = fmt.Sprintf("from %d/%d ", cttyInfo.Uid, cttyInfo.Gid)
			}
			logrus.Warnf("error %s ownership of container PTY %sto %d/%d: %v", op, from, rootUID, rootGID, err)
		}
		// Set permissions on the PTY.
		if err = ctty.Chmod(0o620); err != nil {
			logrus.Errorf("error setting permissions of container PTY: %v", err)
			os.Exit(1)
		}
		// Make a note that our child (the parent subprocess) should
		// have the PTY connected to its stdio, and that we should
		// close it once it's running.
		stdin = ctty
		stdout = ctty
		stderr = ctty
		closeOnceRunning = append(closeOnceRunning, ctty)
	} else {
		// Create pipes for stdio.
		stdinRead, stdinWrite, err := os.Pipe()
		if err != nil {
			logrus.Errorf("error opening pipe for stdin: %v", err)
		}
		stdoutRead, stdoutWrite, err := os.Pipe()
		if err != nil {
			logrus.Errorf("error opening pipe for stdout: %v", err)
		}
		stderrRead, stderrWrite, err := os.Pipe()
		if err != nil {
			logrus.Errorf("error opening pipe for stderr: %v", err)
		}
		// Make notes about what's going where.
		relays[unix.Stdin] = int(stdinWrite.Fd())
		relays[int(stdoutRead.Fd())] = unix.Stdout
		relays[int(stderrRead.Fd())] = unix.Stderr
		fdDesc[int(stdinWrite.Fd())] = "container stdin pipe"
		fdDesc[int(stdoutRead.Fd())] = "container stdout pipe"
		fdDesc[int(stderrRead.Fd())] = "container stderr pipe"
		fdDesc[unix.Stdin] = "stdin"
		fdDesc[unix.Stdout] = "stdout"
		fdDesc[unix.Stderr] = "stderr"
		// Set ownership for the pipes.
		if err = stdinRead.Chown(rootUID, rootGID); err != nil {
			logrus.Errorf("error setting ownership of container stdin pipe: %v", err)
			os.Exit(1)
		}
		if err = stdoutWrite.Chown(rootUID, rootGID); err != nil {
			logrus.Errorf("error setting ownership of container stdout pipe: %v", err)
			os.Exit(1)
		}
		if err = stderrWrite.Chown(rootUID, rootGID); err != nil {
			logrus.Errorf("error setting ownership of container stderr pipe: %v", err)
			os.Exit(1)
		}
		// Make a note that our child (the parent subprocess) should
		// have the pipes connected to its stdio, and that we should
		// close its ends of them once it's running.
		stdin = stdinRead
		stdout = stdoutWrite
		stderr = stderrWrite
		closeOnceRunning = append(closeOnceRunning, stdinRead, stdoutWrite, stderrWrite)
		stdinCopy = stdinWrite
		defer stdoutRead.Close()
		defer stderrRead.Close()
	}
	for readFd, writeFd := range relays {
		if err := unix.SetNonblock(readFd, true); err != nil {
			logrus.Errorf("error setting descriptor %d (%s) non-blocking: %v", readFd, fdDesc[readFd], err)
			return
		}
		if err := unix.SetNonblock(writeFd, false); err != nil {
			logrus.Errorf("error setting descriptor %d (%s) blocking: %v", relays[writeFd], fdDesc[writeFd], err)
			return
		}
	}
	if err := unix.SetNonblock(relays[unix.Stdin], true); err != nil {
		logrus.Errorf("error setting %d to nonblocking: %v", relays[unix.Stdin], err)
	}
	go func() {
		buffers := make(map[int]*bytes.Buffer)
		for _, writeFd := range relays {
			buffers[writeFd] = new(bytes.Buffer)
		}
		pollTimeout := -1
		stdinClose := false
		for len(relays) > 0 {
			fds := make([]unix.PollFd, 0, len(relays))
			for fd := range relays {
				fds = append(fds, unix.PollFd{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP})
			}
			_, err := unix.Poll(fds, pollTimeout)
			if !util.LogIfNotRetryable(err, fmt.Sprintf("poll: %v", err)) {
				return
			}
			removeFds := make(map[int]struct{})
			for _, rfd := range fds {
				if rfd.Revents&unix.POLLHUP == unix.POLLHUP {
					removeFds[int(rfd.Fd)] = struct{}{}
				}
				if rfd.Revents&unix.POLLNVAL == unix.POLLNVAL {
					logrus.Debugf("error polling descriptor %s: closed?", fdDesc[int(rfd.Fd)])
					removeFds[int(rfd.Fd)] = struct{}{}
				}
				if rfd.Revents&unix.POLLIN == 0 {
					if stdinClose && stdinCopy == nil {
						continue
					}
					continue
				}
				b := make([]byte, 8192)
				nread, err := unix.Read(int(rfd.Fd), b)
				util.LogIfNotRetryable(err, fmt.Sprintf("read %s: %v", fdDesc[int(rfd.Fd)], err))
				if nread > 0 {
					if wfd, ok := relays[int(rfd.Fd)]; ok {
						nwritten, err := buffers[wfd].Write(b[:nread])
						if err != nil {
							logrus.Debugf("buffer: %v", err)
							continue
						}
						if nwritten != nread {
							logrus.Debugf("buffer: expected to buffer %d bytes, wrote %d", nread, nwritten)
							continue
						}
					}
					// If this is the last of the data we'll be able to read
					// from this descriptor, read as much as there is to read.
					for rfd.Revents&unix.POLLHUP == unix.POLLHUP {
						nr, err := unix.Read(int(rfd.Fd), b)
						util.LogIfUnexpectedWhileDraining(err, fmt.Sprintf("read %s: %v", fdDesc[int(rfd.Fd)], err))
						if nr <= 0 {
							break
						}
						if wfd, ok := relays[int(rfd.Fd)]; ok {
							nwritten, err := buffers[wfd].Write(b[:nr])
							if err != nil {
								logrus.Debugf("buffer: %v", err)
								break
							}
							if nwritten != nr {
								logrus.Debugf("buffer: expected to buffer %d bytes, wrote %d", nr, nwritten)
								break
							}
						}
					}
				}
				if nread == 0 {
					removeFds[int(rfd.Fd)] = struct{}{}
				}
			}
			pollTimeout = -1
			for wfd, buffer := range buffers {
				if buffer.Len() > 0 {
					nwritten, err := unix.Write(wfd, buffer.Bytes())
					util.LogIfNotRetryable(err, fmt.Sprintf("write %s: %v", fdDesc[wfd], err))
					if nwritten >= 0 {
						_ = buffer.Next(nwritten)
					}
				}
				if buffer.Len() > 0 {
					pollTimeout = 100
				}
				if wfd == relays[unix.Stdin] && stdinClose && buffer.Len() == 0 {
					stdinCopy.Close()
					delete(relays, unix.Stdin)
				}
			}
			for rfd := range removeFds {
				if rfd == unix.Stdin {
					buffer, found := buffers[relays[unix.Stdin]]
					if found && buffer.Len() > 0 {
						stdinClose = true
						continue
					}
				}
				if !options.Spec.Process.Terminal && rfd == unix.Stdin {
					stdinCopy.Close()
				}
				delete(relays, rfd)
			}
		}
	}()

	// Set up mounts and namespaces, and run the parent subprocess.
	status, err := runUsingChroot(options.Spec, options.BundlePath, ctty, stdin, stdout, stderr, noPivot, closeOnceRunning)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running subprocess: %v\n", err)
		os.Exit(1)
	}

	// Pass the process's exit status back to the caller by exiting with the same status.
	if status.Exited() {
		if status.ExitStatus() != 0 {
			fmt.Fprintf(os.Stderr, "subprocess exited with status %d\n", status.ExitStatus())
		}
		os.Exit(status.ExitStatus())
	} else if status.Signaled() {
		fmt.Fprintf(os.Stderr, "subprocess exited on %s\n", status.Signal())
		os.Exit(1)
	}
}

// runUsingChroot, still in the grandparent process, sets up various bind
// mounts and then runs the parent process in its own user namespace with the
// necessary ID mappings.
func runUsingChroot(spec *specs.Spec, bundlePath string, ctty *os.File, stdin io.Reader, stdout, stderr io.Writer, noPivot bool, closeOnceRunning []*os.File) (wstatus unix.WaitStatus, err error) {
	var confwg sync.WaitGroup

	// Create a new mount namespace for ourselves and bind mount everything to a new location.
	undoIntermediates, err := bind.SetupIntermediateMountNamespace(spec, bundlePath)
	if err != nil {
		return 1, err
	}
	defer func() {
		if undoErr := undoIntermediates(); undoErr != nil {
			logrus.Debugf("error cleaning up intermediate mount NS: %v", err)
		}
	}()

	// Bind mount in our filesystems.
	undoChroots, err := setupChrootBindMounts(spec, bundlePath)
	if err != nil {
		return 1, err
	}
	defer func() {
		if undoErr := undoChroots(); undoErr != nil {
			logrus.Debugf("error cleaning up intermediate chroot bind mounts: %v", err)
		}
	}()

	// Create a pipe for passing configuration down to the next process.
	preader, pwriter, err := os.Pipe()
	if err != nil {
		return 1, fmt.Errorf("creating configuration pipe: %w", err)
	}
	config, conferr := json.Marshal(runUsingChrootExecSubprocOptions{
		Spec:       spec,
		BundlePath: bundlePath,
		NoPivot:    noPivot,
	})
	if conferr != nil {
		fmt.Fprintf(os.Stderr, "error re-encoding configuration for %q\n", runUsingChrootExecCommand)
		os.Exit(1)
	}

	// Apologize for the namespace configuration that we're about to ignore.
	logNamespaceDiagnostics(spec)

	// We need to lock the thread so that PR_SET_PDEATHSIG won't trigger if the current thread exits.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Start the parent subprocess.
	cmd := unshare.Command(append([]string{runUsingChrootExecCommand}, spec.Process.Args...)...)
	setPdeathsig(cmd.Cmd)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	cmd.Dir = "/"
	cmd.Env = []string{fmt.Sprintf("LOGLEVEL=%d", logrus.GetLevel())}
	if _, ok := os.LookupEnv(containersConfEnv); ok {
		cmd.Env = append(cmd.Env, containersConfEnv+"="+os.Getenv(containersConfEnv))
	}
	if ctty != nil {
		cmd.Setsid = true
		cmd.Ctty = ctty
	}
	cmd.ExtraFiles = append([]*os.File{preader}, cmd.ExtraFiles...)
	if err := setPlatformUnshareOptions(spec, cmd); err != nil {
		return 1, fmt.Errorf("setting platform unshare options: %w", err)
	}
	interrupted := make(chan os.Signal, 100)
	cmd.Hook = func(int) error {
		for _, f := range closeOnceRunning {
			f.Close()
		}
		signal.Notify(interrupted, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			for receivedSignal := range interrupted {
				if err := cmd.Process.Signal(receivedSignal); err != nil {
					logrus.Infof("%v while attempting to forward %v to child process", err, receivedSignal)
				}
			}
		}()
		return nil
	}

	logrus.Debugf("Running %#v in %#v", cmd.Cmd, cmd)
	confwg.Add(1)
	go func() {
		_, conferr = io.Copy(pwriter, bytes.NewReader(config))
		pwriter.Close()
		confwg.Done()
	}()
	err = cmd.Run()
	confwg.Wait()
	signal.Stop(interrupted)
	close(interrupted)
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if waitStatus, ok := exitError.ProcessState.Sys().(syscall.WaitStatus); ok {
				if waitStatus.Exited() {
					if waitStatus.ExitStatus() != 0 {
						fmt.Fprintf(os.Stderr, "subprocess exited with status %d\n", waitStatus.ExitStatus())
					}
					os.Exit(waitStatus.ExitStatus())
				} else if waitStatus.Signaled() {
					fmt.Fprintf(os.Stderr, "subprocess exited on %s\n", waitStatus.Signal())
					os.Exit(1)
				}
			}
		}
		fmt.Fprintf(os.Stderr, "process exited with error: %v\n", err)
		os.Exit(1)
	}

	return 0, nil
}

// main() for parent subprocess.  Its main job is to try to make our
// environment look like the one described by the runtime configuration blob,
// and then launch the intended command as a child.
func runUsingChrootExecMain() {
	args := os.Args[1:]
	var options runUsingChrootExecSubprocOptions
	var err error

	runtime.LockOSThread()

	// Set logging.
	if level := os.Getenv("LOGLEVEL"); level != "" {
		if ll, err := strconv.Atoi(level); err == nil {
			logrus.SetLevel(logrus.Level(ll))
		}
		os.Unsetenv("LOGLEVEL")
	}

	// Unpack our configuration.
	confPipe := os.NewFile(3, "confpipe")
	if confPipe == nil {
		fmt.Fprintf(os.Stderr, "error reading options pipe\n")
		os.Exit(1)
	}
	defer confPipe.Close()
	if err := json.NewDecoder(confPipe).Decode(&options); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding options: %v\n", err)
		os.Exit(1)
	}

	// Set the hostname.  We're already in a distinct UTS namespace and are admins in the user
	// namespace which created it, so we shouldn't get a permissions error, but seccomp policy
	// might deny our attempt to call sethostname() anyway, so log a debug message for that.
	if options.Spec == nil || options.Spec.Process == nil {
		fmt.Fprintf(os.Stderr, "invalid options spec passed in\n")
		os.Exit(1)
	}

	if options.Spec.Hostname != "" {
		setContainerHostname(options.Spec.Hostname)
	}

	// Try to chroot into the root.  Do this before we potentially
	// block the syscall via the seccomp profile. Allow the
	// platform to override this - on FreeBSD, we use a simple
	// jail to set the hostname in the container, and on Linux
	// we attempt to pivot_root.
	if err := createPlatformContainer(options); err != nil {
		logrus.Debugf("createPlatformContainer: %v", err)
		var oldst, newst unix.Stat_t
		if err := unix.Stat(options.Spec.Root.Path, &oldst); err != nil {
			fmt.Fprintf(os.Stderr, "error stat()ing intended root directory %q: %v\n", options.Spec.Root.Path, err)
			os.Exit(1)
		}
		if err := unix.Chdir(options.Spec.Root.Path); err != nil {
			fmt.Fprintf(os.Stderr, "error chdir()ing to intended root directory %q: %v\n", options.Spec.Root.Path, err)
			os.Exit(1)
		}
		if err := unix.Chroot(options.Spec.Root.Path); err != nil {
			fmt.Fprintf(os.Stderr, "error chroot()ing into directory %q: %v\n", options.Spec.Root.Path, err)
			os.Exit(1)
		}
		if err := unix.Stat("/", &newst); err != nil {
			fmt.Fprintf(os.Stderr, "error stat()ing current root directory: %v\n", err)
			os.Exit(1)
		}
		if oldst.Dev != newst.Dev || oldst.Ino != newst.Ino {
			fmt.Fprintf(os.Stderr, "unknown error chroot()ing into directory %q: %v\n", options.Spec.Root.Path, err)
			os.Exit(1)
		}
		logrus.Debugf("chrooted into %q", options.Spec.Root.Path)
	}

	// not doing because it's still shared: creating devices
	// not doing because it's not applicable: setting annotations
	// not doing because it's still shared: setting sysctl settings
	// not doing because cgroupfs is read only: configuring control groups
	// -> this means we can use the freezer to make sure there aren't any lingering processes
	// -> this means we ignore cgroups-based controls
	// not doing because we don't set any in the config: running hooks
	// not doing because we don't set it in the config: setting rootfs read-only
	// not doing because we don't set it in the config: setting rootfs propagation
	logrus.Debugf("setting apparmor profile")
	if err = setApparmorProfile(options.Spec); err != nil {
		fmt.Fprintf(os.Stderr, "error setting apparmor profile for process: %v\n", err)
		os.Exit(1)
	}
	if err = setSelinuxLabel(options.Spec); err != nil {
		fmt.Fprintf(os.Stderr, "error setting SELinux label for process: %v\n", err)
		os.Exit(1)
	}

	logrus.Debugf("setting resource limits")
	if err = setRlimits(options.Spec, false, false); err != nil {
		fmt.Fprintf(os.Stderr, "error setting process resource limits for process: %v\n", err)
		os.Exit(1)
	}

	// Try to change to the directory.
	cwd := options.Spec.Process.Cwd
	if !filepath.IsAbs(cwd) {
		cwd = "/" + cwd
	}
	cwd = filepath.Clean(cwd)
	if err := unix.Chdir("/"); err != nil {
		fmt.Fprintf(os.Stderr, "error chdir()ing into new root directory %q: %v\n", options.Spec.Root.Path, err)
		os.Exit(1)
	}
	if err := unix.Chdir(cwd); err != nil {
		fmt.Fprintf(os.Stderr, "error chdir()ing into directory %q under root %q: %v\n", cwd, options.Spec.Root.Path, err)
		os.Exit(1)
	}
	logrus.Debugf("changed working directory to %q", cwd)

	// Drop privileges.
	user := options.Spec.Process.User
	if len(user.AdditionalGids) > 0 {
		gids := make([]int, len(user.AdditionalGids))
		for i := range user.AdditionalGids {
			gids[i] = int(user.AdditionalGids[i])
		}
		logrus.Debugf("setting supplemental groups")
		if err = syscall.Setgroups(gids); err != nil {
			fmt.Fprintf(os.Stderr, "error setting supplemental groups list: %v\n", err)
			os.Exit(1)
		}
	} else {
		setgroups, _ := os.ReadFile("/proc/self/setgroups")
		if strings.Trim(string(setgroups), "\n") != "deny" {
			logrus.Debugf("clearing supplemental groups")
			if err = syscall.Setgroups([]int{}); err != nil {
				fmt.Fprintf(os.Stderr, "error clearing supplemental groups list: %v\n", err)
				os.Exit(1)
			}
		}
	}

	logrus.Debugf("setting gid")
	if err = unix.Setresgid(int(user.GID), int(user.GID), int(user.GID)); err != nil {
		fmt.Fprintf(os.Stderr, "error setting GID: %v\n", err)
		os.Exit(1)
	}

	if err = setSeccomp(options.Spec); err != nil {
		fmt.Fprintf(os.Stderr, "error setting seccomp filter for process: %v\n", err)
		os.Exit(1)
	}

	logrus.Debugf("setting capabilities")
	var keepCaps []string
	if user.UID != 0 {
		keepCaps = []string{"CAP_SETUID"}
	}
	if err := setCapabilities(options.Spec, keepCaps...); err != nil {
		fmt.Fprintf(os.Stderr, "error setting capabilities for process: %v\n", err)
		os.Exit(1)
	}

	logrus.Debugf("setting uid")
	if err = unix.Setresuid(int(user.UID), int(user.UID), int(user.UID)); err != nil {
		fmt.Fprintf(os.Stderr, "error setting UID: %v\n", err)
		os.Exit(1)
	}

	// Set $PATH to the value for the container, so that when args[0] is not an absolute path,
	// exec.Command() can find it using exec.LookPath().
	for _, env := range slices.Backward(options.Spec.Process.Env) {
		if val, ok := strings.CutPrefix(env, "PATH="); ok {
			os.Setenv("PATH", val)
			break
		}
	}

	// Actually run the specified command.
	cmd := exec.Command(args[0], args[1:]...)
	setPdeathsig(cmd)
	cmd.Env = options.Spec.Process.Env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Dir = cwd
	logrus.Debugf("Running %#v (PATH = %q)", cmd, os.Getenv("PATH"))
	interrupted := make(chan os.Signal, 100)
	if err = cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "process failed to start with error: %v\n", err)
	}
	go func() {
		for range interrupted {
			if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
				logrus.Infof("%v while attempting to send SIGKILL to child process", err)
			}
		}
	}()
	signal.Notify(interrupted, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	err = cmd.Wait()
	signal.Stop(interrupted)
	close(interrupted)
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if waitStatus, ok := exitError.ProcessState.Sys().(syscall.WaitStatus); ok {
				if waitStatus.Exited() {
					if waitStatus.ExitStatus() != 0 {
						fmt.Fprintf(os.Stderr, "subprocess exited with status %d\n", waitStatus.ExitStatus())
					}
					os.Exit(waitStatus.ExitStatus())
				} else if waitStatus.Signaled() {
					fmt.Fprintf(os.Stderr, "subprocess exited on %s\n", waitStatus.Signal())
					os.Exit(1)
				}
			}
		}
		fmt.Fprintf(os.Stderr, "process exited with error: %v\n", err)
		os.Exit(1)
	}
}

// parses the resource limits for ourselves and any processes that
// we'll start into a format that's more in line with the kernel APIs
func parseRlimits(spec *specs.Spec) (map[int]unix.Rlimit, error) {
	if spec.Process == nil {
		return nil, nil
	}
	parsed := make(map[int]unix.Rlimit)
	for _, limit := range spec.Process.Rlimits {
		resource, recognized := rlimitsMap[strings.ToUpper(limit.Type)]
		if !recognized {
			return nil, fmt.Errorf("parsing limit type %q", limit.Type)
		}
		parsed[resource] = makeRlimit(limit)
	}
	return parsed, nil
}

// setRlimits sets any resource limits that we want to apply to processes that
// we'll start.
func setRlimits(spec *specs.Spec, onlyLower, onlyRaise bool) error {
	limits, err := parseRlimits(spec)
	if err != nil {
		return err
	}
	for resource, desired := range limits {
		var current unix.Rlimit
		if err := unix.Getrlimit(resource, &current); err != nil {
			return fmt.Errorf("reading %q limit: %w", rlimitsReverseMap[resource], err)
		}
		if desired.Max > current.Max && onlyLower {
			// this would raise a hard limit, and we're only here to lower them
			continue
		}
		if desired.Max < current.Max && onlyRaise {
			// this would lower a hard limit, and we're only here to raise them
			continue
		}
		if err := unix.Setrlimit(resource, &desired); err != nil {
			return fmt.Errorf("setting %q limit to soft=%d,hard=%d (was soft=%d,hard=%d): %w", rlimitsReverseMap[resource], desired.Cur, desired.Max, current.Cur, current.Max, err)
		}
	}
	return nil
}

func isDevNull(dev os.FileInfo) bool {
	if dev.Mode()&os.ModeCharDevice != 0 {
		stat, _ := dev.Sys().(*syscall.Stat_t)
		nullStat := syscall.Stat_t{}
		if err := syscall.Stat(os.DevNull, &nullStat); err != nil {
			logrus.Warnf("unable to stat /dev/null: %v", err)
			return false
		}
		if stat.Rdev == nullStat.Rdev {
			return true
		}
	}
	return false
}
