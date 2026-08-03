// Package bundle verifies the signed, indivisible set of binaries, providers and
// service definitions that make up one MCP Devbox Edge release.
package bundle

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	CurrentManifestVersion = 2

	ManifestFile  = "manifest.json"
	SignatureFile = "manifest.sig"
)

const (
	ComponentEdge            = "mcp-edge"
	ComponentDriver          = "model-turn-driver"
	ComponentWorker          = "mcp-autopilot-worker"
	ComponentUpdater         = "mcp-bundle-updater"
	ComponentNode            = "runtime-node"
	ComponentGitHubCLI       = "github-cli"
	ComponentProvider        = "provider-index"
	ComponentHTBActions      = "provider-htb-actions"
	ComponentDevActions      = "provider-dev-actions"
	ComponentProviderPackage = "provider-package"
	ComponentOpenCode        = "opencode"
	ComponentOpenCodeLock    = "opencode-lock"
	ComponentSystemd         = "systemd-unit"
)

type Code string

const (
	BundleMismatch   Code = "bundle_mismatch"
	ProviderOutdated Code = "provider_outdated"
	DriverOutdated   Code = "driver_outdated"
	ManifestInvalid  Code = "manifest_invalid"
)

var (
	releasePattern = regexp.MustCompile(`^p15\.[0-9]+\.[0-9]+$`)
	commitPattern  = regexp.MustCompile(`^[a-f0-9]{40}$`)
	digestPattern  = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type Manifest struct {
	Version         int               `json:"version"`
	Release         string            `json:"release"`
	Commit          string            `json:"commit"`
	ProtocolVersion string            `json:"protocol_version"`
	CatalogHash     string            `json:"catalog_hash"`
	Architecture    string            `json:"architecture"`
	Components      map[string]string `json:"components"`
}

type Compatibility struct {
	Release         string
	Commit          string
	ProtocolVersion string
	CatalogHash     string
	Architecture    string
}

type Metadata struct {
	Release         string
	Commit          string
	ProtocolVersion string
	CatalogHash     string
	Architecture    string
}

type Verified struct {
	Release string `json:"release"`
	Commit  string `json:"commit"`
}

type VerificationError struct {
	Code Code
}

func (e *VerificationError) Error() string { return string(e.Code) }

func RequiredComponents() []string {
	return []string{ComponentEdge, ComponentDriver, ComponentWorker, ComponentUpdater, ComponentNode, ComponentGitHubCLI, ComponentProvider, ComponentHTBActions, ComponentDevActions, ComponentProviderPackage, ComponentOpenCode, ComponentOpenCodeLock, ComponentSystemd}
}

func legacyRequiredComponents() []string {
	return []string{ComponentEdge, ComponentDriver, ComponentWorker, ComponentUpdater, ComponentNode, ComponentProvider, ComponentHTBActions, ComponentProviderPackage, ComponentOpenCode, ComponentOpenCodeLock, ComponentSystemd}
}

func versionTwoRequiredComponents() []string {
	return []string{ComponentEdge, ComponentDriver, ComponentWorker, ComponentUpdater, ComponentNode, ComponentProvider, ComponentHTBActions, ComponentDevActions, ComponentProviderPackage, ComponentOpenCode, ComponentOpenCodeLock, ComponentSystemd}
}

func requiredComponentsForVersion(version int) ([]string, bool) {
	switch version {
	case 1:
		return legacyRequiredComponents(), true
	case 2:
		return versionTwoRequiredComponents(), true
	case 3:
		return RequiredComponents(), true
	default:
		return nil, false
	}
}

func DefaultLayout() map[string]string {
	return map[string]string{
		ComponentEdge:            "bin/mcp-edge",
		ComponentDriver:          "libexec/model-turn-driver",
		ComponentWorker:          "libexec/mcp-autopilot-worker",
		ComponentUpdater:         "libexec/mcp-bundle-updater",
		ComponentNode:            "libexec/node",
		ComponentGitHubCLI:       "libexec/gh",
		ComponentProvider:        "opencode-provider/index.js",
		ComponentHTBActions:      "opencode-provider/htb-actions.js",
		ComponentDevActions:      "opencode-provider/dev-actions.js",
		ComponentProviderPackage: "opencode-provider/package.json",
		ComponentOpenCode:        "opencode/opencode",
		ComponentOpenCodeLock:    "opencode/package-lock.json",
		ComponentSystemd:         "systemd/mcp-devbox-opencode-edge@.service",
	}
}

func layoutForVersion(version int) (map[string]string, bool) {
	layout := DefaultLayout()
	if version == 1 {
		delete(layout, ComponentDevActions)
		delete(layout, ComponentGitHubCLI)
		return layout, true
	}
	if version == 2 {
		delete(layout, ComponentGitHubCLI)
		return layout, true
	}
	if version == 3 {
		return layout, true
	}
	return nil, false
}

func Build(root string, metadata Metadata) (Manifest, error) {
	manifest := Manifest{
		Version: CurrentManifestVersion, Release: metadata.Release, Commit: metadata.Commit,
		ProtocolVersion: metadata.ProtocolVersion, CatalogHash: metadata.CatalogHash,
		Architecture: metadata.Architecture, Components: map[string]string{},
	}
	layout, _ := layoutForVersion(manifest.Version)
	for component, relative := range layout {
		path, err := containedComponentPath(root, relative)
		if err != nil {
			return Manifest{}, componentError(component)
		}
		digest, err := HashFile(path)
		if err != nil {
			return Manifest{}, componentError(component)
		}
		manifest.Components[component] = digest
	}
	if _, err := canonicalManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func LoadAndVerify(root string, publicKey ed25519.PublicKey, expected Compatibility) (Verified, error) {
	manifest, signature, err := loadManifestFiles(root)
	if err != nil {
		return Verified{}, err
	}
	layout, ok := layoutForVersion(manifest.Version)
	if !ok {
		return Verified{}, &VerificationError{Code: ManifestInvalid}
	}
	return Verify(root, manifest, signature, publicKey, layout, expected)
}

func LoadTrusted(root string, publicKey ed25519.PublicKey) (Verified, error) {
	manifest, signature, err := loadManifestFiles(root)
	if err != nil {
		return Verified{}, err
	}
	layout, ok := layoutForVersion(manifest.Version)
	if !ok {
		return Verified{}, &VerificationError{Code: ManifestInvalid}
	}
	return Verify(root, manifest, signature, publicKey, layout, Compatibility{
		Release: manifest.Release, Commit: manifest.Commit, ProtocolVersion: manifest.ProtocolVersion,
		CatalogHash: manifest.CatalogHash, Architecture: manifest.Architecture,
	})
}

func loadManifestFiles(root string) (Manifest, []byte, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	manifestBytes, err := readMetadataFile(root, ManifestFile, 64<<10)
	if err != nil {
		return Manifest{}, nil, &VerificationError{Code: ManifestInvalid}
	}
	signature, err := readMetadataFile(root, SignatureFile, ed25519.SignatureSize)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return Manifest{}, nil, &VerificationError{Code: ManifestInvalid}
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, nil, &VerificationError{Code: ManifestInvalid}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Manifest{}, nil, &VerificationError{Code: ManifestInvalid}
	}
	return manifest, signature, nil
}

func Sign(manifest Manifest, privateKey ed25519.PrivateKey) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("bundle signing key is invalid")
	}
	canonical, err := canonicalManifest(manifest)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(privateKey, canonical), nil
}

