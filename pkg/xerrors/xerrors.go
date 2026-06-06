// Package xerrors defines the stable, machine-readable error codes that agentx
// surfaces to agents. Codes mirror the contract documented in the original
// twitter-cli SCHEMA.md so existing agent tooling stays compatible.
package xerrors

import "fmt"

// Code is a stable error category.
type Code string

const (
	NotAuthenticated Code = "not_authenticated"
	NotFound         Code = "not_found"
	InvalidInput     Code = "invalid_input"
	RateLimited      Code = "rate_limited"
	APIError         Code = "api_error"
	QueryIDError     Code = "query_id_error"
	NetworkError     Code = "network_error"
	AccountError     Code = "account_error"
)

// Error carries a Code plus a human-readable message.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	cause   error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
func (e *Error) Unwrap() error { return e.cause }

// New builds an Error.
func New(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap builds an Error that preserves the underlying cause.
func Wrap(code Code, cause error, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), cause: cause}
}

// CodeOf returns the Code for an error, or APIError if it is not an *Error.
func CodeOf(err error) Code {
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return APIError
}
