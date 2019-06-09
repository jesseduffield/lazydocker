package commands

import (
	"fmt"

	"github.com/go-errors/errors"
	"golang.org/x/xerrors"
)

const (
	// MustStopContainer tells us that we must stop the container before removing it
	MustStopContainer = iota
)

// WrapError wraps an error for the sake of showing a stack trace at the top level
// the go-errors package, for some reason, does not return nil when you try to wrap
// a non-error, so we're just doing it here
func WrapError(err error) error {
	if err == nil {
		return err
	}

	return errors.Wrap(err, 0)
}

// ComplexError an error which carries a code so that calling code has an easier job to do
// adapted from https://medium.com/yakka/better-go-error-handling-with-xerrors-1987650e0c79
type ComplexError struct {
	Message string
	Code    int
	frame   xerrors.Frame
}

// FormatError is a function
func (ce ComplexError) FormatError(p xerrors.Printer) error {
	p.Printf("%d %s", ce.Code, ce.Message)
	ce.frame.Format(p)
	return nil
}

// Format is a function
func (ce ComplexError) Format(f fmt.State, c rune) {
	xerrors.FormatError(ce, f, c)
}

func (ce ComplexError) Error() string {
	return fmt.Sprint(ce)
}

// HasErrorCode is a function
func HasErrorCode(err error, code int) bool {
	var originalErr ComplexError
	if xerrors.As(err, &originalErr) {
		return originalErr.Code == MustStopContainer
	}
	return false
}
