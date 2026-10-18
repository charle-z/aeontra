package edgeclient

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type pathProjectInspector struct {
	states map[string]ProjectCheckoutState
}

func (p pathProjectInspector) Inspect(_ context.Context, path, _, _ string) (ProjectCheckoutState, error) {
	state, ok := p.states[filepath.Clean(path)]
	if !ok {
		return ProjectCheckoutRemoteMismatch, nil
	}
	return state, nil
}

func TestDiscoverProjectCheckoutReusesCanonicalOrAssociatesOneLegacyCheckout(t *testing.T) {
	for _, test := range []struct {
		name          string
		candidateName string
		wantState     ProjectRecoveryState
	}{
		{name: "canonical", candidateName: "repo", wantState: ProjectRecoveryReuseExisting},
		{name: "legacy", candidateName: "legacy-name", wantState: ProjectRecoveryAssociateExisting},
	} {
		t.Run(test.name, func(t *testing.T) {
			roots := newProjectDiscoveryRoots(t)
			candidate := filepath.Join(roots.Dev, test.candidateName)
			if err := os.Mkdir(candidate, 0o700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(candidate, "preserve.txt")
			if err := os.WriteFile(marker, []byte("unchanged\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			decision, err := DiscoverProjectCheckout(context.Background(), ProjectDiscoveryConfig{
				Roots: roots,
				Inspector: pathProjectInspector{states: map[string]ProjectCheckoutState{
					candidate: ProjectCheckoutReady,
				}},
			}, ProjectDiscoveryRequest{Alias: "Project", Owner: "charle-z", Repository: "repo"})
			if err != nil {
				t.Fatal(err)
			}
			if decision.State != test.wantState || decision.CandidatePath != candidate || decision.CandidateCount != 1 {
				t.Fatalf("decision=%+v", decision)
			}
			if content, err := os.ReadFile(marker); err != nil || string(content) != "unchanged\n" {
				t.Fatalf("discovery mutated candidate: %q err=%v", content, err)
			}
			encoded, err := json.Marshal(decision.SafeStatus())
			if err != nil {
				t.Fatal(err)
			}
			text := string(encoded)
			for _, forbidden := range []string{candidate, roots.Dev, "candidate_path"} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("safe discovery exposed %q: %s", forbidden, text)
				}
			}
			for _, required := range []string{"project", "charle-z/repo", string(test.wantState)} {
				if !strings.Contains(text, required) {
					t.Fatalf("safe discovery missing %q: %s", required, text)
				}
			}
		})
	}
}

func TestDiscoverProjectCheckoutReturnsCloneRequiredWhenNoMatchExists(t *testing.T) {
	roots := newProjectDiscoveryRoots(t)
	if err := os.Mkdir(filepath.Join(roots.Dev, "unrelated"), 0o700); err != nil {
		t.Fatal(err)
	}
	decision, err := DiscoverProjectCheckout(context.Background(), ProjectDiscoveryConfig{
		Roots: roots, Inspector: pathProjectInspector{states: map[string]ProjectCheckoutState{}},
	}, ProjectDiscoveryRequest{Alias: "project", Owner: "charle-z", Repository: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != ProjectRecoveryCloneRequired || decision.CandidatePath != "" || decision.CandidateCount != 0 {
		t.Fatalf("decision=%+v", decision)
	}
	if _, err := os.Lstat(filepath.Join(roots.Dev, "repo")); !os.IsNotExist(err) {
		t.Fatalf("discovery created canonical checkout: %v", err)
	}
}

func TestDiscoverProjectCheckoutBlocksAmbiguousDirtyAndCanonicalMismatch(t *testing.T) {
	for _, test := range []struct {
		name       string
		paths      map[string]ProjectCheckoutState
		wantReason ProjectErrorCode
		wantCount  int
	}{
		{
			name: "ambiguous",
			paths: map[string]ProjectCheckoutState{
				"legacy-a": ProjectCheckoutReady,
				"legacy-b": ProjectCheckoutReady,
			},
			wantReason: ProjectErrorAmbiguousCheckout,
			wantCount:  2,
		},
		{
			name: "dirty legacy",
			paths: map[string]ProjectCheckoutState{
				"legacy": ProjectCheckoutDirty,
			},
			wantReason: ProjectErrorCheckoutDirty,
			wantCount:  1,
		},
		{
			name: "canonical mismatch wins",
			paths: map[string]ProjectCheckoutState{
				"repo":   ProjectCheckoutRemoteMismatch,
				"legacy": ProjectCheckoutReady,
			},
			wantReason: ProjectErrorRepositoryMismatch,
			wantCount:  1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			roots := newProjectDiscoveryRoots(t)
			states := make(map[string]ProjectCheckoutState, len(test.paths))
			for name, state := range test.paths {
				path := filepath.Join(roots.Dev, name)
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				states[path] = state
			}
			decision, err := DiscoverProjectCheckout(context.Background(), ProjectDiscoveryConfig{
				Roots: roots, Inspector: pathProjectInspector{states: states},
			}, ProjectDiscoveryRequest{Alias: "project", Owner: "charle-z", Repository: "repo"})
			if err != nil {
				t.Fatal(err)
			}
			if decision.State != ProjectRecoveryBlocked || decision.Reason != test.wantReason || decision.CandidateCount != test.wantCount || decision.CandidatePath != "" {
				t.Fatalf("decision=%+v", decision)
			}
		})
	}
}

func TestDiscoverProjectCheckoutRejectsUnsafeCanonicalAndBoundedRootOverflow(t *testing.T) {
	t.Run("unsafe canonical", func(t *testing.T) {
		roots := newProjectDiscoveryRoots(t)
		if err := os.Symlink(t.TempDir(), filepath.Join(roots.Dev, "repo")); err != nil {
			t.Fatal(err)
		}
		decision, err := DiscoverProjectCheckout(context.Background(), ProjectDiscoveryConfig{
			Roots: roots, Inspector: pathProjectInspector{states: map[string]ProjectCheckoutState{}},
		}, ProjectDiscoveryRequest{Alias: "project", Owner: "charle-z", Repository: "repo"})
		if err != nil {
			t.Fatal(err)
		}
		if decision.State != ProjectRecoveryBlocked || decision.Reason != ProjectErrorCheckoutUnsafe {
			t.Fatalf("decision=%+v", decision)
		}
	})

	t.Run("entry limit", func(t *testing.T) {
		roots := newProjectDiscoveryRoots(t)
		for _, name := range []string{"a", "b", "c"} {
			if err := os.Mkdir(filepath.Join(roots.Dev, name), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		_, err := DiscoverProjectCheckout(context.Background(), ProjectDiscoveryConfig{
			Roots: roots, MaxEntries: 2, Inspector: pathProjectInspector{states: map[string]ProjectCheckoutState{}},
		}, ProjectDiscoveryRequest{Alias: "project", Owner: "charle-z", Repository: "repo"})
		if !projectErrorIs(err, ProjectErrorDiscoveryLimit) {
			t.Fatalf("discovery limit err=%v", err)
		}
	})
}

func newProjectDiscoveryRoots(t *testing.T) WorkspaceRoots {
	t.Helper()
	home := t.TempDir()
	roots := WorkspaceRoots{Dev: filepath.Join(home, "workspaces"), HTBLinux: filepath.Join(home, "htb-machines")}
	for _, root := range []string{roots.Dev, roots.HTBLinux} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return roots
}
