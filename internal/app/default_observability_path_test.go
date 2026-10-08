package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/observability"
)

func TestBuildRuntimeDefaultFileObservabilityUsesPrivateSubdirectory(t *testing.T) {
	clearRuntimeEnv(t)
	root := t.TempDir()
	memoryDir := filepath.Join(root, ".agent-memory")
	if err := os.Mkdir(memoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(memoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.New(config.Config{
		Roots:           []string{root},
		Mode:            config.ModeReadOnly,
		AllowedCommands: []string{"git"},
		SandboxBackend:  "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := buildRuntime(serveOptions{
		Config: cfg,
		Observability: observability.Config{
			Mode:     observability.ModeFile,
			MaxBytes: observability.MinMaxBytes,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Observer.Emit(observability.Event{
		Level:     observability.LevelInfo,
		Component: observability.ComponentServer,
		Name:      observability.EventServerStart,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if pathsOverlap(runtime.StateRoot, root) {
		t.Fatalf("default state root overlaps repository: state=%q repo=%q", runtime.StateRoot, root)
	}
	privateDir := filepath.Join(runtime.StateRoot, "logs")
	filePath := filepath.Join(privateDir, "observability.jsonl")
	dirInfo, err := os.Stat(privateDir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("private dir mode = %o", dirInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o", fileInfo.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(memoryDir, "state")); !os.IsNotExist(err) {
		t.Fatalf("legacy in-repository state was created: %v", err)
	}
}
