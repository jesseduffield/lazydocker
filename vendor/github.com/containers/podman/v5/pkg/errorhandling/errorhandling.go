package errorhandling

import (
	"errors"
	"os"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
)

// JoinErrors converts the error slice into a single human-readable error.
func JoinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}

	// If there's just one error, return it.  This prevents the "%d errors
	// occurred:" header plus list from the multierror package.
	if len(errs) == 1 {
		return errs[0]
	}

	// `multierror` appends new lines which we need to remove to prevent
	// blank lines when printing the error.
	var multiE *multierror.Error
	multiE = multierror.Append(multiE, errs...)

	finalErr := multiE.ErrorOrNil()
	if finalErr == nil {
		return nil
	}
	return errors.New(strings.TrimSpace(finalErr.Error()))
}

// ErrorsToString converts the slice of errors into a slice of corresponding
// error messages.
func ErrorsToStrings(errs []error) []string {
	if len(errs) == 0 {
		return nil
	}
	strErrs := make([]string, len(errs))
	for i := range errs {
		strErrs[i] = errs[i].Error()
	}
	return strErrs
}

// StringsToErrors converts a slice of error messages into a slice of
// corresponding errors.
func StringsToErrors(strErrs []string) []error {
	if len(strErrs) == 0 {
		return nil
	}
	errs := make([]error, len(strErrs))
	for i := range strErrs {
		errs[i] = errors.New(strErrs[i])
	}
	return errs
}

// CloseQuiet closes a file and logs any error. Should only be used within
// a defer.
func CloseQuiet(f *os.File) {
	if err := f.Close(); err != nil {
		logrus.Errorf("Unable to close file %s: %q", f.Name(), err)
	}
}

// Contains checks if err's message contains sub's message. Contains should be
// used iff either err or sub has lost type information (e.g., due to
// marshalling).  For typed errors, please use `errors.Contains(...)` or `Is()`
// in recent version of Go.
func Contains(err error, sub error) bool {
	return strings.Contains(err.Error(), sub.Error())
}

// PodConflictErrorModel is used in remote connections with podman
type PodConflictErrorModel struct {
	Errs []string
	Id   string
}

// ErrorModel is used in remote connections with podman
type ErrorModel struct {
	// API root cause formatted for automated parsing
	// example: API root cause
	Because string `json:"cause"`
	// human error message, formatted for a human to read
	// example: human error message
	Message string `json:"message"`
	// HTTP response code
	// min: 400
	ResponseCode int `json:"response"`
}

func (e ErrorModel) Error() string {
	return e.Message
}

func (e ErrorModel) Cause() error {
	return errors.New(e.Because)
}

func (e ErrorModel) Code() int {
	return e.ResponseCode
}

func (e PodConflictErrorModel) Error() string {
	return strings.Join(e.Errs, ",")
}

func (e PodConflictErrorModel) Code() int {
	return 409
}

// Cause returns the most underlying error for the provided one. There is a
// maximum error depth of 100 to avoid endless loops. An additional error log
// message will be created if this maximum has reached.
func Cause(err error) (cause error) {
	cause = err

	const maxDepth = 100
	for i := 0; i <= maxDepth; i++ {
		res := errors.Unwrap(cause)
		if res == nil {
			return cause
		}
		cause = res
	}

	logrus.Errorf("Max error depth of %d reached, cannot unwrap until root cause: %v", maxDepth, err)
	return cause
}
