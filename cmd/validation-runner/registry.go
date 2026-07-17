package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const repositoryModeReadWrite = "read-write"

type repositoryEntry struct {
	repoID           string
	canonicalPath    string
	hostPath         string
	identity         fileIdentity
	manifestIdentity fileIdentity
	mode             string
	discoveredAt     time.Time
}

type repositoryRegistry struct {
	root         string
	hostRoot     string
	rootIdentity fileIdentity
	entries      map[string]repositoryEntry
}

func discoverRepositoryRegistry(root, hostRoot string, now time.Time) (*repositoryRegistry, error) {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, errors.New("repository registry root is unavailable")
	}
	canonicalRoot = filepath.Clean(canonicalRoot)
	if !filepath.IsAbs(canonicalRoot) || canonicalRoot == string(filepath.Separator) {
		return nil, errors.New("repository registry root is invalid")
	}
	hostRoot = filepath.Clean(hostRoot)
	if !filepath.IsAbs(hostRoot) || hostRoot == string(filepath.Separator) {
		return nil, errors.New("repository host root is invalid")
	}
	rootInfo, err := os.Lstat(canonicalRoot)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 || writableByOthers(rootInfo.Mode()) {
		return nil, errors.New("repository registry root has unsafe ownership or permissions")
	}
	rootIdentity, err := identityFromFileInfo(rootInfo)
	if err != nil {
		return nil, errors.New("repository registry root identity is unavailable")
	}
	registry := &repositoryRegistry{
		root:         canonicalRoot,
		hostRoot:     hostRoot,
		rootIdentity: rootIdentity,
		entries:      make(map[string]repositoryEntry),
	}
	children, err := os.ReadDir(canonicalRoot)
	if err != nil {
		return nil, errors.New("repository registry root cannot be enumerated")
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
	for _, child := range children {
		name := child.Name()
		if !validRepositoryID(name) {
			continue
		}
		if err := registry.register(name, now); err != nil {
			// Non-repositories and unsafe entries are intentionally absent from the
			// immutable snapshot. A request can only select a successfully registered ID.
			continue
		}
	}
	return registry, nil
}

func (r *repositoryRegistry) register(repoID string, discoveredAt time.Time) error {
	if r == nil || !validRepositoryID(repoID) {
		return errors.New("invalid repository identifier")
	}
	if _, exists := r.entries[repoID]; exists {
		return errors.New("duplicate repository identifier")
	}
	candidate := filepath.Join(r.root, repoID)
	canonical, err := filepath.EvalSymlinks(candidate)
	if err != nil || filepath.Clean(canonical) != candidate || filepath.Dir(canonical) != r.root {
		return errors.New("repository is not one canonical direct child")
	}
	info, err := os.Lstat(candidate)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || writableByOthers(info.Mode()) {
		return errors.New("repository has unsafe type or permissions")
	}
	identity, err := identityFromFileInfo(info)
	if err != nil {
		return errors.New("repository identity is unavailable")
	}
	packagePath := filepath.Join(candidate, "package.json")
	packageInfo, err := os.Lstat(packagePath)
	if err != nil || !packageInfo.Mode().IsRegular() || packageInfo.Mode()&os.ModeSymlink != 0 || writableByOthers(packageInfo.Mode()) {
		return errors.New("repository package manifest is unavailable or unsafe")
	}
	manifestIdentity, err := identityFromFileInfo(packageInfo)
	if err != nil {
		return errors.New("repository package manifest identity is unavailable")
	}
	if err := verifyRepositoryDescriptors(r.root, repoID, r.rootIdentity, identity, manifestIdentity); err != nil {
		return err
	}
	r.entries[repoID] = repositoryEntry{
		repoID:           repoID,
		canonicalPath:    candidate,
		hostPath:         filepath.Join(r.hostRoot, repoID),
		identity:         identity,
		manifestIdentity: manifestIdentity,
		mode:             repositoryModeReadWrite,
		discoveredAt:     discoveredAt.UTC(),
	}
	return nil
}

func (r *repositoryRegistry) lookup(repoID string) (repositoryEntry, error) {
	if r == nil || !validRepositoryID(repoID) {
		return repositoryEntry{}, errors.New("unknown repository identifier")
	}
	entry, ok := r.entries[repoID]
	if !ok {
		return repositoryEntry{}, errors.New("unknown repository identifier")
	}
	if err := r.revalidate(entry); err != nil {
		return repositoryEntry{}, err
	}
	return entry, nil
}

func (r *repositoryRegistry) revalidate(entry repositoryEntry) error {
	if r == nil || entry.repoID == "" || entry.mode != repositoryModeReadWrite {
		return errors.New("repository registry entry is invalid")
	}
	rootInfo, err := os.Lstat(r.root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 || writableByOthers(rootInfo.Mode()) {
		return errors.New("repository registry root changed")
	}
	rootIdentity, err := identityFromFileInfo(rootInfo)
	if err != nil || rootIdentity != r.rootIdentity {
		return errors.New("repository registry root changed")
	}
	canonical, err := filepath.EvalSymlinks(entry.canonicalPath)
	if err != nil || filepath.Clean(canonical) != entry.canonicalPath || filepath.Dir(canonical) != r.root {
		return errors.New("registered repository changed")
	}
	info, err := os.Lstat(entry.canonicalPath)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || writableByOthers(info.Mode()) {
		return errors.New("registered repository changed")
	}
	identity, err := identityFromFileInfo(info)
	if err != nil || identity != entry.identity {
		return errors.New("registered repository changed")
	}
	packageInfo, err := os.Lstat(filepath.Join(entry.canonicalPath, "package.json"))
	if err != nil || !packageInfo.Mode().IsRegular() || packageInfo.Mode()&os.ModeSymlink != 0 || writableByOthers(packageInfo.Mode()) {
		return errors.New("registered repository manifest changed")
	}
	manifestIdentity, err := identityFromFileInfo(packageInfo)
	if err != nil || manifestIdentity != entry.manifestIdentity {
		return errors.New("registered repository manifest changed")
	}
	if err := verifyRepositoryDescriptors(r.root, entry.repoID, r.rootIdentity, entry.identity, entry.manifestIdentity); err != nil {
		return err
	}
	return nil
}

func (r *repositoryRegistry) sanitizeOutput(output string) string {
	if r == nil || output == "" {
		return output
	}
	redacted := output
	paths := []string{r.hostRoot, r.root}
	for _, entry := range r.entries {
		paths = append(paths, entry.hostPath, entry.canonicalPath)
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	for _, path := range paths {
		if path != "" && path != string(filepath.Separator) {
			redacted = strings.ReplaceAll(redacted, path, "[repository]")
		}
	}
	return redacted
}

func validRepositoryID(repoID string) bool {
	if repoID == "" || repoID == "." || repoID == ".." || strings.TrimSpace(repoID) != repoID || strings.ContainsRune(repoID, '\x00') {
		return false
	}
	if filepath.IsAbs(repoID) || strings.ContainsAny(repoID, `/\\`) || strings.Contains(repoID, "..") {
		return false
	}
	for _, char := range repoID {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.') {
			return false
		}
	}
	return true
}

func writableByOthers(mode os.FileMode) bool {
	return mode.Perm()&0o022 != 0
}

func (entry repositoryEntry) mountSource() (string, error) {
	if entry.repoID == "" || entry.hostPath == "" || !filepath.IsAbs(entry.hostPath) || entry.mode != repositoryModeReadWrite {
		return "", fmt.Errorf("repository registry entry cannot be mounted")
	}
	return entry.hostPath, nil
}
