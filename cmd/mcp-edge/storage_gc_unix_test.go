//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func testStorageGCPlan(t *testing.T) storageGCPlan {
	t.Helper()
	createdAt := time.Now().UTC().Add(-time.Minute)
	return storageGCPlan{
		Version:       1,
		ID:            "sgp_" + strings.Repeat("a", 32),
		CreatedAt:     createdAt,
		ExpiresAt:     createdAt.Add(5 * time.Minute),
		MinAgeSeconds: 3600,
		Candidates: []storageGCCandidate{{
			Kind:        storageGCCandidateBrowser,
			Alias:       "project",
			Target:      "parrot",
			WorkspaceID: "ws_" + strings.Repeat("b", 32),
			Generation:  1,
			ResourceID:  "br_" + strings.Repeat("c", 32),
			ObservedAt:  createdAt.Add(-2 * time.Hour),
		}},
	}
}

func TestStorageGCPlanRoundTripAndSingleUse(t *testing.T) {
	stateRoot := t.TempDir()
	plan := testStorageGCPlan(t)
	if err := writeStorageGCPlan(stateRoot, plan); err != nil {
		t.Fatal(err)
	}
	loaded, err := readStorageGCPlan(stateRoot, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != plan.ID || loaded.MinAgeSeconds != plan.MinAgeSeconds || len(loaded.Candidates) != 1 {
		t.Fatalf("loaded=%+v", loaded)
	}
	if err := consumeStorageGCPlan(stateRoot, plan.ID); err != nil {
		t.Fatal(err)
	}
	if err := consumeStorageGCPlan(stateRoot, plan.ID); err == nil {
		t.Fatal("gc plan reuse accepted")
	}
}

func TestStorageGCPlanRejectsSymlinkRootAndFile(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		stateRoot := t.TempDir()
		if err := os.Symlink(t.TempDir(), filepath.Join(stateRoot, "storage-gc-plans")); err != nil {
			t.Fatal(err)
		}
		if _, err := storageGCPlanPath(stateRoot, "sgp_"+strings.Repeat("a", 32)); err == nil {
			t.Fatal("symlinked gc plan root accepted")
		}
	})

	t.Run("file", func(t *testing.T) {
		stateRoot := t.TempDir()
		plan := testStorageGCPlan(t)
		path, err := storageGCPlanPath(stateRoot, plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		content, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "plan.json")
		if err := os.WriteFile(outside, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, path); err != nil {
			t.Fatal(err)
		}
		if _, err := readStorageGCPlan(stateRoot, plan.ID); err == nil {
			t.Fatal("symlinked gc plan file accepted")
		}
	})
}

func TestStorageGCBrowserEligibility(t *testing.T) {
	now := time.Now().UTC()
	cutoff := now.Add(-time.Hour)
	old := now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	young := now.Add(-30 * time.Minute).Format(time.RFC3339Nano)

	if observed, ok := storageGCBrowserObservedAt(edgeclient.ProjectBrowserSnapshot{State: "closed", UpdatedAt: old}, cutoff); !ok || observed.After(cutoff) {
		t.Fatalf("old closed browser rejected: %v %v", observed, ok)
	}
	for _, snapshot := range []edgeclient.ProjectBrowserSnapshot{
		{State: "ready", UpdatedAt: old},
		{State: "closed", UpdatedAt: young},
		{State: "closed", UpdatedAt: "not-a-time"},
	} {
		if _, ok := storageGCBrowserObservedAt(snapshot, cutoff); ok {
			t.Fatalf("unsafe browser accepted: %+v", snapshot)
		}
	}
}

func TestStorageGCProcessEligibility(t *testing.T) {
	now := time.Now().UTC()
	cutoff := now.Add(-time.Hour)
	old := now.Add(-2 * time.Hour)
	young := now.Add(-30 * time.Minute)

	if observed, ok := storageGCProcessObservedAt(edgeclient.ProjectProcessSnapshot{State: edgeclient.ProjectProcessExited, FinishedAt: old}, cutoff); !ok || observed.After(cutoff) {
		t.Fatalf("old terminal process rejected: %v %v", observed, ok)
	}
	for _, snapshot := range []edgeclient.ProjectProcessSnapshot{
		{State: edgeclient.ProjectProcessRunning, FinishedAt: old},
		{State: edgeclient.ProjectProcessExited, FinishedAt: young},
		{State: edgeclient.ProjectProcessExited},
	} {
		if _, ok := storageGCProcessObservedAt(snapshot, cutoff); ok {
			t.Fatalf("unsafe process accepted: %+v", snapshot)
		}
	}
}

