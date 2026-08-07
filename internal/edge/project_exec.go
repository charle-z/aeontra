package edge

import (
	"errors"
	"path"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/charle-z/mcp-devbox/internal/policy"
)

const (
	MaxProjectExecStdinBytes  = 32 << 10
	MaxProjectExecStreamBytes = 24 << 10
	maxProjectExecArgvBytes   = 32 << 10
	maxProjectExecEnvBytes    = 16 << 10
)

var projectExecEnvironmentKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

func validateOperationRequestWithProjectExec(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	isBrowserHarness := kind == OperationProjectBrowserHarnessStart || kind == OperationProjectBrowserHarnessStatus || kind == OperationProjectBrowserHarnessList || kind == OperationProjectBrowserHarnessStop || kind == OperationProjectBrowserHarnessCleanup || kind == OperationProjectBrowserHarnessArtifactList || kind == OperationProjectBrowserHarnessArtifactRead
	if isBrowserHarness {
		return normalizeProjectBrowserHarnessRequest(kind, request)
	}
	if !emptyProjectBrowserHarnessRequestFields(request) {
		return OperationRequest{}, errors.New("project browser harness fields are invalid for this operation")
	}
	isBrowser := kind == OperationProjectBrowserCreate || kind == OperationProjectBrowserStatus || kind == OperationProjectBrowserList || kind == OperationProjectBrowserRun || kind == OperationProjectBrowserArtifactRead || kind == OperationProjectBrowserClose || kind == OperationProjectBrowserCleanup
	if isBrowser {
		return normalizeProjectBrowserRequest(kind, request)
	}
	if !emptyProjectBrowserRequestFields(request) {
		return OperationRequest{}, errors.New("project browser fields are invalid for this operation")
	}
	isToolbox := kind == OperationProjectToolboxCreate || kind == OperationProjectToolboxStatus || kind == OperationProjectToolboxExec || kind == OperationProjectToolboxInstall || kind == OperationProjectToolboxCleanup || kind == OperationProjectToolboxRepair || kind == OperationProjectToolboxServiceStart || kind == OperationProjectToolboxServiceStatus || kind == OperationProjectToolboxServiceStop
	if !isToolbox && (request.ToolboxServiceID != "" || request.ToolboxServiceName != "" || hasProjectToolboxResourceRequest(request)) {
		return OperationRequest{}, errors.New("project toolbox service fields are invalid for this operation")
	}
	if kind == OperationProjectNetworkRoute || kind == OperationProjectNetworkProbe {
		return normalizeProjectNetworkRequest(kind, request)
	}
	if !emptyProjectNetworkRequestFields(request) {
		return OperationRequest{}, errors.New("project network fields are invalid for this operation")
	}
	if kind == OperationProjectExec {
		return normalizeProjectExecRequest(request)
	}
	if kind == OperationProjectProcessStart || kind == OperationProjectProcessStatus || kind == OperationProjectProcessStop || kind == OperationProjectProcessSignal || kind == OperationProjectProcessList || kind == OperationProjectProcessCleanup {
		return normalizeProjectProcessRequest(kind, request)
	}
	if kind == OperationProjectGitStatus || kind == OperationProjectGitFetch || kind == OperationProjectGitFastForwardPreview || kind == OperationProjectGitFastForward {
		return normalizeProjectGitSyncRequest(kind, request)
	}
	if kind == OperationProjectGitHubStatus {
		return normalizeProjectGitHubRequest(request)
	}
	if isToolbox {
		return normalizeProjectToolboxRequest(kind, request)
	}
	if !emptyProjectExecRequestFields(request) {
		return OperationRequest{}, errors.New("project exec fields are invalid for this operation")
	}
	if !emptyProjectProcessRequestFields(request) {
		return OperationRequest{}, errors.New("project process fields are invalid for this operation")
	}
	if request.GitPlanID != "" {
		return OperationRequest{}, errors.New("project Git fields are invalid for this operation")
	}
	return validateOperationRequest(kind, request)
}

