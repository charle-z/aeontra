package docs_test

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var pinnedContainerImage = regexp.MustCompile(`^[a-z0-9./_-]+:[a-zA-Z0-9._-]+@sha256:[a-f0-9]{64}$`)
var inlineDockerfileBaseImage = regexp.MustCompile(`FROM[ \t]+([a-z0-9./_-]+:[a-zA-Z0-9._-]+(?:@sha256:[a-f0-9]{64})?)`)

func TestEveryDockerfileBaseImageIsPinnedByDigest(t *testing.T) {
	for _, path := range []string{
		"../Dockerfile",
		"../Dockerfile.front-door",
		"../Dockerfile.front-door-coordinator",
		"../Dockerfile.validation-runner",
		"../test/opencode-e2e/Dockerfile",
	} {
		file, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "FROM ") {
				continue
			}
			image := strings.Fields(line)[1]
			if !pinnedContainerImage.MatchString(image) {
				t.Errorf("%s contains unpinned base image %q", path, image)
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("scan %s: %v", path, err)
		}
		_ = file.Close()
	}
}

func TestWorkflowInlineDockerfileBaseImagesArePinnedByDigest(t *testing.T) {
	paths, err := filepath.Glob("../.github/workflows/*.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, match := range inlineDockerfileBaseImage.FindAllStringSubmatch(string(content), -1) {
			if !pinnedContainerImage.MatchString(match[1]) {
				t.Errorf("%s contains unpinned inline base image %q", path, match[1])
			}
		}
	}
}

func TestRootlessPostgresFixtureIsPinnedByDigest(t *testing.T) {
	workflow := readDoc(t, "../.github/workflows/trusted-linux-workcell-e2e.yml")
	for _, marker := range []string{
		"P12_POSTGRES_IMAGE: docker.io/library/postgres:17-alpine@sha256:",
		"docker pull \"$P12_POSTGRES_IMAGE\"",
		"docker image inspect --format '{{.Id}}' \"$P12_POSTGRES_IMAGE\"",
	} {
		if !strings.Contains(workflow, marker) {
			t.Errorf("rootless workflow missing pinned fixture contract %q", marker)
		}
	}
}
