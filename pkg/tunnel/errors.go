package tunnel

import (
	"errors"
	"fmt"
)

// ErrorCode represents error types.
type ErrorCode string

const (
	ErrCodeAuthFailed       ErrorCode = "AUTH_FAILED"
	ErrCodeTokenExpired     ErrorCode = "TOKEN_EXPIRED"
	ErrCodeTokenInvalid     ErrorCode = "TOKEN_INVALID"
	ErrCodeDomainTaken      ErrorCode = "DOMAIN_TAKEN"
	ErrCodePortUnavailable  ErrorCode = "PORT_UNAVAILABLE"
	ErrCodeConnectionFailed ErrorCode = "CONNECTION_FAILED"
	ErrCodeTunnelFailed     ErrorCode = "TUNNEL_FAILED"
	ErrCodeServerError      ErrorCode = "SERVER_ERROR"
)

// Error is a structured error for the tunnel package.
type Error struct {
	Code    ErrorCode
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error {
	return e.Cause
}

// Is implements errors.Is.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// Predefined errors.
var (
	ErrNotAuthenticated = &Error{Code: ErrCodeAuthFailed, Message: "not authenticated"}
	ErrTokenExpired     = &Error{Code: ErrCodeTokenExpired, Message: "token expired, please re-authenticate"}
	ErrTokenInvalid     = &Error{Code: ErrCodeTokenInvalid, Message: "invalid token"}
	ErrDomainTaken      = &Error{Code: ErrCodeDomainTaken, Message: "domain already in use"}
	ErrPortUnavailable  = &Error{Code: ErrCodePortUnavailable, Message: "no available port"}
	ErrConnectionClosed = &Error{Code: ErrCodeConnectionFailed, Message: "connection closed"}
)

// IsAuthError returns true if the error is an authentication error.
func IsAuthError(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == ErrCodeAuthFailed || e.Code == ErrCodeTokenExpired || e.Code == ErrCodeTokenInvalid
	}
	return false
}

// IsDomainTaken returns true if the error indicates domain is taken.
func IsDomainTaken(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == ErrCodeDomainTaken
	}
	return false
}
