//go:build !windows

package edgeclient

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ProjectBrowserHarnessArtifactListRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
	RunID                     string
	Limit                     int
}

type ProjectBrowserHarnessArtifactReadRequest struct {
	ProjectAlias, TargetAlias string
	Workspace                 Workspace
	RunID                     string
	Path                      string
	Offset                    int64
	Limit                     int
}

type ProjectBrowserHarnessArtifactSummary struct {
	Path, MediaType, SHA256 string
	Bytes                   int64
	UpdatedAt               time.Time
}

type ProjectBrowserHarnessArtifactChunk struct {
	RunID, Path, MediaType, SHA256, DataBase64 string
	Bytes, Offset, Next                        int64
	EOF                                        bool
}

type projectBrowserHarnessPaths struct {
	HostRoot, HostRunRoot, HostProfileRoot                string
	ContainerRoot, ContainerRunRoot, ContainerProfileRoot string
}

type projectBrowserArtifactDigest struct {
	Info   os.FileInfo
	SHA256 string
}

func projectBrowserHarnessPathsFor(workspace Workspace, runID, profile string) projectBrowserHarnessPaths {
	hostRoot := filepath.Join(workspace.Path, filepath.FromSlash(projectBrowserHarnessRootRelative))
	containerRoot := "/workspace/" + projectBrowserHarnessRootRelative
	return projectBrowserHarnessPaths{
		HostRoot:             hostRoot,
		HostRunRoot:          filepath.Join(hostRoot, "runs", runID),
		HostProfileRoot:      filepath.Join(hostRoot, "profiles", profile),
		ContainerRoot:        containerRoot,
		ContainerRunRoot:     containerRoot + "/runs/" + runID,
		ContainerProfileRoot: containerRoot + "/profiles/" + profile,
	}
}

func prepareProjectBrowserHarnessPaths(workspace Workspace, runID, profile string) (projectBrowserHarnessPaths, error) {
	if !projectBrowserHarnessIDPattern.MatchString(runID) || !projectBrowserProfilePattern.MatchString(profile) {
		return projectBrowserHarnessPaths{}, ErrProjectToolboxUnsafeState
	}
	paths := projectBrowserHarnessPathsFor(workspace, runID, profile)
	for _, path := range []string{paths.HostRoot, filepath.Join(paths.HostRoot, "runs"), filepath.Join(paths.HostRoot, "profiles"), paths.HostRunRoot, filepath.Join(paths.HostRunRoot, "artifacts"), filepath.Join(paths.HostRunRoot, "downloads"), paths.HostProfileRoot} {
		if err := ensureProjectBrowserHarnessDirectory(path); err != nil {
			return projectBrowserHarnessPaths{}, err
		}
	}
	for _, name := range []string{"stdout.log", "stderr.log"} {
		file, err := os.OpenFile(filepath.Join(paths.HostRunRoot, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return projectBrowserHarnessPaths{}, ErrProjectToolboxUnsafeState
		}
		if err := file.Close(); err != nil {
			return projectBrowserHarnessPaths{}, ErrProjectToolboxUnsafeState
		}
	}
	return paths, nil
}

func ensureProjectBrowserHarnessDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return ErrProjectToolboxUnsafeState
	}
	if err := os.MkdirAll(path, 0o700); err != nil || rejectSymlinkPath(path) != nil {
		return ErrProjectToolboxUnsafeState
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return ErrProjectToolboxUnsafeState
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !ownedByCurrentUIDPortable(info) {
		return ErrProjectToolboxUnsafeState
	}
	return nil
}

func (manager *ProjectToolboxManager) BrowserHarnessArtifactList(request ProjectBrowserHarnessArtifactListRequest) ([]ProjectBrowserHarnessArtifactSummary, error) {
	manager.mu.Lock()
	if request.Limit < 1 || request.Limit > projectBrowserHarnessMaxArtifacts {
		manager.mu.Unlock()
		return nil, ErrProjectToolboxUnsafeState
	}
	if _, _, err := manager.browserHarnessRecord(request.ProjectAlias, request.TargetAlias, request.Workspace, request.RunID); err != nil {
		manager.mu.Unlock()
		return nil, err
	}
	manager.mu.Unlock()
	_, _, artifacts, err := collectProjectBrowserHarnessArtifacts(request.Workspace, request.RunID, request.Limit)
	return artifacts, err
}

