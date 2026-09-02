package sandboxexecutor

import "errors"

// executorErrorCode is deliberately private. The executor maps these codes to
// the bounded sandboxprotocol.Error contract at the HTTP boundary, while the
// wrapped error remains available for local diagnostics and audit logs.
type executorErrorCode string

func (c executorErrorCode) String() string { return string(c) }

const (
	executorErrInvalidRequest       executorErrorCode = "invalid_request"
	executorErrRequestPolicy        executorErrorCode = "request_policy_denied"
	executorErrWorkspaceSelection   executorErrorCode = "workspace_selection_invalid"
	executorErrWorkingDirectory     executorErrorCode = "working_directory_invalid"
	executorErrWorkspaceSecret      executorErrorCode = "workspace_secret_denied"
	executorErrWorkspaceUnavailable executorErrorCode = "workspace_unavailable"
	executorErrConflict             executorErrorCode = "request_conflict"
	executorErrUnavailable          executorErrorCode = "executor_unavailable"
	executorErrFailed               executorErrorCode = "executor_failed"
	executorErrTimeout              executorErrorCode = "execution_timeout"
	executorErrInternal             executorErrorCode = "internal_error"
)

type executorError struct {
	code executorErrorCode
	err  error
}

func (e *executorError) Error() string { return e.err.Error() }

func (e *executorError) Unwrap() error { return e.err }

func newExecutorError(code executorErrorCode, message string) error {
	return &executorError{code: code, err: errors.New(message)}
}

func wrapExecutorError(code executorErrorCode, err error) error {
	if err == nil {
		return nil
	}
	var existing *executorError
	if errors.As(err, &existing) {
		return err
	}
	return &executorError{code: code, err: err}
}

func executorErrorCodeOf(err error) (executorErrorCode, bool) {
	var typed *executorError
	if !errors.As(err, &typed) || typed == nil {
		return "", false
	}
	return typed.code, true
}
