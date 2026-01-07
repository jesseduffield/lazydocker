package docker

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/docker/distribution/registry/api/errcode"
	"github.com/sirupsen/logrus"
)

var (
	// ErrV1NotSupported is returned when we're trying to talk to a
	// docker V1 registry.
	// Deprecated: The V1 container registry detection is no longer performed, so this error is never returned.
	ErrV1NotSupported = errors.New("can't talk to a V1 container registry")
	// ErrTooManyRequests is returned when the status code returned is 429
	ErrTooManyRequests = errors.New("too many requests to registry")
)

// ErrUnauthorizedForCredentials is returned when the status code returned is 401
type ErrUnauthorizedForCredentials struct { // We only use a struct to allow a type assertion, without limiting the contents of the error otherwise.
	Err error
}

func (e ErrUnauthorizedForCredentials) Error() string {
	return fmt.Sprintf("unable to retrieve auth token: invalid username/password: %s", e.Err.Error())
}

// httpResponseToError translates the https.Response into an error, possibly prefixing it with the supplied context. It returns
// nil if the response is not considered an error.
// NOTE: Almost all callers in this package should use registryHTTPResponseToError instead.
func httpResponseToError(res *http.Response, context string) error {
	switch res.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusTooManyRequests:
		return ErrTooManyRequests
	case http.StatusUnauthorized:
		err := registryHTTPResponseToError(res)
		return ErrUnauthorizedForCredentials{Err: err}
	default:
		if context == "" {
			return newUnexpectedHTTPStatusError(res)
		}
		return fmt.Errorf("%s: %w", context, newUnexpectedHTTPStatusError(res))
	}
}

// registryHTTPResponseToError creates a Go error from an HTTP error response of a docker/distribution
// registry.
//
// WARNING: The OCI distribution spec says
// “A `4XX` response code from the registry MAY return a body in any format.”; but if it is
// JSON, it MUST use the errcode.Error structure.
// So, callers should primarily decide based on HTTP StatusCode, not based on error type here.
func registryHTTPResponseToError(res *http.Response) error {
	err := handleErrorResponse(res)
	// len(errs) == 0 should never be returned by handleErrorResponse; if it does, we don't modify it and let the caller report it as is.
	if errs, ok := err.(errcode.Errors); ok && len(errs) > 0 {
		// The docker/distribution registry implementation almost never returns
		// more than one error in the HTTP body; it seems there is only one
		// possible instance, where the second error reports a cleanup failure
		// we don't really care about.
		//
		// The only _common_ case where a multi-element error is returned is
		// created by the handleErrorResponse parser when OAuth authorization fails:
		// the first element contains errors from a WWW-Authenticate header, the second
		// element contains errors from the response body.
		//
		// In that case the first one is currently _slightly_ more informative (ErrorCodeUnauthorized
		// for invalid tokens, ErrorCodeDenied for permission denied with a valid token
		// for the first error, vs. ErrorCodeUnauthorized for both cases for the second error.)
		//
		// Also, docker/docker similarly only logs the other errors and returns the
		// first one.
		if len(errs) > 1 {
			logrus.Debugf("Discarding non-primary errors:")
			for _, err := range errs[1:] {
				logrus.Debugf("  %s", err.Error())
			}
		}
		err = errs[0]
	}
	switch e := err.(type) {
	case *unexpectedHTTPResponseError:
		response := string(e.Response)
		if len(response) > 50 {
			response = response[:50] + "..."
		}
		// %.0w makes e visible to error.Unwrap() without including any text
		err = fmt.Errorf("StatusCode: %d, %q%.0w", e.StatusCode, response, e)
	case errcode.Error:
		// e.Error() is fmt.Sprintf("%s: %s", e.Code.Error(), e.Message, which is usually
		// rather redundant. So reword it without using e.Code.Error() if e.Message is the default.
		if e.Message == e.Code.Message() {
			// %.0w makes e visible to error.Unwrap() without including any text
			err = fmt.Errorf("%s%.0w", e.Message, e)
		}
	}
	return err
}
