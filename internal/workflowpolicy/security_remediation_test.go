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
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile does not contain %q", required)
		}
	}
}
