package define

import (
	"errors"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	// ExecErrorCodeGeneric is the default error code to return from an exec session if libpod failed
	// prior to calling the runtime
	ExecErrorCodeGeneric = 125
	// ExecErrorCodeCannotInvoke is the error code to return when the runtime fails to invoke a command.
	// An example of this can be found by trying to execute a directory:
	// `podman exec -l /etc`
	ExecErrorCodeCannotInvoke = 126
	// ExecErrorCodeNotFound is the error code to return when a command cannot be found
	ExecErrorCodeNotFound = 127
)

// TranslateExecErrorToExitCode takes an error and checks whether it
// has a predefined exit code associated. If so, it returns that, otherwise it returns
// the exit code originally stated in libpod.Exec()
func TranslateExecErrorToExitCode(originalEC int, err error) int {
	if errors.Is(err, ErrOCIRuntimePermissionDenied) {
		return ExecErrorCodeCannotInvoke
	}
	if errors.Is(err, ErrOCIRuntimeNotFound) {
		return ExecErrorCodeNotFound
	}
	if errors.Is(err, ErrInvalidArg) {
		return ExecErrorCodeGeneric
	}

	return originalEC
}

// ExitCode reads the error message when failing to executing container process
// and then returns 0 if no error, ExecErrorCodeNotFound if command does not exist, or ExecErrorCodeCannotInvoke for
// all other errors
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	e := strings.ToLower(err.Error())
	logrus.Debugf("ExitCode msg: %q", e)
	if strings.Contains(e, "not found") ||
		strings.Contains(e, "executable path is empty") ||
		strings.Contains(e, "no such file") {
		return ExecErrorCodeNotFound
	}

	return ExecErrorCodeCannotInvoke
}
