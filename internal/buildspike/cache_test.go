//go:build !windows

package buildspike

import (
	"strings"
	"testing"
)

func TestRenderBuildkitConfigIsRootlessBoundedAndNoInsecureEntitlements(t *testing.T) {
	config := DefaultConfig(1001)
	body, err := RenderBuildkitConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		`root = "/var/lib/mcp-devbox-buildkit"`,
		`address = ["unix:///run/mcp-devbox-buildkit/buildkit/buildkitd.sock"]`,
		`rootless = true`,
		`noProcessSandbox = false`,
		`max-parallelism = 1`,
		`gc = true`,
		`reservedSpace = "1GB"`,
		`maxUsedSpace = "4GB"`,
		`minFreeSpace = "8GB"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("config missing %q:\n%s", required, text)
		}
	}
	for _, forbidden := range []string{
		"security.insecure", "network.host", "tcp://", "0.0.0.0", "/run/buildkit", "noProcessSandbox = true",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("config contains forbidden %q:\n%s", forbidden, text)
		}
	}
	if len(body) > 16<<10 {
		t.Fatalf("config too large: %d", len(body))
	}
}

func TestBuildCommandUsesReusableBoundedCacheWithoutNoCacheMutation(t *testing.T) {
	request, config := validPlanFixture(t)
	cached, _, err := BuildCommand(config, request)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cached.Args, "\n")
	for _, required := range []string{
		"--export-cache", "type=local,dest=/var/cache/mcp-devbox-buildkit/export,mode=max,reset=true",
		"--import-cache", "type=local,src=/var/cache/mcp-devbox-buildkit/export",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("cached plan missing %q: %v", required, cached.Args)
		}
	}
	if strings.Contains(joined, "--no-cache") {
		t.Fatalf("cached plan unexpectedly disables cache: %v", cached.Args)
	}
	request.NoCache = true
	uncached, _, err := BuildCommand(config, request)
	if err != nil {
		t.Fatal(err)
	}
	if !containsArgument(uncached.Args, "--no-cache") {
		t.Fatalf("uncached plan lacks --no-cache: %v", uncached.Args)
	}
}

func validPlanFixture(t *testing.T) (BuildRequest, Config) {
	t.Helper()
	root := t.TempDir()
	workspaceRoot := root + "/workspaces"
	contextPath := workspaceRoot + "/project"
	outputRoot := root + "/results"
	if err := makePrivateDirs(contextPath, outputRoot); err != nil {
		t.Fatal(err)
	}
	return BuildRequest{
		WorkspaceRoot: workspaceRoot,
		ContextPath:   contextPath,
		Dockerfile:    "Dockerfile",
		OutputRoot:    outputRoot,
		Commit:        "0123456789abcdef0123456789abcdef01234567",
	}, DefaultConfig(1001)
}

func containsArgument(arguments []string, expected string) bool {
	for _, argument := range arguments {
		if argument == expected {
			return true
		}
	}
	return false
}