func normalizeProjectExecRequest(request OperationRequest) (OperationRequest, error) {
	if !emptyProjectProcessRequestFields(request) {
		return OperationRequest{}, errors.New("project exec request is invalid")
	}
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
	if projectExecSecretShaped(strings.Join(request.Argv, "\n")) {
		return OperationRequest{}, errors.New("project exec argv contains a secret")
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
	if projectExecSecretShaped(request.Stdin) {
		return OperationRequest{}, errors.New("project exec stdin contains a secret")
	}
	if len(request.Environment) > 32 {
		return OperationRequest{}, errors.New("project exec environment is invalid")
	}
	environmentBytes := 0
	for key, value := range request.Environment {
		if !projectExecEnvironmentKeyPattern.MatchString(key) || projectExecReservedEnvironmentKey(key) || projectExecSecretShaped(value) ||
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

func projectExecSecretShaped(value string) bool {
	redacted, changed := policy.Redact(value)
	return changed || redacted != value
}

func projectExecReservedEnvironmentKey(key string) bool {
	upper := strings.ToUpper(key)
	switch upper {
	case "PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "TERM", "TMPDIR",
		"DOCKER_HOST", "CONTAINER_HOST", "DOCKER_CONFIG",
		"CONTAINERS_HELPER_BINARY_DIR", "CONTAINERS_CONF", "CONTAINERS_CONF_OVERRIDE", "CONTAINERS_CONF_MODULES",
		"CONTAINERS_STORAGE_CONF":
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
	return kind == OperationProjectSnapshot || kind == OperationProjectExec || kind == OperationProjectProcessStart ||
		kind == OperationProjectBrowserCreate || kind == OperationProjectBrowserRun || kind == OperationProjectBrowserClose || kind == OperationProjectBrowserCleanup ||
		kind == OperationProjectBrowserHarnessStart || kind == OperationProjectBrowserHarnessStop || kind == OperationProjectBrowserHarnessCleanup ||
		kind == OperationProjectGitFetch || kind == OperationProjectGitFastForwardPreview || kind == OperationProjectGitFastForward ||
		kind == OperationProjectToolboxCreate || kind == OperationProjectToolboxExec || kind == OperationProjectToolboxInstall || kind == OperationProjectToolboxCleanup ||
		kind == OperationProjectToolboxRepair || kind == OperationProjectToolboxServiceStart || kind == OperationProjectToolboxServiceStop
}

func hasProjectExecResult(result OperationResult) bool {
	return result.ExecCompleted || result.ExecExitCode != 0 || result.ExecStdout != "" || result.ExecStderr != "" ||
		result.ExecTimedOut || result.ExecStdoutTruncated || result.ExecStderrTruncated || result.ExecTimingKnown ||
		result.ExecPreflightUS != 0 || result.ExecExecutionUS != 0 || result.ExecResultUS != 0
}

func validProjectExecResult(result OperationResult) bool {
	if !result.ExecCompleted || result.ExecExitCode < -1 || result.ExecExitCode > 255 ||
		result.ExecTimedOut != (result.ExecExitCode == -1) || len(result.ExecStdout) > MaxProjectExecStreamBytes ||
		len(result.ExecStderr) > MaxProjectExecStreamBytes || !utf8.ValidString(result.ExecStdout) || !utf8.ValidString(result.ExecStderr) ||
		strings.ContainsRune(result.ExecStdout, 0) || strings.ContainsRune(result.ExecStderr, 0) {
		return false
	}
	if (!result.ExecTimingKnown && (result.ExecPreflightUS != 0 || result.ExecExecutionUS != 0 || result.ExecResultUS != 0)) ||
		(result.ExecTimingKnown && (result.ExecPreflightUS < 0 || result.ExecExecutionUS < 0 || result.ExecResultUS < 0 ||
			result.ExecPreflightUS > 600_000_000 || result.ExecExecutionUS > 600_000_000 || result.ExecResultUS > 600_000_000)) {
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
	metadata.ExecTimingKnown = false
	metadata.ExecPreflightUS = 0
	metadata.ExecExecutionUS = 0
	metadata.ExecResultUS = 0
	return validProjectOperationResult(metadata)
}
