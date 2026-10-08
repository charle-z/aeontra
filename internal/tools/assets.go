package tools

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/assets"
	"github.com/charle-z/mcp-devbox/internal/audit"
)

var ErrAssetsNotConfigured = errors.New("asset library is not configured")

// AssetCapability never receives caller URLs or credentials. It shares central
// policy, audit and single-use plans, and writes only new files in selected roots.
type AssetCapability struct {
	*serviceCore
	mu      sync.RWMutex
	library *assets.Library
	fetch   func(context.Context, string) (assets.Download, error)
}

type AssetMaterializePreview struct {
	PlanID        string       `json:"plan_id"`
	ExpiresAt     time.Time    `json:"expires_at"`
	Repo          string       `json:"repo"`
	Path          string       `json:"path"`
	LibrarySHA256 string       `json:"library_sha256"`
	Asset         assets.Entry `json:"asset"`
}

type AssetMaterializeReceipt struct {
	Status        string `json:"status"`
	Repo          string `json:"repo"`
	Path          string `json:"path"`
	LibrarySHA256 string `json:"library_sha256"`
	assets.Download
}

func (c *AssetCapability) configureLibrary(library *assets.Library) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.library = library
	if library == nil {
		c.fetch = nil
	} else {
		c.fetch = library.Fetch
	}
}

func (c *AssetCapability) snapshot() (*assets.Library, func(context.Context, string) (assets.Download, error), error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.library == nil {
		return nil, nil, ErrAssetsNotConfigured
	}
	return c.library, c.fetch, nil
}

func finishAssetSpan(span *audit.Span, args string, err error) {
	decision := audit.Allow
	if err != nil {
		decision = audit.Error
		if errors.Is(err, ErrAssetsNotConfigured) {
			decision = audit.Deny
		}
	}
	span.Finish(decision, args, nil, err)
}

func (c *AssetCapability) AssetSearch(query string, limit int) (output string, err error) {
	span := c.log.Start("asset_search")
	defer func() { finishAssetSpan(span, fmt.Sprintf("limit=%d", limit), err) }()
	library, _, err := c.snapshot()
	if err != nil {
		return "", err
	}
	entries, err := library.Search(query, limit)
	if err != nil {
		return "", err
	}
	return encodeResultValue(entries)
}

func (c *AssetCapability) AssetMaterializePreview(id, repo, path string) (output string, err error) {
	span := c.log.Start("asset_materialize_preview")
	defer func() { finishAssetSpan(span, "reviewed raster destination", err) }()
	library, _, err := c.snapshot()
	if err != nil {
		return "", err
	}
	entry, err := library.Entry(id)
	if err != nil {
		return "", err
	}
	destination, err := c.assetDestination(repo, path, entry.MIME)
	if err != nil {
		return "", err
	}
	plan, err := c.plans.Create("asset-materialize", map[string]string{
		"id": id, "repo": repo, "path": path, "root": destination.root, "root_identity": destination.rootIdentity, "parent_identity": destination.parentIdentity, "library_sha256": library.Digest(),
	})
	if err != nil {
		return "", err
	}
	return encodeResultValue(AssetMaterializePreview{PlanID: plan.ID, ExpiresAt: plan.ExpiresAt, Repo: repo, Path: path, LibrarySHA256: library.Digest(), Asset: entry})
}

