package edge

import (
	"errors"
	"path"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	MaxProjectExecStdinBytes  = 32 << 10
	MaxProjectExecStreamBytes = 24 << 10
	maxProjectExecArgvBytes   = 32 << 10
	maxProjectExecEnvBytes    = 16 << 10
)

var projectExecEnvironmentKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

func validateOperationRequestWithProjectExec(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	if kind == OperationProjectExec {
		return normalizeProjectExecRequest(request)
	}
	if !emptyProjectExecRequestFields(request) {
		return OperationRequest{}, errors.New("project exec fields are invalid for this operation")
	}
	return validateOperationRequest(kind, request)
}

func normalizeProjectExecRequest(request OperationRequest) (OperationRequest, error) {
	request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
	request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
	request.Profile = strings.TrimSpace(request.Profile)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if !projectOperationAliasPattern.MatchString(request.Alias) || !projectOperationTargetPattern.MatchString(request.TargetAlias) ||
		request.Profile != "linux-workcell" || request.Repository != "" || request.Platform != "" || request.Machine != "" ||
		request.Target != "" || request.Difficulty != "" || request.OperatingSystem != "" || request.WorkspaceID != "" ||
		request.RunUntil != "" || request.Release != "" || !projectOperationIdempotencyPattern.MatchString(request.IdempotencyKey) {
		return OperationRequest{}, errors.New("project exec request is invalid")
	}
	if len(request.Argv) == 0 || len(request.Argv) > 128 {
		return OperationRequest{}, errors.New("project exec argv is invalid")
	}
	argvBytes := 0
	for _, argument := range request.Argv {
		if argument == "" || len(argument) > 8192 || !utf8.ValidString(argument) || strings.ContainsRune(argument, 0) {
			return OperationRequest{}, errors.New("project exec argv is invalid")
		}
		argvBytes += len(argument)
	}
	if argvBytes > maxProjectExecArgvBytes {
		return OperationRequest{}, errors.New("project exec argv is invalid")
	}
	cwd := strings.TrimSpace(request.CWD)
	if cwd == "" || cwd == "." {
		request.CWD = ""
	} else {
		if len(cwd) > 1024 || path.IsAbs(cwd) || strings.ContainsAny(cwd, "\\\x00") {
			return OperationRequest{}, errors.New("project exec cwd is invalid")
		}
		cwd = path.Clean(cwd)
		if cwd == "." || cwd == ".." || strings.HasPrefix(cwd, "../") {
			return OperationRequest{}, errors.New("project exec cwd is invalid")
		}
		request.CWD = cwd
	}
	if len(request.Stdin) > MaxProjectExecStdinBytes || !utf8.ValidString(request.Stdin) || strings.ContainsRune(request.Stdin, 0) {
		return OperationRequest{}, errors.New("project exec stdin is invalid")
	}
	if len(request.Environment) > 32 {
		return OperationRequest{}, errors.New("project exec environment is invalid")
	}
	environmentBytes := 0
	for key, value := range request.Environment {
		if !projectExecEnvironmentKeyPattern.MatchString(key) || projectExecReservedEnvironmentKey(key) ||
			len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return OperationRequest{}, errors.New("project exec environment is invalid")
		}
		environmentBytes += len(key) + len(value)
	}
	if environmentBytes > maxProjectExecEnvBytes || request.TimeoutSeconds < 1 || request.TimeoutSeconds > 120 {
		return OperationRequest{}, errors.New("project exec request is invalid")
	}
	return request, nil
}

func projectExecReservedEnvironmentKey(key string) bool {
	upper := strings.ToUpper(key)
	switch upper {
	case "PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "TERM", "TMPDIR",
		"DOCKER_HOST", "CONTAINER_HOST", "DOCKER_CONFIG":
		return true
	}
	if strings.HasPrefix(upper, "XDG_") || strings.HasPrefix(upper, "MCP_DEVBOX_") {
		return true
	}
	for _, fragment := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "PRIVATE_KEY", "API_KEY"} {
		if strings.Contains(upper, fragment) {
			return true
		}
	}
	return false
}

func emptyProjectExecRequestFields(request OperationRequest) bool {
	return len(request.Argv) == 0 && request.CWD == "" && request.Stdin == "" && len(request.Environment) == 0 && request.TimeoutSeconds == 0
}

func operationRequestEmpty(request OperationRequest) bool {
	return reflect.DeepEqual(request, OperationRequest{})
}

func operationRequestStableBundle(request OperationRequest) bool {
	return reflect.DeepEqual(request, OperationRequest{Release: "stable"})
}

func operationRequestsEqual(left, right OperationRequest) bool {
	return reflect.DeepEqual(left, right)
}

func projectOperationUsesIdempotency(kind OperationKind) bool {
	return kind == OperationProjectSnapshot || kind == OperationProjectExec
}

func hasProjectExecResult(result OperationResult) bool {
	return result.ExecCompleted || result.ExecExitCode != 0 || result.ExecStdout != "" || result.ExecStderr != "" ||
		result.ExecTimedOut || result.ExecStdoutTruncated || result.ExecStderrTruncated
}

func validProjectExecResult(result OperationResult) bool {
	if !result.ExecCompleted || result.ExecExitCode < -1 || result.ExecExitCode > 255 ||
		result.ExecTimedOut != (result.ExecExitCode == -1) || len(result.ExecStdout) > MaxProjectExecStreamBytes ||
		len(result.ExecStderr) > MaxProjectExecStreamBytes || !utf8.ValidString(result.ExecStdout) || !utf8.ValidString(result.ExecStderr) ||
		strings.ContainsRune(result.ExecStdout, 0) || strings.ContainsRune(result.ExecStderr, 0) {
		return false
	}
	metadata := result
	metadata.ExecCompleted = false
	metadata.ExecExitCode = 0
	metadata.ExecStdout = ""
	metadata.ExecStderr = ""
	metadata.ExecTimedOut = false
	metadata.ExecStdoutTruncated = false
	metadata.ExecStderrTruncated = false
	return validProjectOperationResult(metadata)
}
