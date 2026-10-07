// Package clierr defines the error kinds commands return and the stable exit
// codes they map to. The codes follow gh and are a compatibility contract:
// 0 success, 1 failure (including usage errors), 2 cancelled, 4 auth required.
package clierr

import (
	"errors"
	"fmt"
)

// ExitCode is a process exit status.
type ExitCode int

const (
	ExitOK           ExitCode = 0
	ExitError        ExitCode = 1
	ExitCancel       ExitCode = 2
	ExitAuthRequired ExitCode = 4
)

// ErrSilent fails the command with exit 1 after the command already printed its own diagnostics.
var ErrSilent = errors.New("silent error")

// ErrCancel signals that the user cancelled an interactive prompt.
var ErrCancel = errors.New("cancelled")

// FlagError is a usage error: bad flags, arguments or flag combinations. It
// exits 1 like any failure; it differs only in printing a --help hint.
type FlagError struct{ err error }

func (e *FlagError) Error() string { return e.err.Error() }
func (e *FlagError) Unwrap() error { return e.err }

// FlagErrorf returns a usage error.
func FlagErrorf(format string, args ...any) error {
	return &FlagError{fmt.Errorf(format, args...)}
}

// FlagErrorWrap marks err as a usage error.
func FlagErrorWrap(err error) error { return &FlagError{err} }

// AuthError means the command needs credentials it does not have or that were rejected.
type AuthError struct{ err error }

func (e *AuthError) Error() string { return e.err.Error() }
func (e *AuthError) Unwrap() error { return e.err }

// AuthErrorf returns an authentication error.
func AuthErrorf(format string, args ...any) error {
	return &AuthError{fmt.Errorf(format, args...)}
}

// AuthErrorWrap marks err as an authentication error.
func AuthErrorWrap(err error) error { return &AuthError{err} }

// StatusCoder is implemented by API errors that carry an HTTP status.
type StatusCoder interface {
	error
	HTTPStatus() int
}

// Code maps an error returned by a command to its exit code.
func Code(err error) ExitCode {
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, ErrCancel) {
		return ExitCancel
	}
	if _, ok := errors.AsType[*AuthError](err); ok {
		return ExitAuthRequired
	}
	if sc, ok := errors.AsType[StatusCoder](err); ok && sc.HTTPStatus() == 401 {
		return ExitAuthRequired
	}
	return ExitError
}
