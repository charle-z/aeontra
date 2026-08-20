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
		if !strings.Contains(contents[dockerfile], "golang:1.26.6-") {
			t.Errorf("%s must use the fixed versioned Go base", dockerfile)
		}
	}
	for dockerfile, runtimeBase := range map[string]string{
		"Dockerfile":                        "FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d",
		"Dockerfile.validation-runner":      "FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d",
		"Dockerfile.front-door":             "FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d",
		"Dockerfile.front-door-coordinator": "FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d",
	} {
		if !strings.Contains(contents[dockerfile], runtimeBase) {
			t.Errorf("%s must use the supported Alpine 3.21 runtime with OpenSSL 3.3", dockerfile)
		}
		if !strings.Contains(contents[dockerfile], "apk upgrade --no-cache") {
			t.Errorf("%s must upgrade its pinned Alpine runtime before installing packages", dockerfile)
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
	} {
		if strings.Contains(dockerfile, forbidden) {
			t.Errorf("Dockerfile must not introduce a vulnerable npm bootstrap via %q", forbidden)
		}
	}
	for _, required := range []string{
		"https://registry.npmjs.org/npm/-/npm-12.0.1.tgz",
		"5e02bea4c784df1c3bbea9e55c7d2232329e1d1920c254789833ed9e8b0a5f16",
		"https://registry.npmjs.org/brace-expansion/-/brace-expansion-5.0.9.tgz",
		"5d06001fddd25cbee90c96db4dc5b7b57711b984c3141e28d10f143deb52dbaf",
		"/usr/local/lib/node_modules/npm/node_modules/brace-expansion/package.json",
		"https://registry.npmjs.org/ip-address/-/ip-address-10.3.1.tgz",
		"ad1790063beea11a312c801df30d58e147de762f4f77787552376eb7424623e5",
		"/usr/local/lib/node_modules/npm/node_modules/ip-address/package.json",
		"test ! -e /usr/lib/node_modules/npm",
		"busybox wget -qO- http://127.0.0.1:8765/readyz",
		"COPY --from=build /usr/local/go /usr/local/go",
		"https://unofficial-builds.nodejs.org/download/release/v22.23.2/",
		"2d18b5731055f7efa6c899004909b00ee110e38d3775745f60ec9ccf1f9982e7",
		"86e3f4d05d92c6a4e51b0ce8bab6c22d602d4b8a372743fed302403de5376d4c",
		`test "$(/node/bin/node --version)" = v22.23.2`,
		"COPY --from=node-runtime /node/bin/node /usr/local/bin/node",
		"&& (find / -xdev -perm /6000 -type f -exec chmod a-s {} + 2>/dev/null || true)",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile does not contain %q", required)
		}
	}
	if strings.Contains(dockerfile, "&& find / -xdev -perm /6000 -type f -exec chmod a-s {} + 2>/dev/null || true") {
		t.Error("the setuid cleanup fallback must not mask earlier installation failures")
	}
}
