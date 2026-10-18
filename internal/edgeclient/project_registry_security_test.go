package edgeclient

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRegistryRejectsSymlinkUnsafeModeAndFutureSchema(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{
			name: "symlink",
			setup: func(t *testing.T, state string) {
				target := filepath.Join(t.TempDir(), "projects.db")
				if err := os.WriteFile(target, []byte("not-a-database"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(state, projectRegistryFile)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unsafe mode",
			setup: func(t *testing.T, state string) {
				path := filepath.Join(state, projectRegistryFile)
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`PRAGMA user_version=1`); err != nil {
					t.Fatal(err)
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o666); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "future schema",
			setup: func(t *testing.T, state string) {
				path := filepath.Join(state, projectRegistryFile)
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`PRAGMA user_version=2`); err != nil {
					t.Fatal(err)
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, workspaces, _ := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
			test.setup(t, state)
			registry, err := OpenProjectRegistry(ProjectRegistryConfig{
				StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces,
				Inspector: fixedProjectInspector{state: ProjectCheckoutReady},
			})
			if registry != nil {
				_ = registry.Close()
			}
			if !projectErrorIs(err, ProjectErrorRegistryUnavailable) {
				t.Fatalf("unsafe project registry err=%v", err)
			}
		})
	}
}
