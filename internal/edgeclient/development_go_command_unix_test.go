//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
)

type goManifestSourceRunner struct {
	tracked []string
	err     error
}

func (r goManifestSourceRunner) Run(_ context.Context, _ string, args []string, credential GitHubCredential) (string, error) {
	if !reflect.DeepEqual(args, []string{"ls-tree", "-r", "--full-tree", "-z", "--name-only", "HEAD"}) || credential != (GitHubCredential{}) {
		return "", errors.New("unexpected inventory authority")
	}
	if r.err != nil {
		return "", r.err
	}
	return strings.Join(r.tracked, "\x00") + "\x00", nil
}

func goManifestRequirementsFixture(t *testing.T, files map[string]string) (string, []development.CapabilityID, goManifestSourceRunner) {
	t.Helper()
	root := t.TempDir()
	runner := goManifestSourceRunner{}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		runner.tracked = append(runner.tracked, name)
	}
	readiness, err := DetectToolchainReadiness(root)
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := DevelopmentRequirementsFromToolchainReadiness(readiness)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]development.CapabilityID, 0, len(requirements))
	for _, requirement := range requirements {
		ids = append(ids, requirement.ID)
	}
	return root, ids, runner
}

func TestDevelopmentGoCommandRequirementsPreserveProvenance(t *testing.T) {
	root, conservative, runner := goManifestRequirementsFixture(t, map[string]string{
		"go.mod":  "module example.test/project\ngo 1.26.6\ntoolchain go1.26.9\n",
		"go.work": "go 1.26.8\nuse .\n", ".tool-versions": "go 1.26.8\n",
		"mise.toml":    "[tools]\ngolang = '1.26.8'\n",
		"package.json": `{"packageManager":"pnpm@10.13.1"}`,
	})
	evidence, err := DevelopmentGoCommandRequirements(t.Context(), root, "sha256:"+strings.Repeat("a", 64), conservative, runner)
	if err != nil || evidence == nil || !reflect.DeepEqual(evidence.MinimumVersions, []development.GoMinimum{{Manifest: "go.mod", Version: "1.26.6"}, {Manifest: "go.work", Version: "1.26.8"}}) ||
		!reflect.DeepEqual(evidence.ExactRequirements, []development.CapabilityID{"toolchain.go.v1-26-8"}) {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
	// go.mod's suggested toolchain is not a minimum or an exact runtime pin;
	// the registered provider disables toolchain auto-selection.
	if !reflect.DeepEqual(conservative, []development.CapabilityID{"toolchain.go.v1-26-6", "toolchain.go.v1-26-8", "toolchain.pnpm.v10-13-1"}) {
		t.Fatalf("generic requirements changed: %v", conservative)
	}
}

func TestDevelopmentGoCommandRequirementsRejectMalformedAndUncommittedEvidence(t *testing.T) {
	for _, content := range []string{"go nope\n", "go 1.26.6\ngo 1.26.8\n", "go 01.26.6\n", "go 1.26.6 # invalid\n"} {
		t.Run(content, func(t *testing.T) {
			root, conservative, runner := goManifestRequirementsFixture(t, map[string]string{"go.mod": "module example.test/project\ngo 1.26.6\n"})
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := DevelopmentGoCommandRequirements(t.Context(), root, "sha256:"+strings.Repeat("a", 64), conservative, runner); err == nil {
				t.Fatal("malformed minimum accepted")
			}
		})
	}
	for _, name := range []string{"go.mod", "go.work", ".tool-versions", "mise.toml"} {
		t.Run("ignored-"+name, func(t *testing.T) {
			root, conservative, runner := goManifestRequirementsFixture(t, map[string]string{"go.mod": "go 1.26.6\n", "go.work": "go 1.26.6\n", ".tool-versions": "go 1.26.8\n", "mise.toml": "[tools]\ngo='1.26.8'\n"})
			for i, tracked := range runner.tracked {
				if tracked == name {
					runner.tracked = append(runner.tracked[:i], runner.tracked[i+1:]...)
					break
				}
			}
			evidence, err := DevelopmentGoCommandRequirements(t.Context(), root, "sha256:"+strings.Repeat("a", 64), conservative, runner)
			if err != nil || evidence != nil {
				t.Fatalf("ignored/untracked metadata did not retain conservative fallback: %+v %v", evidence, err)
			}
		})
	}
}

func TestDevelopmentGoCommandRequirementsRejectChangedConflictAndUnsupportedPins(t *testing.T) {
	for name, content := range map[string]string{
		".tool-versions": "go 1.26.8 1.26.6\n",
		"mise.toml":      "[tools]\ngo={version='1.26.8'}\n",
	} {
		t.Run(name, func(t *testing.T) {
			root, conservative, runner := goManifestRequirementsFixture(t, map[string]string{"go.mod": "go 1.26.6\n", name: map[string]string{".tool-versions": "go 1.26.8\n", "mise.toml": "[tools]\ngo='1.26.8'\n"}[name]})
			if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := DevelopmentGoCommandRequirements(t.Context(), root, "sha256:"+strings.Repeat("a", 64), conservative, runner); err == nil {
				t.Fatal("unsupported manager pin silently dropped")
			}
		})
	}
	root, conservative, runner := goManifestRequirementsFixture(t, map[string]string{"go.mod": "go 1.26.6\n", ".tool-versions": "go 1.26.8\n", "mise.toml": "[tools]\ngo='1.26.8'\n"})
	if err := os.WriteFile(filepath.Join(root, "mise.toml"), []byte("[tools]\ngo='1.26.9'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DevelopmentGoCommandRequirements(t.Context(), root, "sha256:"+strings.Repeat("a", 64), conservative, runner); err == nil {
		t.Fatal("conflicting pins accepted")
	}
}

func TestDevelopmentGoCommandRequirementsMissingEvidenceStaysConservative(t *testing.T) {
	for _, files := range []map[string]string{{}, {"go.mod": "module example.test/legacy\n"}} {
		root, conservative, _ := goManifestRequirementsFixture(t, files)
		evidence, err := DevelopmentGoCommandRequirements(t.Context(), root, "sha256:"+strings.Repeat("a", 64), conservative, nil)
		if err != nil || evidence != nil {
			t.Fatalf("legacy source gained inferred evidence: %+v %v", evidence, err)
		}
	}
}

func TestDevelopmentGoCommandRequirementsUseCanonicalDirectiveComments(t *testing.T) {
	root, conservative, runner := goManifestRequirementsFixture(t, map[string]string{"go.mod": "module example.test/project\ngo 1.26.6 // minimum for this module\n"})
	evidence, err := DevelopmentGoCommandRequirements(t.Context(), root, "sha256:"+strings.Repeat("a", 64), conservative, runner)
	if err != nil || evidence == nil || evidence.MinimumVersions[0].Version != "1.26.6" {
		t.Fatalf("canonical commented directive was not retained: %+v %v", evidence, err)
	}
}

func TestDevelopmentIgnoredGoWorkspaceCannotAcquireSourceBinding(t *testing.T) {
	root, conservative, runner := goManifestRequirementsFixture(t, map[string]string{"go.mod": "module example.test/project\ngo 1.26.6\n", "go.work": "go 1.26.6\n"})
	runner.tracked = []string{"go.mod"} // go.work is ignored by the selected Git inventory.
	digest := func() string {
		t.Helper()
		value, err := projectSourceContentDigestWithPolicy(t.Context(), root, func([]string) ([]string, error) {
			return runner.tracked, nil
		}, func() error { return nil }, registeredProjectSourceContentPolicy())
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := digest()
	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.26.9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after := digest()
	if before != after {
		t.Fatal("fixture ignored input unexpectedly covered by fingerprint")
	}
	evidence, err := DevelopmentGoCommandRequirements(t.Context(), root, after, conservative, runner)
	if err != nil || evidence != nil {
		t.Fatalf("ignored Go workspace acquired a false source binding: %+v %v", evidence, err)
	}
}
