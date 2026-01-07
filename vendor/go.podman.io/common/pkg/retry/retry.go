package retry

import (
	"context"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/docker/distribution/registry/api/errcode"
	errcodev2 "github.com/docker/distribution/registry/api/v2"
	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/docker"
)

// Options defines the option to retry.
type Options struct {
	MaxRetry         int           // The number of times to possibly retry.
	Delay            time.Duration // The delay to use between retries, if set.
	IsErrorRetryable func(error) bool
}

// RetryOptions is deprecated, use Options.
type RetryOptions = Options // nolint:revive

// RetryIfNecessary deprecated function use IfNecessary.
func RetryIfNecessary(ctx context.Context, operation func() error, options *Options) error { // nolint:revive
	return IfNecessary(ctx, operation, options)
}

// IfNecessary retries the operation in exponential backoff with the retry Options.
func IfNecessary(ctx context.Context, operation func() error, options *Options) error {
	var isRetryable func(error) bool
	if options.IsErrorRetryable != nil {
		isRetryable = options.IsErrorRetryable
	} else {
		isRetryable = IsErrorRetryable
	}
	err := operation()
	for attempt := 0; err != nil && isRetryable(err) && attempt < options.MaxRetry; attempt++ {
		delay := time.Duration(int(math.Pow(2, float64(attempt)))) * time.Second
		if options.Delay != 0 {
			delay = options.Delay
		}
		logrus.Warnf("Failed, retrying in %s ... (%d/%d). Error: %v", delay, attempt+1, options.MaxRetry, err)
		delay += rand.N(delay / 10) // 10 % jitter so that a failure blip doesn’t cause a deterministic stampede
		logrus.Debugf("Retry delay with added jitter: %s", delay)
		select {
		case <-time.After(delay):
			// Do nothing.
		case <-ctx.Done():
			return err
		}
		err = operation()
	}
	return err
}

// IsErrorRetryable makes a HEURISTIC determination whether it is worth retrying upon encountering an error.
// That heuristic is NOT STABLE and it CAN CHANGE AT ANY TIME.
// Callers that have a hard requirement for specific treatment of a class of errors should make their own check
// instead of relying on this function maintaining its past behavior.
func IsErrorRetryable(err error) bool {
	switch err {
	case nil:
		return false
	case context.Canceled, context.DeadlineExceeded:
		return false
	default: // continue
	}

	type unwrapper interface {
		Unwrap() error
	}

	switch e := err.(type) {
	case errcode.Error:
		switch e.Code {
		case errcode.ErrorCodeUnauthorized, errcode.ErrorCodeDenied,
			errcodev2.ErrorCodeNameUnknown, errcodev2.ErrorCodeManifestUnknown:
			return false
		}
		return true
	case docker.UnexpectedHTTPStatusError:
		// Retry on 502, 502 and 503 http server errors, they appear to be quite common in the field.
		// https://github.com/containers/common/issues/2299
		if e.StatusCode >= http.StatusBadGateway && e.StatusCode <= http.StatusGatewayTimeout {
			return true
		}
		return false
	case *net.OpError:
		return IsErrorRetryable(e.Err)
	case *url.Error: // This includes errors returned by the net/http client.
		if e.Err == io.EOF { // Happens when a server accepts a HTTP connection and sends EOF
			return true
		}
		return IsErrorRetryable(e.Err)
	case syscall.Errno:
		return isErrnoRetryable(e)
	case errcode.Errors:
		// if this error is a group of errors, process them all in turn
		for i := range e {
			if !IsErrorRetryable(e[i]) {
				return false
			}
		}
		return true
	case *multierror.Error:
		// if this error is a group of errors, process them all in turn
		for i := range e.Errors {
			if !IsErrorRetryable(e.Errors[i]) {
				return false
			}
		}
		return true
	case net.Error:
		if e.Timeout() {
			return true
		}
		if unwrappable, ok := e.(unwrapper); ok {
			err = unwrappable.Unwrap()
			return IsErrorRetryable(err)
		}
	case unwrapper: // Test this last, because various error types might implement .Unwrap()
		err = e.Unwrap()
		return IsErrorRetryable(err)
	}

	return false
}

func isErrnoRetryable(e error) bool {
	switch e {
	case syscall.ECONNREFUSED, syscall.EINTR, syscall.EAGAIN, syscall.EBUSY, syscall.ENETDOWN, syscall.ENETUNREACH, syscall.ENETRESET, syscall.ECONNABORTED, syscall.ECONNRESET, syscall.ETIMEDOUT, syscall.EHOSTDOWN, syscall.EHOSTUNREACH:
		return true
	}
	return isErrnoERESTART(e)
}