func (manager *ProjectToolboxManager) BrowserHarnessArtifactRead(request ProjectBrowserHarnessArtifactReadRequest) (ProjectBrowserHarnessArtifactChunk, error) {
	manager.mu.Lock()
	if request.Offset < 0 || request.Limit < 1 || request.Limit > projectBrowserHarnessOutputLimit {
		manager.mu.Unlock()
		return ProjectBrowserHarnessArtifactChunk{}, ErrProjectToolboxUnsafeState
	}
	if _, _, err := manager.browserHarnessRecord(request.ProjectAlias, request.TargetAlias, request.Workspace, request.RunID); err != nil {
		manager.mu.Unlock()
		return ProjectBrowserHarnessArtifactChunk{}, err
	}
	path, relative, err := resolveProjectBrowserHarnessArtifact(request.Workspace, request.RunID, request.Path)
	if err != nil {
		manager.mu.Unlock()
		return ProjectBrowserHarnessArtifactChunk{}, err
	}
	manager.mu.Unlock()
	paths := projectBrowserHarnessPathsFor(request.Workspace, request.RunID, "default")
	file, info, err := openStableOwnedRegularUnder(paths.HostRunRoot, path)
	if err != nil {
		return ProjectBrowserHarnessArtifactChunk{}, ErrProjectToolboxUnsafeState
	}
	defer file.Close()
	if info.Size() < 0 || info.Size() > projectBrowserHarnessMaxFileBytes || request.Offset > info.Size() {
		return ProjectBrowserHarnessArtifactChunk{}, ErrProjectToolboxUnsafeState
	}
	cacheKey := request.Workspace.ID + "\x00" + request.RunID + "\x00" + relative
	manager.mu.Lock()
	cached, cacheHit := manager.artifactHash[cacheKey]
	manager.mu.Unlock()
	digest := cached.SHA256
	if !cacheHit || cached.Info == nil || !os.SameFile(cached.Info, info) || cached.Info.Size() != info.Size() || !cached.Info.ModTime().Equal(info.ModTime()) {
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			return ProjectBrowserHarnessArtifactChunk{}, ErrProjectToolboxUnsafeState
		}
		digest = hex.EncodeToString(hash.Sum(nil))
		manager.mu.Lock()
		manager.artifactHash[cacheKey] = projectBrowserArtifactDigest{Info: info, SHA256: digest}
		manager.mu.Unlock()
	}
	if _, err := file.Seek(request.Offset, io.SeekStart); err != nil {
		return ProjectBrowserHarnessArtifactChunk{}, ErrProjectToolboxUnsafeState
	}
	remaining := info.Size() - request.Offset
	size := int64(request.Limit)
	if remaining < size {
		size = remaining
	}
	buffer := make([]byte, size)
	n, err := io.ReadFull(file, buffer)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return ProjectBrowserHarnessArtifactChunk{}, ErrProjectToolboxUnsafeState
	}
	buffer = buffer[:n]
	next := request.Offset + int64(n)
	return ProjectBrowserHarnessArtifactChunk{RunID: request.RunID, Path: relative, MediaType: browserHarnessMediaType(relative), SHA256: digest, Bytes: info.Size(), Offset: request.Offset, Next: next, EOF: next == info.Size(), DataBase64: base64.StdEncoding.EncodeToString(buffer)}, nil
}

func scanProjectBrowserHarnessArtifacts(workspace Workspace, runID string, limit int) (int, int64, error) {
	count, bytes, _, err := collectProjectBrowserHarnessArtifacts(workspace, runID, limit)
	return count, bytes, err
}

func collectProjectBrowserHarnessArtifacts(workspace Workspace, runID string, limit int) (int, int64, []ProjectBrowserHarnessArtifactSummary, error) {
	if !projectBrowserHarnessIDPattern.MatchString(runID) || limit < 1 || limit > projectBrowserHarnessMaxArtifacts {
		return 0, 0, nil, ErrProjectToolboxUnsafeState
	}
	paths := projectBrowserHarnessPathsFor(workspace, runID, "default")
	state := projectBrowserArtifactScan{limit: limit}
	for _, rootName := range []string{"artifacts", "downloads"} {
		if err := scanProjectBrowserHarnessDirectory(paths.HostRunRoot, rootName, 1, &state); err != nil {
			return 0, 0, nil, err
		}
	}
	sort.Slice(state.entries, func(i, j int) bool { return state.entries[i].Path < state.entries[j].Path })
	return state.count, state.total, state.entries, nil
}

type projectBrowserArtifactScan struct {
	entries []ProjectBrowserHarnessArtifactSummary
	count   int
	scanned int
	total   int64
	limit   int
}