func TestStorageGCApplyRejectsExpiredPlanBeforeEffects(t *testing.T) {
	stateRoot := t.TempDir()
	plan := testStorageGCPlan(t)
	plan.CreatedAt = time.Now().UTC().Add(-20 * time.Minute)
	plan.ExpiresAt = plan.CreatedAt.Add(5 * time.Minute)
	plan.Candidates[0].ObservedAt = plan.CreatedAt.Add(-2 * time.Hour)
	if err := writeStorageGCPlan(stateRoot, plan); err != nil {
		t.Fatal(err)
	}
	if _, code := applyStorageGC(context.Background(), stateRoot, nil, nil, plan.ID); code != "storage_gc_plan_expired" {
		t.Fatalf("expired plan code=%q", code)
	}
}

func TestStorageGCApplyRejectsUsedPlanBeforeEffects(t *testing.T) {
	stateRoot := t.TempDir()
	plan := testStorageGCPlan(t)
	if err := writeStorageGCPlan(stateRoot, plan); err != nil {
		t.Fatal(err)
	}
	if err := consumeStorageGCPlan(stateRoot, plan.ID); err != nil {
		t.Fatal(err)
	}
	if _, code := applyStorageGC(context.Background(), stateRoot, nil, nil, plan.ID); code != "storage_gc_plan_used" {
		t.Fatalf("used plan code=%q", code)
	}
}

func TestStorageGCPlanConvergenceRemovesOnlyExpiredSafePlans(t *testing.T) {
	stateRoot := t.TempDir()
	expired := testStorageGCPlan(t)
	expired.CreatedAt = time.Now().UTC().Add(-20 * time.Minute)
	expired.ExpiresAt = expired.CreatedAt.Add(5 * time.Minute)
	expired.Candidates[0].ObservedAt = expired.CreatedAt.Add(-2 * time.Hour)
	if err := writeStorageGCPlan(stateRoot, expired); err != nil {
		t.Fatal(err)
	}
	if err := consumeStorageGCPlan(stateRoot, expired.ID); err != nil {
		t.Fatal(err)
	}

	current := testStorageGCPlan(t)
	current.ID = "sgp_" + strings.Repeat("d", 32)
	current.Candidates[0].ResourceID = "br_" + strings.Repeat("e", 32)
	if err := writeStorageGCPlan(stateRoot, current); err != nil {
		t.Fatal(err)
	}

	if err := convergeStorageGCPlans(stateRoot, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	expiredPath, err := storageGCPlanPath(stateRoot, expired.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(expiredPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired plan remains: %v", err)
	}
	if _, err := os.Lstat(expiredPath + ".consumed"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired consumed marker remains: %v", err)
	}
	currentPath, err := storageGCPlanPath(stateRoot, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(currentPath); err != nil {
		t.Fatalf("current plan removed: %v", err)
	}
}

func TestStorageGCPlanAcceptsDisposableToolboxCandidate(t *testing.T) {
	plan := testStorageGCPlan(t)
	plan.Candidates = append(plan.Candidates, storageGCCandidate{
		Kind: storageGCCandidateToolbox, Alias: "project", Target: "parrot",
		WorkspaceID: "ws_" + strings.Repeat("b", 32), Generation: 1,
		ResourceID: "tb_" + strings.Repeat("f", 32), ObservedAt: plan.CreatedAt.Add(-2 * time.Hour),
	})
	if !validStorageGCPlan(plan, plan.ID) {
		t.Fatal("valid disposable toolbox candidate rejected")
	}
	result := storageGCResultForPlan(plan, 0, 0)
	found := false
	for _, category := range result.StorageGCCategories {
		if category.Class == "disposable_toolboxes" {
			found = category.ObjectCount == 1 && !category.Complete
		}
	}
	if !found {
		t.Fatalf("disposable toolbox category missing: %+v", result.StorageGCCategories)
	}
}
