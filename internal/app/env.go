package app

import (
	"os"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
)

// tokenEnv is the preferred way to supply the HTTP bearer token (keeps it out of
// the process argument list / shell history).
const tokenEnv = "MCP_DEVBOX_TOKEN"

// Env fallbacks for the test/allowlist commands so a containerized deploy (Coolify)
// can configure them without baking flags into the image. A flag, when set, wins.
const brainRootEnv = "MCP_DEVBOX_BRAIN_ROOT"
const taskRootEnv = "MCP_DEVBOX_TASK_ROOT"
const stateRootEnv = "MCP_DEVBOX_STATE_ROOT"

const (
	testCmdEnv      = "MCP_DEVBOX_TEST_CMD"
	allowCmdEnv     = "MCP_DEVBOX_ALLOW_CMD"
	sandboxEnv      = "MCP_DEVBOX_SANDBOX"
	sandboxImageEnv = "MCP_DEVBOX_SANDBOX_IMAGE"
)

// OAuth env. When both are set, the HTTP transport enables its in-process OAuth
// authorization server (see internal/oauth). publicURLEnv is the public HTTPS base URL
// (the OAuth issuer); the canonical MCP resource used as the token audience is that base
// plus the MCP path.
const (
	publicURLEnv             = "MCP_DEVBOX_PUBLIC_URL"
	oauthPassphraseEnv       = "MCP_DEVBOX_OAUTH_PASSPHRASE"
	oauthClientStorePathEnv  = "MCP_DEVBOX_OAUTH_CLIENT_STORE"
	oauthRefreshStorePathEnv = "MCP_DEVBOX_OAUTH_REFRESH_STORE"
)

const (
	observabilityModeEnv     = "MCP_DEVBOX_OBSERVABILITY"
	observabilityPathEnv     = "MCP_DEVBOX_OBSERVABILITY_PATH"
	observabilityMaxBytesEnv = "MCP_DEVBOX_OBSERVABILITY_MAX_BYTES"
)

const (
	githubTokenEnv             = "GITHUB_TOKEN"
	githubOwnerEnv             = "GITHUB_OWNER"
	githubOwnerTypeEnv         = "GITHUB_OWNER_TYPE"
	githubDefaultVisibilityEnv = "GITHUB_DEFAULT_VISIBILITY"
)

const (
	coolifyURLEnv             = "COOLIFY_URL"
	coolifyAPITokenEnv        = "COOLIFY_API_TOKEN"
	coolifyAllowedAppsEnv     = "COOLIFY_ALLOWED_APPS"
	coolifyServerUUIDEnv      = "COOLIFY_SERVER_UUID"
	coolifyProjectUUIDEnv     = "COOLIFY_PROJECT_UUID"
	coolifyEnvironmentNameEnv = "COOLIFY_ENVIRONMENT_NAME"
	coolifyEnvironmentUUIDEnv = "COOLIFY_ENVIRONMENT_UUID"
	coolifyAllowedDomainsEnv  = "COOLIFY_ALLOWED_DOMAINS"
	coolifyGitHubAppUUIDEnv   = "COOLIFY_GITHUB_APP_UUID"
	coolifyDestinationUUIDEnv = "COOLIFY_DESTINATION_UUID"
	coolifyAllowedMountsEnv   = "COOLIFY_ALLOWED_MOUNTS"
)

const (
	privilegedTasksEnv    = "MCP_DEVBOX_PRIVILEGED_TASKS"
	privilegedServicesEnv = "MCP_DEVBOX_PRIVILEGED_SERVICES"
	privilegedTimeoutEnv  = "MCP_DEVBOX_PRIVILEGED_TIMEOUT"
)

const (
	validationRunnerURLEnv   = "MCP_DEVBOX_VALIDATION_RUNNER_URL"
	validationRunnerTokenEnv = "MCP_DEVBOX_VALIDATION_RUNNER_TOKEN"
)

// envFallback returns flagVal when non-empty (after trimming), otherwise the value
// of the named environment variable.
func envFallback(flagVal, envName string) string {
	if strings.TrimSpace(flagVal) != "" {
		return flagVal
	}
	return os.Getenv(envName)
}

const adminTokenEnv = "MCP_DEVBOX_ADMIN_TOKEN"

// commitEnvVars are consulted (in order) at startup to stamp the running git commit when
// it was not baked in via -ldflags. SOURCE_COMMIT is injected by Coolify at deploy time.
var commitEnvVars = []string{"MCP_DEVBOX_COMMIT", "SOURCE_COMMIT"}

// stampCommit sets the build commit from the environment when present, so /healthz and
// the MCP initialize response report exactly which commit is live even if the image was
// not built with an -ldflags stamp.
func stampCommit() {
	for _, name := range commitEnvVars {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			buildinfo.Commit = v
			return
		}
	}
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitFields(s string) []string { return strings.Fields(strings.TrimSpace(s)) }

func splitSemicolon(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
