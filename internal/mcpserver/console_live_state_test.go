package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

type consoleEdgeFixture struct {
	devices []edge.Device
	err     error
}

func (fixture consoleEdgeFixture) DeviceActive(string) bool { return true }
func (fixture consoleEdgeFixture) ActiveDevices() ([]edge.Device, error) {
	return fixture.devices, fixture.err
}

func TestConsoleSelectorsUseOnlyRealOpaqueEntities(t *testing.T) {
	firstRoot := filepath.Join(t.TempDir(), "first-private-project")
	secondRoot := filepath.Join(t.TempDir(), "second-private-project")
	for _, root := range []string{firstRoot, secondRoot} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.New(config.Config{Roots: []string{firstRoot, secondRoot}})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := New(tools.NewService(pol, nil, firstRoot))
	server.edgeDevices = consoleEdgeFixture{devices: []edge.Device{
		{ID: "ed_0123456789abcdef0123456789abcdef", Name: "private-parrot-name", State: edge.StateActive, PairedAt: time.Date(2026, 7, 17, 10, 0, 0, 123, time.UTC)},
		{ID: "ed_1123456789abcdef0123456789abcdef", Name: "private-wsl-name", State: edge.StateActive, PairedAt: time.Date(2026, 7, 17, 11, 0, 0, 456, time.UTC)},
	}}

	projects := server.consoleProjects()
	if len(projects) != 2 || projects[0].ID == projects[1].ID || projects[0].Label != "Configured project 1" || projects[1].Label != "Configured project 2" {
		t.Fatalf("projects=%+v", projects)
	}
	again := server.consoleProjects()
	if projects[0].ID != again[0].ID || projects[1].ID != again[1].ID {
		t.Fatalf("project IDs changed: first=%+v second=%+v", projects, again)
	}
	for _, project := range projects {
		if !strings.HasPrefix(project.ID, "prj_") || strings.Contains(project.ID, "private") || strings.Contains(project.Label, firstRoot) || strings.Contains(project.Label, secondRoot) {
			t.Fatalf("project leaked private source: %+v", project)
		}
	}

	devices, err := server.consoleEdgeDevices()
	if err != nil || len(devices) != 2 || devices[0].ID == devices[1].ID {
		t.Fatalf("devices=%+v err=%v", devices, err)
	}
	for index, device := range devices {
		if !strings.HasPrefix(device.ID, "edge_") || device.Label != "Paired Edge "+string(rune('1'+index)) || device.PairedAt == "" {
			t.Fatalf("device=%+v", device)
		}
		for _, private := range []string{"private-parrot-name", "private-wsl-name", "ed_0123456789abcdef0123456789abcdef", "ed_1123456789abcdef0123456789abcdef"} {
			if strings.Contains(device.ID, private) || strings.Contains(device.Label, private) {
				t.Fatalf("device leaked %q: %+v", private, device)
			}
		}
	}
}

func TestConsoleStorageBudgetCountsDBWALAndLogsOnly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSized := func(path string, size int) {
		t.Helper()
		if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeSized(filepath.Join(root, "metrics.db"), 100)
	writeSized(filepath.Join(root, "metrics.db-wal"), 20)
	writeSized(filepath.Join(root, "metrics.db-shm"), 10)
	writeSized(filepath.Join(root, "nested", "audit.jsonl"), 30)
	writeSized(filepath.Join(root, "nested", "ignored.txt"), 999)
	externalAudit := filepath.Join(t.TempDir(), "external.log")
	writeSized(externalAudit, 40)

	budget := readConsoleStorageBudget(root, externalAudit)
	if !budget.Available || budget.DatabaseBytes != 100 || budget.WALBytes != 30 || budget.LogBytes != 70 || budget.TotalBytes != 200 || budget.LimitBytes != consoleStorageLimitBytes || budget.State != "healthy" {
		t.Fatalf("budget=%+v", budget)
	}
	insideAudit := filepath.Join(root, "nested", "audit.jsonl")
	withoutDoubleCount := readConsoleStorageBudget(root, insideAudit)
	if withoutDoubleCount.LogBytes != 30 || withoutDoubleCount.TotalBytes != 160 {
		t.Fatalf("inside audit counted twice: %+v", withoutDoubleCount)
	}
}

func TestConsoleStorageBudgetRejectsSymlinksAndReportsThresholds(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(root, "large.db")
	if err := os.WriteFile(large, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(large, consoleStorageLimitBytes*3/4); err != nil {
		t.Fatal(err)
	}
	if budget := readConsoleStorageBudget(root, ""); !budget.Available || budget.State != "nearing_limit" {
		t.Fatalf("near budget=%+v", budget)
	}
	if err := os.Truncate(large, consoleStorageLimitBytes*9/10); err != nil {
		t.Fatal(err)
	}
	if budget := readConsoleStorageBudget(root, ""); !budget.Available || budget.State != "degraded" {
		t.Fatalf("degraded budget=%+v", budget)
	}

	outside := filepath.Join(t.TempDir(), "outside.db")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.db")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if budget := readConsoleStorageBudget(root, ""); budget.Available || budget.State != "unavailable" {
		t.Fatalf("symlink budget=%+v", budget)
	}
}
