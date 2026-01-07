package probs

import (
	"fmt"
	"net/http"

	"github.com/letsencrypt/boulder/identifier"
)

const (
	// Error types that can be used in ACME payloads. These are sorted in the
	// same order as they are defined in RFC8555 Section 6.7. We do not implement
	// the `compound`, `externalAccountRequired`, or `userActionRequired` errors,
	// because we have no path that would return them.
	AccountDoesNotExistProblem   = ProblemType("accountDoesNotExist")
	AlreadyRevokedProblem        = ProblemType("alreadyRevoked")
	BadCSRProblem                = ProblemType("badCSR")
	BadNonceProblem              = ProblemType("badNonce")
	BadPublicKeyProblem          = ProblemType("badPublicKey")
	BadRevocationReasonProblem   = ProblemType("badRevocationReason")
	BadSignatureAlgorithmProblem = ProblemType("badSignatureAlgorithm")
	CAAProblem                   = ProblemType("caa")
	// ConflictProblem is a problem type that is not defined in RFC8555.
	ConflictProblem              = ProblemType("conflict")
	ConnectionProblem            = ProblemType("connection")
	DNSProblem                   = ProblemType("dns")
	InvalidContactProblem        = ProblemType("invalidContact")
	MalformedProblem             = ProblemType("malformed")
	OrderNotReadyProblem         = ProblemType("orderNotReady")
	RateLimitedProblem           = ProblemType("rateLimited")
	RejectedIdentifierProblem    = ProblemType("rejectedIdentifier")
	ServerInternalProblem        = ProblemType("serverInternal")
	TLSProblem                   = ProblemType("tls")
	UnauthorizedProblem          = ProblemType("unauthorized")
	UnsupportedContactProblem    = ProblemType("unsupportedContact")
	UnsupportedIdentifierProblem = ProblemType("unsupportedIdentifier")

	ErrorNS = "urn:ietf:params:acme:error:"
)

// ProblemType defines the error types in the ACME protocol
type ProblemType string

// ProblemDetails objects represent problem documents
// https://tools.ietf.org/html/draft-ietf-appsawg-http-problem-00
type ProblemDetails struct {
	Type   ProblemType `json:"type,omitempty"`
	Detail string      `json:"detail,omitempty"`
	// HTTPStatus is the HTTP status code the ProblemDetails should probably be sent
	// as.
	HTTPStatus int `json:"status,omitempty"`
	// SubProblems are optional additional per-identifier problems. See
	// RFC 8555 Section 6.7.1: https://tools.ietf.org/html/rfc8555#section-6.7.1
	SubProblems []SubProblemDetails `json:"subproblems,omitempty"`
}

// SubProblemDetails represents sub-problems specific to an identifier that are
// related to a top-level ProblemDetails.
// See RFC 8555 Section 6.7.1: https://tools.ietf.org/html/rfc8555#section-6.7.1
type SubProblemDetails struct {
	ProblemDetails
	Identifier identifier.ACMEIdentifier `json:"identifier"`
}

func (pd *ProblemDetails) Error() string {
	return fmt.Sprintf("%s :: %s", pd.Type, pd.Detail)
}

// WithSubProblems returns a new ProblemsDetails instance created by adding the
// provided subProbs to the existing ProblemsDetail.
func (pd *ProblemDetails) WithSubProblems(subProbs []SubProblemDetails) *ProblemDetails {
	return &ProblemDetails{
		Type:        pd.Type,
		Detail:      pd.Detail,
		HTTPStatus:  pd.HTTPStatus,
		SubProblems: append(pd.SubProblems, subProbs...),
	}
}

// Helper functions which construct the basic RFC8555 Problem Documents, with
// the Type already set and the Details supplied by the caller.

// AccountDoesNotExist returns a ProblemDetails representing an
// AccountDoesNotExistProblem error
func AccountDoesNotExist(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       AccountDoesNotExistProblem,
		Detail:     detail,
		HTTPStatus: http.StatusBadRequest,
	}
}

// AlreadyRevoked returns a ProblemDetails with a AlreadyRevokedProblem and a 400 Bad
// Request status code.
func AlreadyRevoked(detail string, a ...any) *ProblemDetails {
	return &ProblemDetails{
		Type:       AlreadyRevokedProblem,
		Detail:     fmt.Sprintf(detail, a...),
		HTTPStatus: http.StatusBadRequest,
	}
}

// BadCSR returns a ProblemDetails representing a BadCSRProblem.
func BadCSR(detail string, a ...any) *ProblemDetails {
	return &ProblemDetails{
		Type:       BadCSRProblem,
		Detail:     fmt.Sprintf(detail, a...),
		HTTPStatus: http.StatusBadRequest,
	}
}

// BadNonce returns a ProblemDetails with a BadNonceProblem and a 400 Bad
// Request status code.
func BadNonce(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       BadNonceProblem,
		Detail:     detail,
		HTTPStatus: http.StatusBadRequest,
	}
}

// BadPublicKey returns a ProblemDetails with a BadPublicKeyProblem and a 400 Bad
// Request status code.
func BadPublicKey(detail string, a ...any) *ProblemDetails {
	return &ProblemDetails{
		Type:       BadPublicKeyProblem,
		Detail:     fmt.Sprintf(detail, a...),
		HTTPStatus: http.StatusBadRequest,
	}
}

// BadRevocationReason returns a ProblemDetails representing
// a BadRevocationReasonProblem
func BadRevocationReason(detail string, a ...any) *ProblemDetails {
	return &ProblemDetails{
		Type:       BadRevocationReasonProblem,
		Detail:     fmt.Sprintf(detail, a...),
		HTTPStatus: http.StatusBadRequest,
	}
}

