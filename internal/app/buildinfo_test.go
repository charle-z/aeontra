package app

import (
	"testing"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
)

func TestStampCommitPreservesDeploymentEnvContract(t *testing.T) {
	old := buildinfo.Commit
	buildinfo.Commit = "unknown"
	defer func() { buildinfo.Commit = old }()

	t.Setenv("MCP_DEVBOX_COMMIT", "env-commit")
	t.Setenv("SOURCE_COMMIT", "coolify-commit")
	stampCommit()

	if buildinfo.Commit != "env-commit" {
		t.Fatalf("commit = %q, want MCP_DEVBOX_COMMIT precedence", buildinfo.Commit)
	}
}