func Verify(root string, manifest Manifest, signature []byte, publicKey ed25519.PublicKey, paths map[string]string, expected Compatibility) (Verified, error) {
	canonical, err := canonicalManifest(manifest)
	if err != nil || len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, canonical, signature) {
		return Verified{}, &VerificationError{Code: ManifestInvalid}
	}
	if expected.Release == "" || manifest.Release != expected.Release ||
		expected.Commit == "" || manifest.Commit != expected.Commit ||
		expected.ProtocolVersion == "" || manifest.ProtocolVersion != expected.ProtocolVersion ||
		expected.CatalogHash == "" || manifest.CatalogHash != expected.CatalogHash ||
		expected.Architecture == "" || manifest.Architecture != expected.Architecture {
		return Verified{}, &VerificationError{Code: BundleMismatch}
	}
	required, ok := requiredComponentsForVersion(manifest.Version)
	if !ok {
		return Verified{}, &VerificationError{Code: ManifestInvalid}
	}
	for _, component := range required {
		relative, ok := paths[component]
		if !ok || strings.TrimSpace(relative) == "" {
			return Verified{}, componentError(component)
		}
		path, err := containedComponentPath(root, relative)
		if err != nil {
			return Verified{}, componentError(component)
		}
		digest, err := HashFile(path)
		if err != nil || digest != manifest.Components[component] {
			return Verified{}, componentError(component)
		}
	}
	return Verified{Release: manifest.Release, Commit: manifest.Commit}, nil
}

func canonicalManifest(manifest Manifest) ([]byte, error) {
	required, supported := requiredComponentsForVersion(manifest.Version)
	if !supported || !releasePattern.MatchString(manifest.Release) || !commitPattern.MatchString(manifest.Commit) ||
		strings.TrimSpace(manifest.ProtocolVersion) == "" || !digestPattern.MatchString(manifest.CatalogHash) ||
		(manifest.Architecture != "amd64" && manifest.Architecture != "arm64") || len(manifest.Components) != len(required) {
		return nil, &VerificationError{Code: ManifestInvalid}
	}
	sort.Strings(required)
	for _, component := range required {
		if !digestPattern.MatchString(manifest.Components[component]) {
			return nil, &VerificationError{Code: ManifestInvalid}
		}
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("encode bundle manifest: %w", err)
	}
	return encoded, nil
}

func containedComponentPath(root, relative string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if !filepath.IsAbs(root) || filepath.IsAbs(relative) || strings.TrimSpace(relative) == "" {
		return "", errors.New("bundle component path is invalid")
	}
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("bundle component escapes release root")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("bundle component is unavailable")
	}
	return path, nil
}

func readMetadataFile(root, name string, limit int64) ([]byte, error) {
	path, err := containedComponentPath(root, name)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(content)) > limit {
		return nil, errors.New("bundle metadata is invalid")
	}
	return content, nil
}

func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("bundle component is not a regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func componentError(component string) error {
	switch component {
	case ComponentProvider, ComponentHTBActions, ComponentDevActions, ComponentProviderPackage:
		return &VerificationError{Code: ProviderOutdated}
	case ComponentDriver:
		return &VerificationError{Code: DriverOutdated}
	default:
		return &VerificationError{Code: BundleMismatch}
	}
}
