package workflowpolicy

import (
	"os"
	"strings"
	"testing"
)

func TestBackendBuildContextExcludesNestedNodeModules(t *testing.T) {
	content, err := os.ReadFile("../../.dockerignore")
	if err != nil {
		t.Fatal(err)
	}
	patterns := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	for _, required := range []string{"node_modules", "**/node_modules"} {
		found := false
		for _, pattern := range patterns {
			found = found || strings.TrimSpace(pattern) == required
		}
		if !found {
			t.Errorf("Docker context must exclude %q to keep host package-manager links out of the build", required)
		}
	}
}

func TestBackendRuntimeKeepsNodeWithoutNPM(t *testing.T) {
	content, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(content)
	runtimeStart := strings.LastIndex(dockerfile, "\nFROM ")
	if runtimeStart < 0 {
		t.Fatal("Dockerfile has no final runtime stage")
	}
	runtime := dockerfile[runtimeStart:]
	for _, required := range []string{
		"apk add --no-cache ca-certificates curl git libstdc++ nodejs-22",
		`test "$(node --version)" = v22.23.2`,
		"test ! -e /usr/local/lib/node_modules/npm",
		"test ! -e /usr/lib/node_modules/npm",
		"! command -v npm",
		"! command -v npx",
		"COPY --from=build /usr/local/go /usr/local/go",
		"USER 10001:10001",
		"curl -fsS --max-time 2 http://127.0.0.1:8765/readyz",
	} {
		if !strings.Contains(runtime, required) {
			t.Errorf("backend runtime must contain %q", required)
		}
	}
	for _, forbidden := range []string{
		"AS node-runtime",
		"npm pack",
		"COPY --from=node-runtime",
		"npm-cli.js",
		"npx-cli.js",
	} {
		if strings.Contains(dockerfile, forbidden) {
			t.Errorf("backend must not assemble or copy an unused npm tree via %q", forbidden)
		}
	}
	for _, required := range []string{
		"corepack prepare pnpm@10.13.1 --activate",
		"pnpm install --frozen-lockfile --ignore-scripts",
		"pnpm console:build",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("console build must retain %q", required)
		}
	}
}

func TestWolfiRuntimesSelectFixedOpenSSL(t *testing.T) {
	for _, name := range []string{"Dockerfile", "Dockerfile.site", "Dockerfile.front-door", "Dockerfile.front-door-coordinator", "Dockerfile.validation-runner", "Dockerfile.sandbox-workcell"} {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile("../../" + name)
			if err != nil {
				t.Fatal(err)
			}
			runtime := string(content)
			if start := strings.LastIndex(runtime, "\nFROM "); start >= 0 {
				runtime = runtime[start:]
			}
			for _, required := range []string{"libssl3=3.6.5-r1", "libcrypto3=3.6.5-r1"} {
				if !strings.Contains(runtime, required) {
					t.Errorf("final runtime must select the corrected OpenSSL package %q", required)
				}
			}
		})
	}
}