func (c *AssetCapability) AssetMaterialize(planID string, approve bool) (output string, err error) {
	span := c.log.Start("asset_materialize")
	decision := audit.Allow
	auditArgs := "reviewed raster destination"
	defer func() {
		if err != nil {
			decision = audit.Error
		}
		span.Finish(decision, auditArgs, nil, err)
	}()
	library, fetch, err := c.snapshot()
	if err != nil {
		return "", err
	}
	needsApproval, err := c.pol.CheckAction()
	if err != nil {
		return "", err
	}
	if needsApproval && !approve {
		decision = audit.Ask
		return "APPROVAL REQUIRED: asset_materialize would download and create the reviewed raster file. Re-invoke with approve=true.", nil
	}
	plan, err := c.plans.Consume(strings.TrimSpace(planID), "asset-materialize")
	if err != nil {
		return "", err
	}
	if library.Digest() != plan.Args["library_sha256"] {
		return "", errors.New("asset library changed after preview")
	}
	entry, err := library.Entry(plan.Args["id"])
	if err != nil {
		return "", err
	}
	destination, err := c.assetDestination(plan.Args["repo"], plan.Args["path"], entry.MIME)
	if err != nil {
		return "", err
	}
	if !destination.matches(plan) {
		return "", errors.New("asset destination changed after preview")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	download, err := fetch(ctx, entry.ID)
	if err != nil {
		return "", err
	}
	// The downloader owns raster validation. Rebind its immutable bytes and entry
	// here before the filesystem effect, including when a test replaces fetch.
	if download.Entry != entry || download.Size != int64(len(download.Bytes)) || download.Size < 1 || download.Size > entry.MaxBytes || fmt.Sprintf("%x", sha256.Sum256(download.Bytes)) != entry.SHA256 {
		return "", errors.New("asset download identity mismatch")
	}
	current, err := c.assetDestination(plan.Args["repo"], plan.Args["path"], entry.MIME)
	if err != nil || !current.matches(plan) {
		return "", errors.New("asset destination changed during download")
	}
	if err := createAssetBeneath(destination.root, plan.Args["path"], destination.rootIdentity, destination.parentIdentity, download.Bytes); err != nil {
		return "", err
	}
	auditArgs = fmt.Sprintf("asset_id=%s sha256=%s library_sha256=%s bytes=%d", entry.ID, entry.SHA256, library.Digest(), download.Size)
	return encodeResultValue(AssetMaterializeReceipt{Status: "materialized", Repo: plan.Args["repo"], Path: plan.Args["path"], LibrarySHA256: library.Digest(), Download: download})
}

type assetDestination struct{ root, rootIdentity, parentIdentity string }

func (d assetDestination) matches(plan ActionPlan) bool {
	return d.root == plan.Args["root"] && d.rootIdentity == plan.Args["root_identity"] && d.parentIdentity == plan.Args["parent_identity"]
}

func (c *AssetCapability) assetDestination(repo, path, media string) (assetDestination, error) {
	if (repo != "" && (!filepath.IsLocal(repo) || filepath.Clean(repo) != repo)) || !filepath.IsLocal(path) || filepath.Clean(path) != path || len(path) > 240 || strings.ContainsAny(repo+path, "\\:\x00\r\n\t") {
		return assetDestination{}, errors.New("asset destination must be an exact relative path")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if (media == "image/png" && ext != ".png") || (media == "image/jpeg" && ext != ".jpg" && ext != ".jpeg") {
		return assetDestination{}, errors.New("asset destination extension must match the raster type")
	}
	root, err := c.workdir(repo)
	if err != nil {
		return assetDestination{}, err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return assetDestination{}, errors.New("asset repository root is unsafe")
	}
	target := filepath.Join(root, path)
	resolved, _, err := c.pol.CheckWrite(target)
	if err != nil {
		return assetDestination{}, err
	}
	if target != resolved {
		return assetDestination{}, errors.New("asset destination symlinks are not allowed")
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return assetDestination{}, errors.New("asset destination must not exist")
	}
	parent := filepath.Dir(target)
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return assetDestination{}, errors.New("asset destination parent must already be a safe directory")
	}
	rootIdentity, err := assetDirectoryIdentity(rootInfo)
	if err != nil {
		return assetDestination{}, err
	}
	parentIdentity, err := assetDirectoryIdentity(parentInfo)
	if err != nil {
		return assetDestination{}, err
	}
	return assetDestination{root: root, rootIdentity: rootIdentity, parentIdentity: parentIdentity}, nil
}
