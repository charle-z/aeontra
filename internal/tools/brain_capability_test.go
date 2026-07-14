package tools

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

func newBrainCapabilityTestService(t *testing.T) (*Service, *brainpkg.Store, *bytes.Buffer, string, string) {
	t.Helper()
	repoRoot := t.TempDir()
	cfg, err := config.New(config.Config{
		Roots:           []string{repoRoot},
		Mode:            config.ModeAllow,
		AllowedCommands: []string{"git", "go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := policy.NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var auditBuffer bytes.Buffer
	service := NewService(pol, audit.New(&auditBuffer), repoRoot)
	brainRoot := filepath.Join(t.TempDir(), "brain")
	store, err := brainpkg.OpenStore(brainRoot, time.Date(2026, 7, 13, 23, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeGit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.OpenIndex(context.Background()); err != nil {
		t.Fatal(err)
	}
	return service, store, &auditBuffer, repoRoot, brainRoot
}

func TestBrainCapabilityDisabledFailsClosedUniformly(t *testing.T) {
	service, _ := newTestService(t, config.ModeReadOnly)
	if service.BrainCapability == nil {
		t.Fatal("Brain capability is nil")
	}
	if service.BrainCapability.Available() {
		t.Fatal("Brain capability unexpectedly available")
	}
	ctx := context.Background()
	checks := []func() error{
		func() error { _, err := service.BrainSearch(ctx, "query", 5); return err },
		func() error { _, err := service.BrainRead(ctx, "safe-note"); return err },
		func() error {
			_, err := service.BrainWrite(ctx, brainpkg.AgentDraft{Slug: "safe-note"})
			return err
		},
		func() error { _, err := service.BrainIndex(ctx, "status"); return err },
		func() error { _, err := service.BrainContext(ctx, 5); return err },
	}
	for index, check := range checks {
		if err := check(); !errors.Is(err, ErrBrainNotConfigured) {
			t.Fatalf("check %d error=%v", index, err)
		}
	}
	if err := service.BrainCapability.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBrainCapabilityUsesIsolatedStoreAndSafeAudit(t *testing.T) {
	service, store, auditBuffer, _, brainRoot := newBrainCapabilityTestService(t)
	defer service.BrainCapability.Close()
	service.WithBrainStore(store)
	if !service.BrainCapability.Available() {
		t.Fatal("Brain capability is unavailable")
	}

	curatedSource := `---
slug: release-gates
title: Release gates
type: fact
author: owner
created: 2026-07-13T22:00:00Z
updated: 2026-07-13T22:30:00Z
provenance: PR 3 and production smoke
---

Staticcheck race CodeQL and container gate.
`
	curatedPath := filepath.Join(brainRoot, brainpkg.CuratedDir, "release-gates.md")
	if err := os.WriteFile(curatedPath, []byte(curatedSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reindex(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.pol.CheckRead(curatedPath); !errors.Is(err, policy.ErrOutsideJail) {
		t.Fatalf("Brain root entered repository policy roots: %v", err)
	}

	ctx := context.Background()
	results, err := service.BrainSearch(ctx, "staticcheck race", 5)
	if err != nil || len(results) != 1 || results[0].Slug != "release-gates" {
		t.Fatalf("search=%+v err=%v", results, err)
	}
	writeBody := "unique-private-body linked to [[release-gates]]."
	written, err := service.BrainWrite(ctx, brainpkg.AgentDraft{
		Slug:       "working-observation",
		Title:      "Working observation",
		Type:       brainpkg.TypeNote,
		Author:     "agent:chatgpt",
		Provenance: "unique-private-provenance",
		ReviewBy:   "2026-08-13",
		Body:       writeBody,
	})
	if err != nil || written.Metadata.Slug != "working-observation" {
		t.Fatalf("write=%+v err=%v", written, err)
	}
	read, err := service.BrainRead(ctx, "release-gates")
	if err != nil || len(read.Backlinks) != 1 || read.Backlinks[0] != "working-observation" {
		t.Fatalf("read=%+v err=%v", read, err)
	}
	status, err := service.BrainIndex(ctx, "status")
	if err != nil || status.NoteCount != 2 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	digest, err := service.BrainContext(ctx, 5)
	if err != nil || !strings.Contains(digest, "release-gates") || strings.Contains(digest, writeBody) {
		t.Fatalf("digest=%q err=%v", digest, err)
	}

	auditText := auditBuffer.String()
	for _, required := range []string{"brain_search", "brain_write", "brain_read", "brain_index", "brain_context"} {
		if !strings.Contains(auditText, required) {
			t.Errorf("audit does not contain %q: %s", required, auditText)
		}
	}
	for _, forbidden := range []string{"staticcheck race", writeBody, "unique-private-provenance", brainRoot} {
		if strings.Contains(auditText, forbidden) {
			t.Errorf("audit leaked %q: %s", forbidden, auditText)
		}
	}
}

func TestBrainCapabilityCloseDisablesAndClosesStore(t *testing.T) {
	service, store, _, _, _ := newBrainCapabilityTestService(t)
	service.WithBrainStore(store)
	if err := service.BrainCapability.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.BrainCapability.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if service.BrainCapability.Available() {
		t.Fatal("Brain capability remained available after close")
	}
	if _, err := service.BrainContext(context.Background(), 1); !errors.Is(err, ErrBrainNotConfigured) {
		t.Fatalf("context after close error=%v", err)
	}
	if _, err := store.IndexStatus(context.Background()); err == nil {
		t.Fatal("store index remained open after service close")
	}
}

func TestBrainCapabilityRejectsUnknownIndexActionWithoutAuditLeak(t *testing.T) {
	service, store, auditBuffer, _, _ := newBrainCapabilityTestService(t)
	defer service.BrainCapability.Close()
	service.WithBrainStore(store)
	canary := "github_pat_0123456789abcdefghijklmnopQRSTUV"
	if _, err := service.BrainIndex(context.Background(), "unknown-"+canary); err == nil || strings.Contains(err.Error(), canary) {
		t.Fatalf("unknown action error=%v", err)
	}
	if strings.Contains(auditBuffer.String(), canary) {
		t.Fatalf("audit leaked action canary: %s", auditBuffer.String())
	}
}
