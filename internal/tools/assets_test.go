//go:build linux

package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/assets"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
)

func assetTestLibrary(t *testing.T) (*assets.Library, assets.Download) {
	t.Helper()
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	entry := assets.Entry{ID: "example", Title: "Example logo", URL: "https://images.example.com/logo.png", SHA256: fmt.Sprintf("%x", sha256.Sum256(body.Bytes())), MIME: "image/png", MaxBytes: 4096, License: "CC0-1.0", LicenseURL: "https://example.com/license", Attribution: "Example author", Provenance: "https://example.com/original", ReviewedBy: "operator", ReviewedAt: "2026-10-07"}
	encoded, _ := json.Marshal(map[string]any{"version": 1, "assets": []assets.Entry{entry}})
	library, err := assets.ParseLibrary(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return library, assets.Download{Entry: entry, Bytes: body.Bytes(), Size: int64(body.Len()), Width: 2, Height: 2}
}

func assetConfiguredService(t *testing.T, mode config.Mode) (*Service, string, *int, assets.Download) {
	t.Helper()
	svc, root := newTestService(t, mode)
	library, download := assetTestLibrary(t)
	svc.WithAssetLibrary(library)
	calls := 0
	svc.AssetCapability.fetch = func(context.Context, string) (assets.Download, error) { calls++; return download, nil }
	return svc, root, &calls, download
}

func assetPlan(t *testing.T, svc *Service, path string) AssetMaterializePreview {
	t.Helper()
	out, err := svc.AssetMaterializePreview("example", "", path)
	if err != nil {
		t.Fatal(err)
	}
	var preview AssetMaterializePreview
	if err := json.Unmarshal([]byte(out), &preview); err != nil {
		t.Fatal(err)
	}
	return preview
}

func TestAssetSearchDisabledAndReadOnlyLocal(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	if _, err := svc.AssetSearch("logo", 5); !errors.Is(err, ErrAssetsNotConfigured) {
		t.Fatalf("disabled=%v", err)
	}
	svc, _, calls, _ := assetConfiguredService(t, config.ModeReadOnly)
	out, err := svc.AssetSearch("logo", 5)
	if err != nil || !strings.Contains(out, "CC0-1.0") || *calls != 0 {
		t.Fatalf("search=%s err=%v calls=%d", out, err, *calls)
	}
	if _, err := svc.AssetMaterializePreview("example", "", "logo.png"); !errors.Is(err, policy.ErrReadOnly) {
		t.Fatalf("read-only=%v", err)
	}
}

func TestAssetMaterializeReviewedBytesAskAndSingleUse(t *testing.T) {
	svc, root, calls, download := assetConfiguredService(t, config.ModeAsk)
	preview := assetPlan(t, svc, "logo.png")
	if preview.Asset.SHA256 != download.SHA256 || preview.Path != "logo.png" || *calls != 0 {
		t.Fatalf("preview=%+v calls=%d", preview, *calls)
	}
	out, err := svc.AssetMaterialize(preview.PlanID, false)
	if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") || *calls != 0 {
		t.Fatalf("ask=%s err=%v", out, err)
	}
	out, err = svc.AssetMaterialize(preview.PlanID, true)
	if err != nil || !strings.Contains(out, download.SHA256) || !strings.Contains(out, "Example author") || *calls != 1 {
		t.Fatalf("materialize=%s err=%v", out, err)
	}
	body, err := os.ReadFile(filepath.Join(root, "logo.png"))
	if err != nil || !bytes.Equal(body, download.Bytes) {
		t.Fatalf("bytes=%v err=%v", body, err)
	}
	if _, err := svc.AssetMaterialize(preview.PlanID, true); err == nil {
		t.Fatal("plan replay accepted")
	}
}

func TestAssetPreviewRejectsUnsafeAndExistingDestinations(t *testing.T) {
	svc, root, calls, _ := assetConfiguredService(t, config.ModeAllow)
	write(t, root, "existing.png", "irreplaceable")
	for _, path := range []string{"../outside.png", filepath.Join(root, "absolute.png"), ".env/logo.png", ".git/config", "existing.png", "missing/logo.png", "bad.txt", "a\\logo.png"} {
		if _, err := svc.AssetMaterializePreview("example", "", path); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	if _, err := svc.AssetMaterializePreview("example", "../outside", "logo.png"); err == nil {
		t.Fatal("outside repo accepted")
	}
	if *calls != 0 {
		t.Fatal("preview downloaded")
	}
}

func TestAssetMaterializeRevalidatesDestinationAndLibrary(t *testing.T) {
	svc, root, calls, _ := assetConfiguredService(t, config.ModeAllow)
	preview := assetPlan(t, svc, "logo.png")
	write(t, root, "logo.png", "user-created")
	if _, err := svc.AssetMaterialize(preview.PlanID, true); err == nil || *calls != 0 {
		t.Fatal("destination drift downloaded or overwrote")
	}
	preview = assetPlan(t, svc, "other.png")
	svc.WithAssetLibrary(nil)
	if _, err := svc.AssetMaterialize(preview.PlanID, true); !errors.Is(err, ErrAssetsNotConfigured) {
		t.Fatalf("disabled=%v", err)
	}
	if *calls != 0 {
		t.Fatal("disabled downloaded")
	}
}

func TestAssetMaterializeRacePreservesUserFile(t *testing.T) {
	svc, root, _, download := assetConfiguredService(t, config.ModeAllow)
	preview := assetPlan(t, svc, "logo.png")
	svc.AssetCapability.fetch = func(context.Context, string) (assets.Download, error) {
		write(t, root, "logo.png", "racing user file")
		return download, nil
	}
	if _, err := svc.AssetMaterialize(preview.PlanID, true); err == nil {
		t.Fatal("raced target accepted")
	}
	body, _ := os.ReadFile(filepath.Join(root, "logo.png"))
	if string(body) != "racing user file" {
		t.Fatal("raced user bytes changed")
	}
}

func TestAssetPreviewRejectsSymlinkAndExecutionRootDrift(t *testing.T) {
	svc, root, calls, _ := assetConfiguredService(t, config.ModeAllow)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skip(err)
	}
	if _, err := svc.AssetMaterializePreview("example", "", "escape/logo.png"); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Mkdir(filepath.Join(root, "images"), 0755); err != nil {
		t.Fatal(err)
	}
	preview := assetPlan(t, svc, "images/logo.png")
	if err := os.Rename(filepath.Join(root, "images"), filepath.Join(root, "old-images")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "images"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AssetMaterialize(preview.PlanID, true); err == nil || *calls != 0 {
		t.Fatal("parent identity drift accepted")
	}
}

func TestAssetMaterializeExpiredPlanDoesNotDownload(t *testing.T) {
	svc, _, calls, _ := assetConfiguredService(t, config.ModeAllow)
	now := time.Now()
	svc.plans.now = func() time.Time { return now }
	preview := assetPlan(t, svc, "logo.png")
	now = now.Add(6 * time.Minute)
	if _, err := svc.AssetMaterialize(preview.PlanID, true); err == nil || *calls != 0 {
		t.Fatal("expired plan downloaded")
	}
}
