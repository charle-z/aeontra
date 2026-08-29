package workflowpolicy

import (
	"os"
	"strings"
	"testing"
)

func TestPublicSiteContainerKeepsNarrowRuntimeBoundary(t *testing.T) {
	content, err := os.ReadFile("../../Dockerfile.site")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"COPY cmd/aeontra-site ./cmd/aeontra-site",
		"COPY internal/buildinfo ./internal/buildinfo",
		"COPY internal/landing ./internal/landing",
		"COPY internal/publicsite ./internal/publicsite",
		"-o /out/aeontra-site ./cmd/aeontra-site",
		"ENV AEONTRA_SITE_ADDR=:8080",
		"USER 10001:10001",
		"http://127.0.0.1:8080/healthz",
		`ENTRYPOINT ["/usr/local/bin/aeontra-site"]`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("Dockerfile.site lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"COPY internal ./internal",
		"COPY cmd ./cmd",
		"mcp-devbox serve",
		"Dockerfile.front-door",
		"/var/run/docker.sock",
		"/run/docker.sock",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("Dockerfile.site widens the public-site boundary with %q", forbidden)
		}
	}
}
