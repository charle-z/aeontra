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
