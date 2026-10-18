package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestProductionDockerfileProtectsTwoVCPUHost(t *testing.T) {
	dockerfileBytes, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	dockerfile := string(dockerfileBytes)

	for _, required := range []string{
		"# syntax=docker/dockerfile:1.7",
		"ARG BUILD_GOMAXPROCS=1",
		"ARG BUILD_UV_THREADPOOL_SIZE=1",
		"ARG BUILD_GO_PARALLELISM=1",
		"GOMAXPROCS=${BUILD_GOMAXPROCS}",
		"UV_THREADPOOL_SIZE=${BUILD_UV_THREADPOOL_SIZE}",
		"go build -p=${BUILD_GO_PARALLELISM}",
		"--mount=type=cache,target=/go/pkg/mod,sharing=locked",
		"--mount=type=cache,target=/root/.cache/go-build,sharing=locked",
		"pnpm console:build",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile does not contain %q", required)
		}
	}

	for _, forbidden := range []string{"pnpm console:check", "pnpm console:test"} {
		if strings.Contains(dockerfile, forbidden) {
			t.Errorf("production image build repeats CI command %q", forbidden)
		}
	}
}

func TestConfigurationDocumentsBuildCPUBudget(t *testing.T) {
	guideBytes, err := os.ReadFile("configuration.md")
	if err != nil {
		t.Fatalf("read configuration guide: %v", err)
	}
	guide := string(guideBytes)
	for _, required := range []string{
		"`BUILD_GOMAXPROCS`",
		"`BUILD_GO_PARALLELISM`",
		"`BUILD_UV_THREADPOOL_SIZE`",
		"dedicated builders may raise",
	} {
		if !strings.Contains(guide, required) {
			t.Errorf("configuration guide does not contain %q", required)
		}
	}
}
