//go:build linux || freebsd

package netavark

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

type netavarkError struct {
	exitCode int
	// Set the json key to "error" so we can directly unmarshal into this struct
	Msg string `json:"error"`
	err error
}

func (e *netavarkError) Error() string {
	ec := ""
	// only add the exit code the error message if we have at least info log level
	// the normal user does not need to care about the number
	if e.exitCode > 0 && logrus.IsLevelEnabled(logrus.InfoLevel) {
		ec = " (exit code " + strconv.Itoa(e.exitCode) + ")"
	}
	msg := "netavark" + ec
	if len(msg) > 0 {
		msg += ": " + e.Msg
	}
	if e.err != nil {
		msg += ": " + e.err.Error()
	}
	return msg
}

func (e *netavarkError) Unwrap() error {
	return e.err
}

func newNetavarkError(msg string, err error) error {
	return &netavarkError{
		Msg: msg,
		err: err,
	}
}

// Type to implement io.Writer interface
// This will write the logrus at info level.
type logrusNetavarkWriter struct{}

func (l *logrusNetavarkWriter) Write(b []byte) (int, error) {
	logrus.Info("netavark: ", string(b))
	return len(b), nil
}

// getRustLogEnv returns the RUST_LOG env var based on the current logrus level.
func getRustLogEnv() string {
	level := logrus.GetLevel().String()
	// rust env_log uses warn instead of warning
	if level == "warning" {
		level = "warn"
	}
	// the rust netlink library is very verbose
	// make sure to only log netavark logs
	return "RUST_LOG=netavark=" + level
}

// execNetavark will execute netavark with the following arguments
// It takes the path to the binary, the list of args and an interface which is
// marshaled to json and send via stdin to netavark. The result interface is
// used to marshal the netavark output into it. This can be nil.
// All errors return by this function should be of the type netavarkError
// to provide a helpful error message.
func (n *netavarkNetwork) execNetavark(args []string, needPlugin bool, stdin, result any) error {
	// set the netavark log level to the same as the podman
	env := append(os.Environ(), getRustLogEnv())
	// Netavark need access to iptables in $PATH. As it turns out debian doesn't put
	// /usr/sbin in $PATH for rootless users. This will break rootless networking completely.
	// We might break existing users and we cannot expect everyone to change their $PATH so
	// let's add /usr/sbin to $PATH ourselves.
	path := os.Getenv("PATH")
	if !strings.Contains(path, "/usr/sbin") {
		path += ":/usr/sbin"
		env = append(env, "PATH="+path)
	}
	// if we run with debug log level lets also set RUST_BACKTRACE=1 so we can get the full stack trace in case of panics
	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		env = append(env, "RUST_BACKTRACE=1")
	}
	if n.dnsBindPort != 0 {
		env = append(env, "NETAVARK_DNS_PORT="+strconv.Itoa(int(n.dnsBindPort)))
	}
	if n.firewallDriver != "" {
		env = append(env, "NETAVARK_FW="+n.firewallDriver)
	}
	return n.execBinary(n.netavarkBinary, append(n.getCommonNetavarkOptions(needPlugin), args...), stdin, result, env)
}

func (n *netavarkNetwork) execPlugin(path string, args []string, stdin, result any) error {
	return n.execBinary(path, args, stdin, result, nil)
}

func (n *netavarkNetwork) execBinary(path string, args []string, stdin, result any, env []string) error {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return newNetavarkError("failed to create stdin pipe", err)
	}
	stdinWClosed := false
	defer func() {
		stdinR.Close()
		if !stdinWClosed {
			stdinW.Close()
		}
	}()

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return newNetavarkError("failed to create stdout pipe", err)
	}
	stdoutWClosed := false
	defer func() {
		stdoutR.Close()
		if !stdoutWClosed {
			stdoutW.Close()
		}
	}()

	// connect stderr to the podman stderr for logging
	var logWriter io.Writer = os.Stderr
	if n.syslog {
		// connect logrus to stderr as well so that the logs will be written to the syslog as well
		logWriter = io.MultiWriter(logWriter, &logrusNetavarkWriter{})
	}

	cmd := exec.Command(path, args...)
	// connect the pipes to stdin and stdout
	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = logWriter
	cmd.Env = env

	err = cmd.Start()
	if err != nil {
		return newNetavarkError("failed to start process", err)
	}
	err = json.NewEncoder(stdinW).Encode(stdin)
	// we have to close stdinW so netavark gets the EOF and does not hang forever
	stdinW.Close()
	stdinWClosed = true
	if err != nil {
		return newNetavarkError("failed to encode stdin data", err)
	}

	dec := json.NewDecoder(stdoutR)

	err = cmd.Wait()
	// we have to close stdoutW so we can decode the json without hanging forever
	stdoutW.Close()
	stdoutWClosed = true
	if err != nil {
		exitError := &exec.ExitError{}
		if errors.As(err, &exitError) {
			ne := &netavarkError{}
			// lets disallow unknown fields to make sure we do not get some unexpected stuff
			dec.DisallowUnknownFields()
			// this will unmarshal the error message into the error struct
			ne.err = dec.Decode(ne)
			ne.exitCode = exitError.ExitCode()
			return ne
		}
		return newNetavarkError("unexpected failure during execution", err)
	}

	if result != nil {
		err = dec.Decode(result)
		if err != nil {
			return newNetavarkError("failed to decode result", err)
		}
	}
	return nil
}
