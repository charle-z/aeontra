package mcpserver

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/oauth"
	"github.com/charle-z/mcp-devbox/internal/observability"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/tools"
)

func TestConsoleDataProviderReportsRealSafeAggregates(t *testing.T) {
	server, _, _ := newObservedServer(t)
	server.payload.record(400, 200)
	if err := server.observer.Emit(observability.Event{
		Name: observability.EventHTTPRequest, Route: observability.RouteMCP, StatusCode: 404, DurationMS: 12,
	}); err != nil {
		t.Fatal(err)
	}
	provider, err := oauth.NewProvider(oauth.Config{Issuer: "http://localhost:8765", Resource: "http://localhost:8765/mcp", Passphrase: "owner-secret"})
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := server.consoleDataProvider(testToken, provider, nil)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.System.Available || snapshot.System.CPUCount < 1 || snapshot.System.MemoryTotalBytes == 0 || snapshot.System.DiskTotalBytes == 0 {
		t.Fatalf("system=%+v", snapshot.System)
	}
	if snapshot.Payload.RequestCount != 1 || snapshot.Payload.InputBytes != 400 || snapshot.Payload.OutputBytes != 200 || snapshot.Payload.InputTokensEstimate != 100 || snapshot.Payload.OutputTokensEstimate != 50 || snapshot.Payload.Formula != "bytes / 4 (estimate)" {
		t.Fatalf("payload=%+v", snapshot.Payload)
	}
	if !snapshot.Observability.Enabled || len(snapshot.Observability.Routes) != 1 {
		t.Fatalf("observability=%+v", snapshot.Observability)
	}
	route := snapshot.Observability.Routes[0]
	if route.Route != "mcp" || route.Requests != 1 || route.Client4XX != 1 || route.Server5XX != 0 || route.P95MS != 12 {
		t.Fatalf("route=%+v", route)
	}
	if !snapshot.Security.OAuthEnabled || !snapshot.Security.BearerRecovery || snapshot.Security.QueryAuth != "rejected" || snapshot.Security.FreeShell != "absent" || snapshot.Security.ConsoleAuthority != "presentation-only" {
		t.Fatalf("security=%+v", snapshot.Security)
	}
	if snapshot.Edge.State != "not_paired" || snapshot.Brain.Available || snapshot.Brain.Nodes == nil || snapshot.Brain.Edges == nil {
		t.Fatalf("edge=%+v brain=%+v", snapshot.Edge, snapshot.Brain)
	}
}

func TestConsoleDataProviderReportsPairedOnlyFromRealEdgeState(t *testing.T) {
	server, _, _ := newObservedServer(t)
	snapshot, err := server.consoleDataProvider("", nil, func() string { return "paired" })(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Edge.State != "paired" {
		t.Fatalf("edge=%+v", snapshot.Edge)
	}
	invalid, err := server.consoleDataProvider("", nil, func() string { return "fabricated" })(context.Background())
	if err != nil || invalid.Edge.State != "not_paired" {
		t.Fatalf("edge=%+v err=%v", invalid.Edge, err)
	}
}

func TestConsoleDataProviderIncludesOpaqueBrainSnapshot(t *testing.T) {
	repoRoot := t.TempDir()
	cfg, err := config.New(config.Config{Roots: []string{repoRoot}, Mode: config.ModeReadOnly, AllowedCommands: []string{"git"}})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	service := tools.NewService(pol, audit.New(&bytes.Buffer{}), pol.Roots()[0])
	brainRoot := filepath.Join(t.TempDir(), "brain")
	store, err := brainpkg.OpenStore(brainRoot, time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := `---
slug: private-console-source
title: Private console source
type: fact
author: owner
created: 2026-07-14T19:00:00Z
updated: 2026-07-14T19:00:00Z
provenance: private test fixture
---

A private body.
`
	if err := os.WriteFile(filepath.Join(brainRoot, brainpkg.CuratedDir, "private-console-source.md"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.WithBrainStore(store)
	server := New(service)

	snapshot, err := server.consoleDataProvider("", nil, nil)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Brain.Available || !snapshot.Brain.Ready || snapshot.Brain.SchemaVersion != 1 || snapshot.Brain.NoteCount != 1 || len(snapshot.Brain.Nodes) != 1 || snapshot.Brain.Nodes[0].ID != "n0001" {
		t.Fatalf("brain=%+v", snapshot.Brain)
	}
	if snapshot.Security.OAuthEnabled || snapshot.Security.BearerRecovery || snapshot.Observability.Enabled {
		t.Fatalf("unexpected optional state: security=%+v observability=%+v", snapshot.Security, snapshot.Observability)
	}
}

func TestReadSystemDataPopulatesRealHostValues(t *testing.T) {
	data := readSystemData()
	if !data.Available || data.CPUCount < 1 || data.MemoryTotalBytes == 0 || data.DiskTotalBytes == 0 || data.Load1 < 0 || data.Load5 < 0 || data.Load15 < 0 {
		t.Fatalf("system=%+v", data)
	}
	var memoryData = data
	memoryData.MemoryTotalBytes = 0
	if !readMemory(&memoryData) || memoryData.MemoryTotalBytes == 0 {
		t.Fatalf("memory=%+v", memoryData)
	}
	var loadData = data
	loadData.Load1, loadData.Load5, loadData.Load15 = 0, 0, 0
	if !readLoad(&loadData) {
		t.Fatalf("load=%+v", loadData)
	}
}
