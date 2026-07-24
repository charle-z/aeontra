package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimePersistsBrainConsoleIdentityUnderStateRoot(t *testing.T) {
	clearRuntimeEnv(t)
	repoRoot := t.TempDir()
	brainRoot := filepath.Join(t.TempDir(), "brain")
	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(brainRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"curated", "working", ".cache"} {
		if err := os.Mkdir(filepath.Join(brainRoot, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	source := fmt.Sprintf(`---
slug: runtime-console-note
title: Runtime console note
type: fact
author: owner
created: %s
updated: %s
provenance: runtime fixture
console_summary: Safe runtime summary.
---

Private runtime body.
`, now.Add(-time.Minute).Format(time.RFC3339), now.Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(brainRoot, "curated", "runtime-console-note.md"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	opts := serveOptions{Config: brainRuntimeConfig(t, repoRoot), BrainRoot: brainRoot, StateRoot: stateRoot}
	first, err := buildRuntime(opts)
	if err != nil {
		t.Fatal(err)
	}
	firstSnapshot, err := first.Service.BrainCapability.BrainConsoleSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := first.Server.CatalogInfo()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.ToolCount != 102 || catalog.Hash != "sha256:5a2091d85585d13eb7efbc22d942b2dfbd71fc7d547581803eb7633cac64d68b" {
		t.Fatalf("catalog=%+v", catalog)
	}
	keyPath := filepath.Join(stateRoot, "brain", "console-node.key")
	keyBefore, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstSnapshot.Nodes) != 1 || firstSnapshot.Nodes[0].Title != "Runtime console note" || firstSnapshot.Nodes[0].Summary != "Safe runtime summary." {
		t.Fatalf("snapshot=%+v", firstSnapshot)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := buildRuntime(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	secondSnapshot, err := second.Service.BrainCapability.BrainConsoleSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	keyAfter, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondSnapshot.Nodes) != 1 || firstSnapshot.Nodes[0].ID != secondSnapshot.Nodes[0].ID || !bytes.Equal(keyBefore, keyAfter) {
		t.Fatalf("identity changed across runtime restart: first=%+v second=%+v", firstSnapshot.Nodes, secondSnapshot.Nodes)
	}
	directoryInfo, err := os.Lstat(filepath.Dir(keyPath))
	if err != nil || directoryInfo.Mode().Perm() != 0o700 || !directoryInfo.IsDir() {
		t.Fatalf("identity directory=%v err=%v", directoryInfo, err)
	}
	keyInfo, err := os.Lstat(keyPath)
	if err != nil || keyInfo.Mode().Perm() != 0o600 || !keyInfo.Mode().IsRegular() || keyInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("identity file=%v err=%v", keyInfo, err)
	}
}
