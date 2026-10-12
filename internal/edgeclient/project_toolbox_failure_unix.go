//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Keep the original cause available to errors.Is/As without formatting engine
// output, paths, or credentials into the returned error.
type projectToolboxInspectionError struct {
	stage, classified, cause error
}

// The CLI reports socket/storage failures as ordinary exit errors. Keep only
// closed hints from its bounded output, never the output itself. Hints are for
// diagnosis only and must not authorize retry, repair, or identity acceptance.
type projectToolboxCommandError struct {
	cause    error
	category string
}

func (e *projectToolboxCommandError) Error() string { return "toolbox engine command failed" }
func (e *projectToolboxCommandError) Unwrap() error { return e.cause }

func toolboxCommandError(err error, output []byte) error {
	category := ""
	if len(output) > 64<<10 {
		output = output[:64<<10]
	}
	text := strings.ToLower(string(output))
	for _, hint := range []struct{ text, category string }{
		{"read-only file system", "engine_read_only_filesystem"},
		{"permission denied", "engine_permission_denied"},
		{"connection refused", "engine_connection_refused"},
		{"connection reset by peer", "engine_connection_reset"},
		{"no space left on device", "engine_storage_full"},
		{"cannot connect to podman", "engine_unreachable"},
	} {
		if strings.Contains(text, hint.text) {
			category = hint.category
			break
		}
	}
	return &projectToolboxCommandError{cause: err, category: category}
}

func (e *projectToolboxInspectionError) Error() string { return e.stage.Error() }
func (e *projectToolboxInspectionError) Unwrap() []error {
	causes := []error{}
	if e.classified != nil {
		causes = append(causes, e.classified)
	}
	if e.cause != nil {
		causes = append(causes, e.cause)
	}
	if errors.Is(e.classified, ErrProjectToolboxContainerMissing) || errors.Is(e.classified, ErrProjectToolboxEndpointStale) {
		return causes
	}
	return append(causes, e.stage)
}

func toolboxInspectionError(stage, classified, cause error) error {
	return &projectToolboxInspectionError{stage: stage, classified: classified, cause: cause}
}

// ProjectToolboxFailureDiagnostic returns closed, content-free fields for the
// private Edge journal. An exit code of -1 means no normal exit was observed.
func ProjectToolboxFailureDiagnostic(err error) (stage, category string, exitCode int, ok bool) {
	var failure *projectToolboxInspectionError
	if !errors.As(err, &failure) {
		return "", "", -1, false
	}
	stage = "unknown"
	switch failure.stage {
	case ErrProjectToolboxOwnershipInspectUnavailable:
		stage = "ownership"
	case ErrProjectToolboxStateInspectUnavailable:
		stage = "state"
	case ErrProjectToolboxStorageInspectUnavailable:
		stage = "storage"
	case ErrProjectToolboxMountMismatch:
		stage = "mounts"
	case ErrProjectToolboxResourceMismatch:
		stage = "resources"
	case ErrProjectToolboxEnvironmentMismatch:
		stage = "environment"
	}
	category, exitCode = "unknown", -1
	switch {
	case failure.cause == nil:
		category = "invalid_response"
	case errors.Is(err, context.Canceled):
		category = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		category = "timeout"
	case errors.Is(failure.cause, os.ErrPermission):
		category = "permission_denied"
	case errors.Is(failure.cause, syscall.ECONNREFUSED):
		category = "connection_refused"
	case errors.Is(failure.cause, syscall.ECONNRESET):
		category = "connection_reset"
	default:
		var limited *rootlessCommandOutputLimitError
		var exited *exec.ExitError
		var path *os.PathError
		switch {
		case errors.As(failure.cause, &limited):
			category, exitCode = "output_limit", limited.ExitCode()
		case errors.As(failure.cause, &exited):
			category, exitCode = "process_exit", exited.ExitCode()
			var command *projectToolboxCommandError
			if errors.As(failure.cause, &command) && command.category != "" {
				category = command.category
			}
		case errors.As(failure.cause, &path):
			category = "process_start_failed"
		}
	}
	return stage, category, exitCode, true
}

func toolboxContextFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func (manager *ProjectToolboxManager) inspectFailure(ctx context.Context, record projectToolboxRecord, reference string, stage, cause error) error {
	if cancelled := toolboxContextFailure(ctx, cause); cancelled != nil {
		return toolboxInspectionError(stage, nil, cancelled)
	}
	classified := manager.classifyContainerInspectFailure(ctx, record, reference)
	return toolboxInspectionError(stage, classified, cause)
}