func scanProjectBrowserHarnessDirectory(runRoot, relative string, depth int, state *projectBrowserArtifactScan) error {
	if state == nil || depth > projectBrowserHarnessMaxDepth || len(relative) > 1024 {
		return ErrProjectToolboxUnsafeState
	}
	directory, _, err := openStableOwnedDirectoryUnder(runRoot, filepath.Join(runRoot, filepath.FromSlash(relative)))
	if err != nil {
		return ErrProjectToolboxUnsafeState
	}
	defer directory.Close()
	for {
		infos, readErr := directory.Readdir(32)
		for _, info := range infos {
			state.scanned++
			if state.scanned > projectBrowserHarnessMaxScanItems || info.Name() == "" || strings.ContainsAny(info.Name(), "/\\\x00") || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUIDPortable(info) {
				return ErrProjectToolboxUnsafeState
			}
			child := filepath.ToSlash(filepath.Join(relative, info.Name()))
			if len(child) > 1024 {
				return ErrProjectToolboxUnsafeState
			}
			if info.IsDir() {
				if err := scanProjectBrowserHarnessDirectory(runRoot, child, depth+1, state); err != nil {
					return err
				}
				continue
			}
			if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > projectBrowserHarnessMaxFileBytes || state.total > projectBrowserHarnessMaxReadBytes-info.Size() {
				return ErrProjectToolboxUnsafeState
			}
			state.count++
			state.total += info.Size()
			if state.count > projectBrowserHarnessMaxArtifacts {
				return ErrProjectToolboxUnsafeState
			}
			if len(state.entries) < state.limit {
				path := filepath.Join(runRoot, filepath.FromSlash(child))
				digest, err := sha256ProjectBrowserArtifact(runRoot, path)
				if err != nil {
					return err
				}
				state.entries = append(state.entries, ProjectBrowserHarnessArtifactSummary{Path: child, MediaType: browserHarnessMediaType(child), SHA256: digest, Bytes: info.Size(), UpdatedAt: info.ModTime().UTC()})
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return ErrProjectToolboxUnsafeState
		}
	}
}

func resolveProjectBrowserHarnessArtifact(workspace Workspace, runID, value string) (string, string, error) {
	if !projectBrowserHarnessIDPattern.MatchString(runID) || value == "" || filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\\") {
		return "", "", ErrProjectToolboxUnsafeState
	}
	clean := filepath.ToSlash(filepath.Clean(value))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || !(strings.HasPrefix(clean, "artifacts/") || strings.HasPrefix(clean, "downloads/")) {
		return "", "", ErrProjectToolboxUnsafeState
	}
	paths := projectBrowserHarnessPathsFor(workspace, runID, "default")
	path := filepath.Join(paths.HostRunRoot, filepath.FromSlash(clean))
	rel, err := filepath.Rel(paths.HostRunRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rejectSymlinkPath(path) != nil {
		return "", "", ErrProjectToolboxUnsafeState
	}
	return path, clean, nil
}

func sha256ProjectBrowserArtifact(root, path string) (string, error) {
	file, _, err := openStableOwnedRegularUnder(root, path)
	if err != nil {
		return "", ErrProjectToolboxUnsafeState
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", ErrProjectToolboxUnsafeState
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func browserHarnessMediaType(path string) string {
	media := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if media == "" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".zip":
			media = "application/zip"
		case ".har":
			media = "application/json"
		case ".webm":
			media = "video/webm"
		case ".mp4":
			media = "video/mp4"
		case ".pdf":
			media = "application/pdf"
		default:
			media = "application/octet-stream"
		}
	}
	return media
}

func removeProjectBrowserHarnessRunRoot(workspace Workspace, runID string) error {
	if !projectBrowserHarnessIDPattern.MatchString(runID) {
		return ErrProjectToolboxUnsafeState
	}
	paths := projectBrowserHarnessPathsFor(workspace, runID, "default")
	root := filepath.Join(workspace.Path, filepath.FromSlash(projectBrowserHarnessRootRelative), "runs")
	if filepath.Clean(paths.HostRunRoot) != filepath.Join(filepath.Clean(root), runID) {
		return ErrProjectToolboxUnsafeState
	}
	info, err := os.Lstat(paths.HostRunRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUIDPortable(info) {
		return ErrProjectToolboxUnsafeState
	}
	if err := os.RemoveAll(paths.HostRunRoot); err != nil {
		return ErrProjectToolboxUnsafeState
	}
	return nil
}

func removeProjectBrowserHarnessProfileRoot(workspace Workspace, profile string) error {
	if !projectBrowserProfilePattern.MatchString(profile) {
		return ErrProjectToolboxUnsafeState
	}
	paths := projectBrowserHarnessPathsFor(workspace, "bh_00000000000000000000000000000000", profile)
	root := filepath.Join(workspace.Path, filepath.FromSlash(projectBrowserHarnessRootRelative), "profiles")
	if filepath.Clean(paths.HostProfileRoot) != filepath.Join(filepath.Clean(root), profile) {
		return ErrProjectToolboxUnsafeState
	}
	info, err := os.Lstat(paths.HostProfileRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUIDPortable(info) {
		return ErrProjectToolboxUnsafeState
	}
	if err := os.RemoveAll(paths.HostProfileRoot); err != nil {
		return ErrProjectToolboxUnsafeState
	}
	return nil
}