// BadSignatureAlgorithm returns a ProblemDetails with a BadSignatureAlgorithmProblem
// and a 400 Bad Request status code.
func BadSignatureAlgorithm(detail string, a ...any) *ProblemDetails {
	return &ProblemDetails{
		Type:       BadSignatureAlgorithmProblem,
		Detail:     fmt.Sprintf(detail, a...),
		HTTPStatus: http.StatusBadRequest,
	}
}

// CAA returns a ProblemDetails representing a CAAProblem
func CAA(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       CAAProblem,
		Detail:     detail,
		HTTPStatus: http.StatusForbidden,
	}
}

// Connection returns a ProblemDetails representing a ConnectionProblem
// error
func Connection(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       ConnectionProblem,
		Detail:     detail,
		HTTPStatus: http.StatusBadRequest,
	}
}

// DNS returns a ProblemDetails representing a DNSProblem
func DNS(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       DNSProblem,
		Detail:     detail,
		HTTPStatus: http.StatusBadRequest,
	}
}

// InvalidContact returns a ProblemDetails representing an InvalidContactProblem.
func InvalidContact(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       InvalidContactProblem,
		Detail:     detail,
		HTTPStatus: http.StatusBadRequest,
	}
}

// Malformed returns a ProblemDetails with a MalformedProblem and a 400 Bad
// Request status code.
func Malformed(detail string, a ...any) *ProblemDetails {
	if len(a) > 0 {
		detail = fmt.Sprintf(detail, a...)
	}
	return &ProblemDetails{
		Type:       MalformedProblem,
		Detail:     detail,
		HTTPStatus: http.StatusBadRequest,
	}
}

// OrderNotReady returns a ProblemDetails representing a OrderNotReadyProblem
func OrderNotReady(detail string, a ...any) *ProblemDetails {
	return &ProblemDetails{
		Type:       OrderNotReadyProblem,
		Detail:     fmt.Sprintf(detail, a...),
		HTTPStatus: http.StatusForbidden,
	}
}

// RateLimited returns a ProblemDetails representing a RateLimitedProblem error
func RateLimited(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       RateLimitedProblem,
		Detail:     detail,
		HTTPStatus: http.StatusTooManyRequests,
	}
}

// RejectedIdentifier returns a ProblemDetails with a RejectedIdentifierProblem and a 400 Bad
// Request status code.
func RejectedIdentifier(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       RejectedIdentifierProblem,
		Detail:     detail,
		HTTPStatus: http.StatusBadRequest,
	}
}

// ServerInternal returns a ProblemDetails with a ServerInternalProblem and a
// 500 Internal Server Failure status code.
func ServerInternal(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       ServerInternalProblem,
		Detail:     detail,
		HTTPStatus: http.StatusInternalServerError,
	}
}

// TLS returns a ProblemDetails representing a TLSProblem error
func TLS(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       TLSProblem,
		Detail:     detail,
		HTTPStatus: http.StatusBadRequest,
	}
}

// Unauthorized returns a ProblemDetails with an UnauthorizedProblem and a 403
// Forbidden status code.
func Unauthorized(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       UnauthorizedProblem,
		Detail:     detail,
		HTTPStatus: http.StatusForbidden,
	}
}

// UnsupportedContact returns a ProblemDetails representing an
// UnsupportedContactProblem
func UnsupportedContact(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       UnsupportedContactProblem,
		Detail:     detail,
		HTTPStatus: http.StatusBadRequest,
	}
}

// UnsupportedIdentifier returns a ProblemDetails representing an
// UnsupportedIdentifierProblem
func UnsupportedIdentifier(detail string, a ...any) *ProblemDetails {
	return &ProblemDetails{
		Type:       UnsupportedIdentifierProblem,
		Detail:     fmt.Sprintf(detail, a...),
		HTTPStatus: http.StatusBadRequest,
	}
}

// Additional helper functions that return variations on MalformedProblem with
// different HTTP status codes set.

// Canceled returns a ProblemDetails with a MalformedProblem and a 408 Request
// Timeout status code.
func Canceled(detail string, a ...any) *ProblemDetails {
	if len(a) > 0 {
		detail = fmt.Sprintf(detail, a...)
	}
	return &ProblemDetails{
		Type:       MalformedProblem,
		Detail:     detail,
		HTTPStatus: http.StatusRequestTimeout,
	}
}

// Conflict returns a ProblemDetails with a ConflictProblem and a 409 Conflict
// status code.
func Conflict(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       ConflictProblem,
		Detail:     detail,
		HTTPStatus: http.StatusConflict,
	}
}

// ContentLengthRequired returns a ProblemDetails representing a missing
// Content-Length header error
func ContentLengthRequired() *ProblemDetails {
	return &ProblemDetails{
		Type:       MalformedProblem,
		Detail:     "missing Content-Length header",
		HTTPStatus: http.StatusLengthRequired,
	}
}

// InvalidContentType returns a ProblemDetails suitable for a missing
// ContentType header, or an incorrect ContentType header
func InvalidContentType(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       MalformedProblem,
		Detail:     detail,
		HTTPStatus: http.StatusUnsupportedMediaType,
	}
}

// MethodNotAllowed returns a ProblemDetails representing a disallowed HTTP
// method error.
func MethodNotAllowed() *ProblemDetails {
	return &ProblemDetails{
		Type:       MalformedProblem,
		Detail:     "Method not allowed",
		HTTPStatus: http.StatusMethodNotAllowed,
	}
}

// NotFound returns a ProblemDetails with a MalformedProblem and a 404 Not Found
// status code.
func NotFound(detail string) *ProblemDetails {
	return &ProblemDetails{
		Type:       MalformedProblem,
		Detail:     detail,
		HTTPStatus: http.StatusNotFound,
	}
}