func TestP6ToolchainAndContainerRemediationStayPinned(t *testing.T) {
	files := map[string]string{
		"go.mod":                            "../../go.mod",
		"ci.yml":                            "../../.github/workflows/ci.yml",
		"security.yml":                      "../../.github/workflows/security.yml",
		"fuzz.yml":                          "../../.github/workflows/fuzz.yml",
		"Dockerfile":                        "../../Dockerfile",
		"Dockerfile.site":                   "../../Dockerfile.site",
		"Dockerfile.validation-runner":      "../../Dockerfile.validation-runner",
		"Dockerfile.front-door":             "../../Dockerfile.front-door",
		"Dockerfile.front-door-coordinator": "../../Dockerfile.front-door-coordinator",
		"Dockerfile.sandbox-runner":         "../../Dockerfile.sandbox-runner",
		"test/opencode-e2e/Dockerfile":      "../../test/opencode-e2e/Dockerfile",
	}
	contents := make(map[string]string, len(files))
	for name, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		contents[name] = string(content)
	}

	if !strings.Contains(contents["go.mod"], "go 1.26.9") {
		t.Error("go.mod must require the Go 1.26.9 security release")
	}
	for _, workflow := range []string{"ci.yml", "security.yml", "fuzz.yml"} {
		if !strings.Contains(contents[workflow], `go-version: "1.26.9"`) {
			t.Errorf("%s must use Go 1.26.9", workflow)
		}
	}
	for _, dockerfile := range []string{"Dockerfile", "Dockerfile.site", "Dockerfile.validation-runner", "Dockerfile.front-door", "Dockerfile.front-door-coordinator", "Dockerfile.sandbox-runner", "test/opencode-e2e/Dockerfile"} {
		if !strings.Contains(contents[dockerfile], "golang:1.26.9-") {
			t.Errorf("%s must use the fixed versioned Go base", dockerfile)
		}
	}
	for dockerfile, runtimeBase := range map[string]string{
		"Dockerfile":                        "FROM cgr.dev/chainguard/wolfi-base:latest@sha256:1d95114038f76513a9ace6fca107d5582b08c65981f81f61cb56bf7fd2ef216d",
		"Dockerfile.site":                   "FROM cgr.dev/chainguard/wolfi-base:latest@sha256:1d95114038f76513a9ace6fca107d5582b08c65981f81f61cb56bf7fd2ef216d",
		"Dockerfile.validation-runner":      "FROM cgr.dev/chainguard/wolfi-base:latest@sha256:1d95114038f76513a9ace6fca107d5582b08c65981f81f61cb56bf7fd2ef216d",
		"Dockerfile.front-door":             "FROM cgr.dev/chainguard/wolfi-base:latest@sha256:1d95114038f76513a9ace6fca107d5582b08c65981f81f61cb56bf7fd2ef216d",
		"Dockerfile.front-door-coordinator": "FROM cgr.dev/chainguard/wolfi-base:latest@sha256:1d95114038f76513a9ace6fca107d5582b08c65981f81f61cb56bf7fd2ef216d",
	} {
		if !strings.Contains(contents[dockerfile], runtimeBase) {
			t.Errorf("%s must use its reviewed digest-pinned runtime", dockerfile)
		}
		if !strings.Contains(contents[dockerfile], "apk upgrade --no-cache") {
			t.Errorf("%s must upgrade its pinned runtime before installing packages", dockerfile)
		}
		if !strings.Contains(contents[dockerfile], "HEALTHCHECK --interval=10s --timeout=10s") {
			t.Errorf("%s must allow the bounded healthcheck to start under host load", dockerfile)
		}
	}
	for dockerfile, healthURL := range map[string]string{
		"Dockerfile.site":                   "http://127.0.0.1:8080/healthz",
		"Dockerfile.front-door":             "http://127.0.0.1:8765/front-door/healthz",
		"Dockerfile.front-door-coordinator": "http://127.0.0.1:8766/readyz",
		"Dockerfile.validation-runner":      "http://127.0.0.1:8787/healthz",
	} {
		if !strings.Contains(contents[dockerfile], "apk add --no-cache ca-certificates curl") {
			t.Errorf("%s must install curl for its healthcheck", dockerfile)
		}
		if !strings.Contains(contents[dockerfile], "curl -fsS --max-time 2 "+healthURL) {
			t.Errorf("%s must check its own health endpoint", dockerfile)
		}
	}
	if !strings.Contains(contents["Dockerfile.validation-runner"], "COPY go.mod go.sum ./") {
		t.Error("Dockerfile.validation-runner must bind dependency downloads to go.sum")
	}
	for _, required := range []string{
		"DOCKER_CLI_VERSION=29.7.2",
		"DOCKER_CLI_COMMIT=a7dcaa6fdb6ed04aacbfdc76357fdae01605609e",
		"DOCKER_CLI_SOURCE_SHA256=225b7ab2a15f5230b482df8461069cd4bce38891266fb9898d4188d0a3cbf54a",
		"CGO_ENABLED=0 GO111MODULE=auto go build",
		"test \"$(go version -m /out/docker",
		"COPY --from=docker-cli /go/src/github.com/docker/cli/LICENSE /usr/share/licenses/docker-cli/LICENSE",
		"COPY --from=docker-cli /go/src/github.com/docker/cli/NOTICE /usr/share/licenses/docker-cli/NOTICE",
	} {
		if !strings.Contains(contents["Dockerfile.validation-runner"], required) {
			t.Errorf("Dockerfile.validation-runner does not contain %q", required)
		}
	}
	if strings.Contains(contents["Dockerfile.validation-runner"], "FROM docker:") {
		t.Error("Dockerfile.validation-runner must not inherit a Docker CLI built with a vulnerable Go toolchain")
	}

	dockerfile := contents["Dockerfile"]
	if strings.Contains(dockerfile, "apk add --no-cache ca-certificates git nodejs npm wget") {
		t.Error("the final image must not install the vulnerable GNU wget package")
	}
	for _, forbidden := range []string{
		"apk add --no-cache ca-certificates git nodejs npm",
		"npm install --global npm@12.0.1",
		"apk del npm",
		"unofficial-builds.nodejs.org",
		`busybox wget -qO "$npm_archive"`,
		`busybox wget -qO "$brace_archive"`,
		`busybox wget -qO "$ip_archive"`,
		`busybox wget -qO "$tar_archive"`,
	} {
		if strings.Contains(dockerfile, forbidden) {
			t.Errorf("Dockerfile must not introduce a vulnerable npm bootstrap via %q", forbidden)
		}
	}
	for _, required := range []string{
		"test ! -e /usr/local/lib/node_modules/npm",
		"test ! -e /usr/lib/node_modules/npm",
		"curl -fsS --max-time 2 http://127.0.0.1:8765/readyz",
		"COPY --from=build /usr/local/go /usr/local/go",
		`test "$(node --version)" = v22.23.2`,
		"apk add --no-cache ca-certificates curl git libstdc++ nodejs-22",
		"&& (find / -xdev -perm /6000 -type f -exec chmod a-s {} + 2>/dev/null || true)",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile does not contain %q", required)
		}
	}
	if strings.Contains(dockerfile, "COPY --from=node-runtime /usr/local/bin/node /usr/local/bin/node") {
		t.Error("Dockerfile must not copy a musl-linked Node executable into the Wolfi runtime")
	}
	if strings.Contains(dockerfile, "&& find / -xdev -perm /6000 -type f -exec chmod a-s {} + 2>/dev/null || true") {
		t.Error("the setuid cleanup fallback must not mask earlier installation failures")
	}
}
