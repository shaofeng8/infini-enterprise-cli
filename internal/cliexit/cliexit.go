// Package cliexit maps failures onto the stable exit codes every command shares.
//
//	0 success | 1 business | 2 usage | 3 auth | 4 network | 5 license
//
// Scripts and CI branch on these numbers, so they must not be renumbered.
package cliexit

import (
	"errors"
	"fmt"
)

const (
	CodeOK       = 0
	CodeBusiness = 1
	CodeUsage    = 2
	CodeAuth     = 3
	CodeNetwork  = 4
	CodeLicense  = 5
)

// Error carries an exit code and an optional remediation hint shown to humans.
type Error struct {
	Code int
	Err  error
	Hint string
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func New(code int, format string, args ...any) *Error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

func Wrap(code int, err error) *Error {
	if err == nil {
		return nil
	}
	var existing *Error
	if errors.As(err, &existing) {
		return existing
	}
	return &Error{Code: code, Err: err}
}

// Hint attaches a remediation hint, preserving any code already assigned.
func Hint(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	wrapped := Wrap(CodeBusiness, err)
	wrapped.Hint = fmt.Sprintf(format, args...)
	return wrapped
}

func Usage(format string, args ...any) *Error {
	return New(CodeUsage, format, args...)
}

func CodeOf(err error) int {
	if err == nil {
		return CodeOK
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeBusiness
}

func HintOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Hint
	}
	return ""
}
