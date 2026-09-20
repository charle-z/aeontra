//go:build !windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

const (
	storageGCPlanTTL                = 10 * time.Minute
	storageGCMaxCandidates          = 256
	storageGCMaxPlanFiles           = 512
	storageGCMaxPlanBytes     int64 = 256 << 10
	storageGCCandidateBrowser       = "browser"
	storageGCCandidateProcess       = "process"
	storageGCCandidateToolbox       = "toolbox"
)

type storageGCCandidate struct {
	Kind        string
	Alias       string
	Target      string
	WorkspaceID string
	Generation  uint64
	ResourceID  string
	ObservedAt  time.Time
	Bytes       uint64
}

type storageGCPlan struct {
	Version       int
	ID            string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	MinAgeSeconds int
	Candidates    []storageGCCandidate
}

func newStorageGCPlanID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errors.New("storage gc plan generation failed")
	}
	return "sgp_" + hex.EncodeToString(raw[:]), nil
}

func validStorageGCPrivateID(value, prefix string) bool {
	if len(value) != len(prefix)+32 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, char := range value[len(prefix):] {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func storageGCPlanPath(stateRoot, id string) (string, error) {
	if !validStorageGCPrivateID(id, "sgp_") {
		return "", errors.New("storage gc plan id is invalid")
	}
	root := filepath.Join(stateRoot, "storage-gc-plans")
	if err := os.Mkdir(root, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !projectGitOwned(info) {
		return "", errors.New("storage gc plan root is unsafe")
	}
	return filepath.Join(root, id+".json"), nil
}

func convergeStorageGCPlans(stateRoot string, now time.Time) error {
	root := filepath.Join(stateRoot, "storage-gc-plans")
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !projectGitOwned(info) {
		return errors.New("storage gc plan root is unsafe")
	}
	directory, err := os.Open(root)
	if err != nil {
		return err
	}
	entries, readErr := directory.ReadDir(storageGCMaxPlanFiles + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !validStorageGCPrivateID(id, "sgp_") {
			continue
		}
		plan, planErr := readStorageGCPlan(stateRoot, id)
		if planErr != nil || now.Before(plan.ExpiresAt) {
			continue
		}
		path, pathErr := storageGCPlanPath(stateRoot, id)
		if pathErr != nil {
			continue
		}
		consumed, consumedErr := storageGCPlanConsumed(stateRoot, id)
		if consumedErr != nil {
			continue
		}
		if consumed {
			if removeErr := os.Remove(path + ".consumed"); removeErr != nil {
				continue
			}
		}
		_ = os.Remove(path)
	}
	directory, err = os.Open(root)
	if err != nil {
		return err
	}
	remaining, readErr := directory.ReadDir(storageGCMaxPlanFiles + 1)
	closeErr = directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(remaining) > storageGCMaxPlanFiles {
		return errors.New("storage gc plan store is full")
	}
	return nil
}

func writeStorageGCPlan(stateRoot string, plan storageGCPlan) error {
	path, err := storageGCPlanPath(stateRoot, plan.ID)
	if err != nil {
		return err
	}
	content, err := json.Marshal(plan)
	if err != nil || int64(len(content)) > storageGCMaxPlanBytes {
		return errors.New("storage gc plan is oversized")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("storage gc plan write failed")
	}
	return nil
}

func readStorageGCPlan(stateRoot, id string) (storageGCPlan, error) {
	path, err := storageGCPlanPath(stateRoot, id)
	if err != nil {
		return storageGCPlan{}, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != 0o600 || info.Size() < 1 || info.Size() > storageGCMaxPlanBytes ||
		!projectGitOwned(info) {
		return storageGCPlan{}, errors.New("storage gc plan is unsafe")
	}
	file, err := os.Open(path)
	if err != nil {
		return storageGCPlan{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, storageGCMaxPlanBytes))
	decoder.DisallowUnknownFields()
	var plan storageGCPlan
	if err := decoder.Decode(&plan); err != nil || decoder.Decode(&struct{}{}) != io.EOF || !validStorageGCPlan(plan, id) {
		return storageGCPlan{}, errors.New("storage gc plan is invalid")
	}
	return plan, nil
}

func consumeStorageGCPlan(stateRoot, id string) error {
	path, err := storageGCPlanPath(stateRoot, id)
	if err != nil {
		return err
	}
	marker := path + ".consumed"
	file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString("consumed"); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func validStorageGCPlan(plan storageGCPlan, id string) bool {
	if plan.Version != 1 || plan.ID != id || plan.CreatedAt.IsZero() || plan.ExpiresAt.IsZero() ||
		!plan.ExpiresAt.After(plan.CreatedAt) || plan.ExpiresAt.Sub(plan.CreatedAt) > storageGCPlanTTL ||
		plan.MinAgeSeconds < 60 || plan.MinAgeSeconds > 31_536_000 || len(plan.Candidates) > storageGCMaxCandidates {
		return false
	}
	for _, candidate := range plan.Candidates {
		if candidate.Alias == "" || len(candidate.Alias) > 128 ||
			candidate.Target == "" || len(candidate.Target) > 128 ||
			candidate.Generation == 0 ||
			!validStorageGCPrivateID(candidate.WorkspaceID, "ws_") ||
			candidate.ObservedAt.IsZero() || candidate.ObservedAt.After(plan.CreatedAt) {
			return false
		}
		switch candidate.Kind {
		case storageGCCandidateBrowser:
			if !validStorageGCPrivateID(candidate.ResourceID, "br_") {
				return false
			}
		case storageGCCandidateProcess:
			if !validStorageGCPrivateID(candidate.ResourceID, "pr_") {
				return false
			}
		case storageGCCandidateToolbox:
			if !validStorageGCPrivateID(candidate.ResourceID, "tb_") {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func storageGCBrowserObservedAt(snapshot edgeclient.ProjectBrowserSnapshot, cutoff time.Time) (time.Time, bool) {
	if snapshot.State != "closed" || snapshot.UpdatedAt == "" {
		return time.Time{}, false
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, snapshot.UpdatedAt)
	if err != nil {
		return time.Time{}, false
	}
	updatedAt = updatedAt.UTC()
	return updatedAt, !updatedAt.After(cutoff)
}

func storageGCProcessObservedAt(snapshot edgeclient.ProjectProcessSnapshot, cutoff time.Time) (time.Time, bool) {
	if snapshot.FinishedAt.IsZero() {
		return time.Time{}, false
	}
	switch snapshot.State {
	case edgeclient.ProjectProcessExited, edgeclient.ProjectProcessFailed, edgeclient.ProjectProcessStopped:
	default:
		return time.Time{}, false
	}
	finishedAt := snapshot.FinishedAt.UTC()
	return finishedAt, !finishedAt.After(cutoff)
}

func collectStorageGCCandidates(ctx context.Context, projects *edgeclient.ProjectRegistry, processes *edgeclient.ProjectProcessManager, browsers *edgeclient.ProjectBrowserManager, toolboxes []*edgeclient.ProjectToolboxManager, createdAt time.Time, minAgeSeconds int) ([]storageGCCandidate, string) {
	claims, err := projects.ListClaimsContext(ctx, "")
	if err != nil {
		return nil, "storage_gc_registry_unavailable"
	}
	cutoff := createdAt.Add(-time.Duration(minAgeSeconds) * time.Second)
	candidates := make([]storageGCCandidate, 0)
	for _, claim := range claims {
		if claim.State != edgeclient.ProjectClaimHealthy || claim.WorkspaceID == "" {
			continue
		}
		resolution, err := projects.ResolveRegistered(claim.Alias, claim.Target)
		if err != nil || resolution.Workspace.ID != claim.WorkspaceID {
			return nil, "storage_gc_registry_changed"
		}
		browserItems, err := browsers.List(edgeclient.ProjectBrowserListRequest{Resolution: resolution, Limit: edge.MaxBrowserSessions})
		if err != nil {
			return nil, "storage_gc_browser_unavailable"
		}
		for _, item := range browserItems {
			observedAt, ok := storageGCBrowserObservedAt(item, cutoff)
			if !ok {
				continue
			}
			candidates = append(candidates, storageGCCandidate{
				Kind: storageGCCandidateBrowser, Alias: claim.Alias, Target: claim.Target,
				WorkspaceID: claim.WorkspaceID, Generation: claim.Generation,
				ResourceID: item.SessionID, ObservedAt: observedAt,
			})
			if len(candidates) > storageGCMaxCandidates {
				return nil, "storage_gc_candidate_limit"
			}
		}
		processItems, err := processes.List(edgeclient.ProjectProcessListRequest{
			ProjectAlias: claim.Alias, TargetAlias: claim.Target, WorkspaceID: claim.WorkspaceID, Limit: 100,
		})
		if err != nil {
			return nil, "storage_gc_process_unavailable"
		}
		for _, item := range processItems {
			observedAt, ok := storageGCProcessObservedAt(item, cutoff)
			if !ok {
				continue
			}
			candidates = append(candidates, storageGCCandidate{
				Kind: storageGCCandidateProcess, Alias: claim.Alias, Target: claim.Target,
				WorkspaceID: claim.WorkspaceID, Generation: claim.Generation,
				ResourceID: item.ProcessID, ObservedAt: observedAt,
			})
			if len(candidates) > storageGCMaxCandidates {
				return nil, "storage_gc_candidate_limit"
			}
		}
		if snapshot, _, eligible := storageGCToolboxCandidate(ctx, toolboxes, resolution, cutoff); eligible {
			candidates = append(candidates, storageGCCandidate{
				Kind: storageGCCandidateToolbox, Alias: claim.Alias, Target: claim.Target,
				WorkspaceID: claim.WorkspaceID, Generation: claim.Generation,
				ResourceID: snapshot.ToolboxID, ObservedAt: snapshot.UpdatedAt,
			})
			if len(candidates) > storageGCMaxCandidates {
				return nil, "storage_gc_candidate_limit"
			}
		}
	}
	return candidates, ""
}

func openStorageGCToolboxManagers(stateRoot string) []*edgeclient.ProjectToolboxManager {
	endpoints, err := edgeclient.DiscoverRootlessContainerEndpoints(os.Geteuid(), "")
	if err != nil || len(endpoints) == 0 {
		return nil
	}
	managers := make([]*edgeclient.ProjectToolboxManager, 0, len(endpoints))
	for _, endpoint := range endpoints {
		manager, err := edgeclient.OpenProjectToolboxManager(edgeclient.ProjectToolboxManagerConfig{StateRoot: stateRoot, Endpoint: endpoint})
		if err != nil {
			return nil
		}
		managers = append(managers, manager)
	}
	return managers
}

func storageGCToolboxCandidate(ctx context.Context, managers []*edgeclient.ProjectToolboxManager, resolution edgeclient.ProjectResolution, cutoff time.Time) (edgeclient.ProjectToolboxSnapshot, *edgeclient.ProjectToolboxManager, bool) {
	var selected edgeclient.ProjectToolboxSnapshot
	var selectedManager *edgeclient.ProjectToolboxManager
	for _, manager := range managers {
		snapshot, eligible, err := manager.StorageGCCandidate(ctx, edgeclient.ProjectToolboxStorageGCStatusRequest{
			ProjectAlias: resolution.Project.Alias, TargetAlias: resolution.TargetAlias,
			Workspace: resolution.Workspace, Cutoff: cutoff,
		})
		if err != nil || !eligible {
			continue
		}
		if selectedManager != nil {
			return edgeclient.ProjectToolboxSnapshot{}, nil, false
		}
		selected, selectedManager = snapshot, manager
	}
	return selected, selectedManager, selectedManager != nil
}

func storageGCResultForPlan(plan storageGCPlan, removedCount, removedBytes uint64) edge.OperationResult {
	var candidateBytes uint64
	browserCount := uint64(0)
	processCount := uint64(0)
	toolboxCount := uint64(0)
	for _, candidate := range plan.Candidates {
		candidateBytes += candidate.Bytes
		switch candidate.Kind {
		case storageGCCandidateBrowser:
			browserCount++
		case storageGCCandidateProcess:
			processCount++
		case storageGCCandidateToolbox:
			toolboxCount++
		}
	}
	categories := make([]edge.StorageCategorySummary, 0, 3)
	if browserCount > 0 {
		categories = append(categories, edge.StorageCategorySummary{Class: "browser_profiles_artifacts", ObjectCount: browserCount, Complete: false})
	}
	if processCount > 0 {
		categories = append(categories, edge.StorageCategorySummary{Class: "process_logs", ObjectCount: processCount, Complete: false})
	}
	if toolboxCount > 0 {
		categories = append(categories, edge.StorageCategorySummary{Class: "disposable_toolboxes", ObjectCount: toolboxCount, Complete: false})
	}
	return edge.OperationResult{
		StorageGCPlanID: plan.ID, StorageGCPlanExpiresAt: plan.ExpiresAt.UTC().Format(time.RFC3339),
		StorageGCMinAgeSeconds: plan.MinAgeSeconds, StorageGCCandidateCount: uint64(len(plan.Candidates)),
		StorageGCCandidateBytes: candidateBytes, StorageGCRemovedCount: removedCount,
		StorageGCRemovedBytes: removedBytes, StorageGCCategories: categories,
	}
}

func previewStorageGC(ctx context.Context, stateRoot string, processes *edgeclient.ProjectProcessManager, browsers *edgeclient.ProjectBrowserManager, minAgeSeconds int) (edge.OperationResult, string) {
	if err := convergeStorageGCPlans(stateRoot, time.Now().UTC()); err != nil {
		return edge.OperationResult{}, "storage_gc_plan_store_unavailable"
	}
	_, workspaces, projects, _, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	createdAt := time.Now().UTC()
	toolboxes := openStorageGCToolboxManagers(stateRoot)
	candidates, code := collectStorageGCCandidates(ctx, projects, processes, browsers, toolboxes, createdAt, minAgeSeconds)
	if code != "" {
		return edge.OperationResult{}, code
	}
	id, err := newStorageGCPlanID()
	if err != nil {
		return edge.OperationResult{}, "storage_gc_plan_unavailable"
	}
	plan := storageGCPlan{
		Version: 1, ID: id, CreatedAt: createdAt, ExpiresAt: createdAt.Add(storageGCPlanTTL),
		MinAgeSeconds: minAgeSeconds, Candidates: candidates,
	}
	if err := writeStorageGCPlan(stateRoot, plan); err != nil {
		return edge.OperationResult{}, "storage_gc_plan_unavailable"
	}
	return storageGCResultForPlan(plan, 0, 0), ""
}

func storageGCPlanConsumed(stateRoot, id string) (bool, error) {
	path, err := storageGCPlanPath(stateRoot, id)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(path + ".consumed")
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != 0o600 || info.Size() < 1 || info.Size() > 64 || !projectGitOwned(info) {
		return false, errors.New("storage gc consumed marker is unsafe")
	}
	return true, nil
}

type storageGCPreflight struct {
	resolutions     map[string]edgeclient.ProjectResolution
	toolboxManagers map[string]*edgeclient.ProjectToolboxManager
}

func storageGCToolboxManagerKey(alias, target, toolboxID string) string {
	return alias + "|" + target + "|" + toolboxID
}

func preflightStorageGC(ctx context.Context, projects *edgeclient.ProjectRegistry, processes *edgeclient.ProjectProcessManager, browsers *edgeclient.ProjectBrowserManager, toolboxes []*edgeclient.ProjectToolboxManager, plan storageGCPlan) (storageGCPreflight, string) {
	claims, err := projects.ListClaimsContext(ctx, "")
	if err != nil {
		return storageGCPreflight{}, "storage_gc_registry_unavailable"
	}
	healthy := make(map[string]edgeclient.ProjectClaim, len(claims))
	for _, claim := range claims {
		if claim.State == edgeclient.ProjectClaimHealthy {
			healthy[claim.Alias+"\x00"+claim.Target] = claim
		}
	}
	cutoff := plan.CreatedAt.Add(-time.Duration(plan.MinAgeSeconds) * time.Second)
	resolutions := make(map[string]edgeclient.ProjectResolution)
	toolboxManagers := make(map[string]*edgeclient.ProjectToolboxManager)
	for _, candidate := range plan.Candidates {
		key := candidate.Alias + "\x00" + candidate.Target
		claim, ok := healthy[key]
		if !ok || claim.WorkspaceID != candidate.WorkspaceID || claim.Generation != candidate.Generation {
			return storageGCPreflight{}, "storage_gc_plan_changed"
		}
		resolution, ok := resolutions[key]
		if !ok {
			resolution, err = projects.ResolveRegistered(candidate.Alias, candidate.Target)
			if err != nil || resolution.Workspace.ID != candidate.WorkspaceID {
				return storageGCPreflight{}, "storage_gc_plan_changed"
			}
			resolutions[key] = resolution
		}
		switch candidate.Kind {
		case storageGCCandidateBrowser:
			items, listErr := browsers.List(edgeclient.ProjectBrowserListRequest{Resolution: resolution, Limit: edge.MaxBrowserSessions})
			if listErr != nil {
				return storageGCPreflight{}, "storage_gc_browser_unavailable"
			}
			matched := false
			for _, item := range items {
				if item.SessionID != candidate.ResourceID {
					continue
				}
				observedAt, eligible := storageGCBrowserObservedAt(item, cutoff)
				if !eligible || !observedAt.Equal(candidate.ObservedAt) {
					return storageGCPreflight{}, "storage_gc_plan_changed"
				}
				matched = true
				break
			}
			if !matched {
				return storageGCPreflight{}, "storage_gc_plan_changed"
			}
		case storageGCCandidateProcess:
			items, listErr := processes.List(edgeclient.ProjectProcessListRequest{
				ProjectAlias: candidate.Alias, TargetAlias: candidate.Target,
				WorkspaceID: candidate.WorkspaceID, Limit: 100,
			})
			if listErr != nil {
				return storageGCPreflight{}, "storage_gc_process_unavailable"
			}
			matched := false
			for _, item := range items {
				if item.ProcessID != candidate.ResourceID {
					continue
				}
				observedAt, eligible := storageGCProcessObservedAt(item, cutoff)
				if !eligible || !observedAt.Equal(candidate.ObservedAt) {
					return storageGCPreflight{}, "storage_gc_plan_changed"
				}
				matched = true
				break
			}
			if !matched {
				return storageGCPreflight{}, "storage_gc_plan_changed"
			}
		case storageGCCandidateToolbox:
			if len(toolboxes) == 0 {
				return storageGCPreflight{}, "storage_gc_toolbox_unavailable"
			}
			snapshot, manager, eligible := storageGCToolboxCandidate(ctx, toolboxes, resolution, cutoff)
			if !eligible || snapshot.ToolboxID != candidate.ResourceID || !snapshot.UpdatedAt.Equal(candidate.ObservedAt) {
				return storageGCPreflight{}, "storage_gc_plan_changed"
			}
			toolboxManagers[storageGCToolboxManagerKey(candidate.Alias, candidate.Target, candidate.ResourceID)] = manager
		default:
			return storageGCPreflight{}, "storage_gc_plan_invalid"
		}
	}
	return storageGCPreflight{resolutions: resolutions, toolboxManagers: toolboxManagers}, ""
}

func applyStorageGC(ctx context.Context, stateRoot string, processes *edgeclient.ProjectProcessManager, browsers *edgeclient.ProjectBrowserManager, planID string) (edge.OperationResult, string) {
	plan, err := readStorageGCPlan(stateRoot, planID)
	if err != nil {
		return edge.OperationResult{}, "storage_gc_plan_invalid"
	}
	if !time.Now().UTC().Before(plan.ExpiresAt) {
		return edge.OperationResult{}, "storage_gc_plan_expired"
	}
	consumed, err := storageGCPlanConsumed(stateRoot, plan.ID)
	if err != nil {
		return edge.OperationResult{}, "storage_gc_plan_invalid"
	}
	if consumed {
		return edge.OperationResult{}, "storage_gc_plan_used"
	}

	_, workspaces, projects, _, code := openProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	toolboxes := openStorageGCToolboxManagers(stateRoot)
	preflight, code := preflightStorageGC(ctx, projects, processes, browsers, toolboxes, plan)
	if code != "" {
		return edge.OperationResult{}, code
	}
	if err := consumeStorageGCPlan(stateRoot, plan.ID); err != nil {
		return edge.OperationResult{}, "storage_gc_plan_used"
	}

	var removed uint64
	for _, candidate := range plan.Candidates {
		key := candidate.Alias + "\x00" + candidate.Target
		switch candidate.Kind {
		case storageGCCandidateBrowser:
			result, cleanupErr := browsers.Cleanup(edgeclient.ProjectBrowserCleanupRequest{
				Resolution: preflight.resolutions[key],
				SessionID:  candidate.ResourceID,
			})
			if cleanupErr != nil || result.Removed != 1 {
				return storageGCResultForPlan(plan, removed, 0), "storage_gc_apply_partial"
			}
			removed++
		case storageGCCandidateProcess:
			result, cleanupErr := processes.Cleanup(edgeclient.ProjectProcessCleanupRequest{
				ProcessID:    candidate.ResourceID,
				ProjectAlias: candidate.Alias,
				TargetAlias:  candidate.Target,
				WorkspaceID:  candidate.WorkspaceID,
			})
			if cleanupErr != nil || result.Active != 0 || result.Removed != 1 {
				return storageGCResultForPlan(plan, removed, 0), "storage_gc_apply_partial"
			}
			removed++
		case storageGCCandidateToolbox:
			manager := preflight.toolboxManagers[storageGCToolboxManagerKey(candidate.Alias, candidate.Target, candidate.ResourceID)]
			if manager == nil {
				return storageGCResultForPlan(plan, removed, 0), "storage_gc_apply_partial"
			}
			removedToolbox, cleanupErr := manager.StorageGCCleanup(ctx, edgeclient.ProjectToolboxStorageGCCleanupRequest{
				ProjectAlias: candidate.Alias, TargetAlias: candidate.Target,
				Workspace: preflight.resolutions[key].Workspace,
				ToolboxID: candidate.ResourceID, ObservedAt: candidate.ObservedAt,
				Cutoff: plan.CreatedAt.Add(-time.Duration(plan.MinAgeSeconds) * time.Second),
			})
			if cleanupErr != nil || !removedToolbox {
				return storageGCResultForPlan(plan, removed, 0), "storage_gc_apply_partial"
			}
			removed++
		default:
			return storageGCResultForPlan(plan, removed, 0), "storage_gc_apply_partial"
		}
	}
	return storageGCResultForPlan(plan, removed, 0), ""
}

func executeStorageGC(ctx context.Context, stateRoot string, processes *edgeclient.ProjectProcessManager, browsers *edgeclient.ProjectBrowserManager, operation edge.Operation) (edge.OperationResult, string) {
	switch operation.Kind {
	case edge.OperationStorageGCPreview:
		return previewStorageGC(ctx, stateRoot, processes, browsers, operation.Request.StorageGCMinAgeSeconds)
	case edge.OperationStorageGCApply:
		return applyStorageGC(ctx, stateRoot, processes, browsers, operation.Request.StorageGCPlanID)
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
}
