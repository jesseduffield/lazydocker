package multierr

import (
	"fmt"
	"strings"
)

// Format creates an error value from the input array (which should not be empty)
// If the input contains a single error value, it is returned as is.
// If there are multiple, they are formatted as a multi-error (with Unwrap() []error) with the provided initial, separator, and ending strings.
//
// Typical usage:
//
//	var errs []error
//	// …
//	errs = append(errs, …)
//	// …
//	if errs != nil { return multierr.Format("Failures doing $FOO", "\n* ", "", errs)}
func Format(first, middle, last string, errs []error) error {
	switch len(errs) {
	case 0:
		return fmt.Errorf("internal error: multierr.Format called with 0 errors")
	case 1:
		return errs[0]
	default:
		// We have to do this — and this function only really exists — because fmt.Errorf(format, errs...) is invalid:
		// []error is not a valid parameter to a function expecting []any
		anyErrs := make([]any, 0, len(errs))
		for _, e := range errs {
			anyErrs = append(anyErrs, e)
		}
		return fmt.Errorf(first+"%w"+strings.Repeat(middle+"%w", len(errs)-1)+last, anyErrs...)
	}
}
