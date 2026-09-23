package workflowpolicy

import (
	"os"
	"strings"
	"testing"
)

func TestP6ToolchainAndContainerRemediationStayPinned(t *testing.T) {
	files := map[string]string{
		"go.mod":                            "../../go.mod",
		"ci.yml":                            "../../.github/workflows/ci.yml",
		"security.yml":                      "../../.github/workflows/security.yml",
		"fuzz.yml":                          "../../.github/workflows/fuzz.yml",
		"Dockerfile":                        "../../Dockerfile",
		"Dockerfile.validation-runner":      "../../Dockerfile.validation-runner",
		"Dockerfile.front-door":             "../../Dockerfile.front-door",
		"Dockerfile.front-door-coordinator": "../../Dockerfile.front-door-coordinator",
	}
	contents := make(map[string]string, len(files))
	for name, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		contents[name] = string(content)
	}

	if !strings.Contains(contents["go.mod"], "go 1.26.6") {
		t.Error("go.mod must require the Go 1.26.6 security release")
	}
	for _, workflow := range []string{"ci.yml", "security.yml", "fuzz.yml"} {
		if !strings.Contains(contents[workflow], `go-version: "1.26.6"`) {
			t.Errorf("%s must use Go 1.26.6", workflow)
		}
	}
	for _, dockerfile := range []string{"Dockerfile", "Dockerfile.validation-runner", "Dockerfile.front-door", "Dockerfile.front-door-coordinator"} {
		if !strings.Contains(contents[dockerfile], "golang:1.26.6-alpine3.24") {
			t.Errorf("%s must use the fixed versioned Go/Alpine base", dockerfile)
		}
	}

	dockerfile := contents["Dockerfile"]
	if strings.Contains(dockerfile, "apk add --no-cache ca-certificates git nodejs npm wget") {
		t.Error("the final image must not install the vulnerable GNU wget package")
	}
	for _, forbidden := range []string{
		"apk add --no-cache ca-certificates git nodejs npm",
		"npm install --global npm@12.0.1",
		"apk del npm",
	} {
		if strings.Contains(dockerfile, forbidden) {
			t.Errorf("Dockerfile must not introduce a vulnerable npm bootstrap via %q", forbidden)
		}
	}
	for _, required := range []string{
		"FROM cgr.dev/chainguard/wolfi-base:latest@sha256:1d95114038f76513a9ace6fca107d5582b08c65981f81f61cb56bf7fd2ef216d",
		"npm pack --ignore-scripts --pack-destination /tmp npm@12.0.1",
		"5e02bea4c784df1c3bbea9e55c7d2232329e1d1920c254789833ed9e8b0a5f16",
		"npm pack --ignore-scripts --pack-destination /tmp brace-expansion@5.0.9",
		"5d06001fddd25cbee90c96db4dc5b7b57711b984c3141e28d10f143deb52dbaf",
		"/usr/local/lib/node_modules/npm/node_modules/brace-expansion/package.json",
		"npm pack --ignore-scripts --pack-destination /tmp ip-address@10.3.1",
		"ad1790063beea11a312c801df30d58e147de762f4f77787552376eb7424623e5",
		"/usr/local/lib/node_modules/npm/node_modules/ip-address/package.json",
		"npm pack --ignore-scripts --pack-destination /tmp tar@7.5.21",
		"bcedf25a21daecd1a18fb5e19ab855b7d79ec8ef1da175e8ba85cfc0ed0069d1",
		"/usr/local/lib/node_modules/npm/node_modules/tar/package.json",
		"test ! -e /usr/lib/node_modules/npm",
		"curl -fsS --max-time 2 http://127.0.0.1:8765/readyz",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile does not contain %q", required)
		}
	}
}

func TestFrontDoorRuntimeUsesPinnedFixedBase(t *testing.T) {
	data, err := os.ReadFile("../../Dockerfile.front-door")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(data)
	for _, required := range []string{
		"FROM golang:1.26.6-alpine3.24@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build",
		"FROM cgr.dev/chainguard/wolfi-base:latest@sha256:1d95114038f76513a9ace6fca107d5582b08c65981f81f61cb56bf7fd2ef216d",
		"apk upgrade --no-cache",
		"apk add --no-cache ca-certificates curl",
		"addgroup -S -g 10002 mcpfront",
		"USER 10002:10002",
		"curl -fsS --max-time 2 http://127.0.0.1:8765/front-door/healthz",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile.front-door missing %q", required)
		}
	}
}
