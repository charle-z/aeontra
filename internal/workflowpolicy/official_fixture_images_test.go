package workflowpolicy

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestOfficialCIFixtureImagesUseImmutableRegistryReferences(t *testing.T) {
	for _, tc := range []struct {
		name, path, pattern, old string
	}{
		{"debian", "../../.github/workflows/p15-edge.yml", `public\.ecr\.aws/docker/library/debian@sha256:[a-f0-9]{64} sh -euxc`, "debian:bookworm-slim sh -euxc"},
		{"postgres", "../../.github/workflows/trusted-linux-workcell-e2e.yml", `P12_POSTGRES_IMAGE: public\.ecr\.aws/docker/library/postgres@sha256:18cfe3ef5e6815560c98237d6216d1e5119702fb0f3894c8785dd58b8bbe5d73`, "P12_POSTGRES_IMAGE: docker.io/"},
		{"alpine", "../edgeclient/trusted_linux_workcell_rootless_cycles_e2e_test.go", `FROM public\.ecr\.aws/docker/library/alpine@sha256:[a-f0-9]{64}\\nCOPY fixture`, "FROM docker.io/library/alpine:3.20"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if !regexp.MustCompile(tc.pattern).Match(body) {
				t.Error("fixture must use an immutable official registry reference")
			}
			if strings.Contains(string(body), tc.old) {
				t.Error("fixture still depends on the previous Docker Hub reference")
			}
		})
	}
}

func TestContainerBuildsUseOfficialMirrorsWithoutChangingPinnedContent(t *testing.T) {
	paths := []string{
		"../../Dockerfile", "../../Dockerfile.site", "../../Dockerfile.front-door",
		"../../Dockerfile.front-door-coordinator", "../../Dockerfile.validation-runner",
		"../../Dockerfile.sandbox-runner", "../../test/opencode-e2e/Dockerfile",
	}
	pins := map[string]string{
		"node:22-alpine3.22":       "cd7807368cf24826297cbad5dca1a44972ccfd770647db52a8c7589eb4599ac8",
		"node:24-bookworm-slim":    "3638d9a6fe4030bd716be989438248074489337ba3275657f93595428be4fc03",
		"golang:1.26.9-alpine3.24": "cdfd4fe2da6b225d8b40c6b7a105736e548e83ff56d5d8f9394446eeb5eb84e0",
		"golang:1.26.9-bookworm":   "d9c68c2c51161e12fd77e4c6320687c9cd86e1af1e3ad6e6cd63ff970641453c",
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || fields[0] != "FROM" || strings.HasPrefix(fields[1], "cgr.dev/") || fields[1] == "scratch" {
				continue
			}
			const prefix = "public.ecr.aws/docker/library/"
			image := fields[1]
			if !strings.HasPrefix(image, prefix) {
				t.Errorf("%s: builder image must use the official mirror: %s", path, image)
				continue
			}
			name, digest, ok := strings.Cut(strings.TrimPrefix(image, prefix), "@sha256:")
			if !ok || pins[name] == "" || digest != pins[name] {
				t.Errorf("%s: mirror changed reviewed image content: %s", path, image)
			}
		}
	}
}
