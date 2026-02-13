package app

import (
	"errors"
	"net"
)

// Exit codes:
// 0 = success
// 1 = general error
// 2 = auth required (token expired/missing)
// 3 = network error
// 4 = not found

type ExitError struct {
	Code int
	Err  error
}

func (e ExitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e ExitError) Unwrap() error {
	return e.Err
}

func WrapExit(code int, err error) error {
	if err == nil {
		return nil
	}
	return ExitError{Code: code, Err: err}
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr ExitError
	if errors.As(err, &exitErr) && exitErr.Code != 0 {
		return exitErr.Code
	}
	if isNetErr(err) {
		return 3
	}
	return 1
}

func isNetErr(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}
