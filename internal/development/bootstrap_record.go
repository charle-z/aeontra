package development

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

const bootstrapArtifactSizeLimit int64 = 256 << 20

var (
	bootstrapRecordSHA256 = regexp.MustCompile(`^[a-f0-9]{64}$`)
	bootstrapRecordNumber = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})$`)
)

// BootstrapResolution is the server-selected, immutable identity of one
// development toolchain bootstrap. It contains official digests, never caller
// URLs or filesystem paths, and is suitable for persistence with an operation.
type BootstrapResolution struct {
	CapabilityID    CapabilityID `json:"capability_id"`
	Toolchain       string       `json:"toolchain"`
	Version         string       `json:"version"`
	Platform        string       `json:"platform"`
	ArtifactFile    string       `json:"artifact_file,omitempty"`
	ArtifactSHA256  string       `json:"artifact_sha256,omitempty"`
	ArtifactSize    int64        `json:"artifact_size,omitempty"`
	InstallerSHA256 string       `json:"installer_sha256,omitempty"`
	ManifestSHA256  string       `json:"manifest_sha256,omitempty"`
}

// BootstrapCapabilitySupported reports whether one numeric selector has an
// administrator-owned installer. It grants no capability by itself.
func BootstrapCapabilitySupported(id CapabilityID) bool {
	_, _, err := parseBootstrapCapability(id)
	return err == nil
}

// ValidateBootstrapResolution rejects non-canonical or internally inconsistent
// metadata before an operation is staged or a recipe is constructed.
func ValidateBootstrapResolution(resolution BootstrapResolution) error {
	toolchain, selector, err := parseBootstrapCapability(resolution.CapabilityID)
	if err != nil || toolchain != resolution.Toolchain {
		return errors.New("development bootstrap capability binding is invalid")
	}
	versionParts, err := parseBootstrapVersion(resolution.Version, 3)
	if err != nil || len(selector) > len(versionParts) {
		return errors.New("development bootstrap version is invalid")
	}
	for index := range selector {
		if selector[index] != versionParts[index] {
			return errors.New("development bootstrap version does not match capability")
		}
	}

	switch resolution.Toolchain {
	case "go":
		if selector[0] != "1" || resolution.Platform != "amd64" && resolution.Platform != "arm64" {
			return errors.New("go bootstrap platform or major version is unsupported")
		}
		wantFile := "go" + resolution.Version + ".linux-" + resolution.Platform + ".tar.gz"
		if resolution.ArtifactFile != wantFile || !bootstrapRecordSHA256.MatchString(resolution.ArtifactSHA256) || resolution.ArtifactSize < 1 || resolution.ArtifactSize > bootstrapArtifactSizeLimit || resolution.InstallerSHA256 != "" || resolution.ManifestSHA256 != "" {
			return errors.New("go bootstrap artifact metadata is invalid")
		}
	case "rust":
		if len(selector) != 3 || resolution.Version != strings.Join(selector, ".") || (resolution.Platform != "x86_64-unknown-linux-gnu" && resolution.Platform != "aarch64-unknown-linux-gnu") {
			return errors.New("rust bootstrap platform or version is unsupported")
		}
		if resolution.ArtifactFile != "" || resolution.ArtifactSHA256 != "" || resolution.ArtifactSize != 0 || !bootstrapRecordSHA256.MatchString(resolution.InstallerSHA256) || !bootstrapRecordSHA256.MatchString(resolution.ManifestSHA256) {
			return errors.New("rust bootstrap checksum metadata is invalid")
		}
	default:
		return errors.New("development bootstrap toolchain is unsupported")
	}
	return nil
}

// BootstrapResolutionDigest hashes the canonical JSON representation of one
// validated resolution. The digest binds retries to the original selection.
func BootstrapResolutionDigest(resolution BootstrapResolution) (string, error) {
	if err := ValidateBootstrapResolution(resolution); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(resolution)
	if err != nil {
		return "", errors.New("development bootstrap resolution cannot be encoded")
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func parseBootstrapCapability(id CapabilityID) (string, []string, error) {
	value := string(id)
	var toolchain, suffix string
	switch {
	case strings.HasPrefix(value, "toolchain.go.v"):
		toolchain, suffix = "go", strings.TrimPrefix(value, "toolchain.go.v")
	case strings.HasPrefix(value, "toolchain.rust.v"):
		toolchain, suffix = "rust", strings.TrimPrefix(value, "toolchain.rust.v")
	default:
		return "", nil, errors.New("development bootstrap capability is unsupported")
	}
	parts := strings.Split(suffix, "-")
	if toolchain == "go" && (len(parts) < 2 || len(parts) > 3) || toolchain == "rust" && len(parts) != 3 {
		return "", nil, errors.New("development bootstrap capability precision is unsupported")
	}
	for _, part := range parts {
		if !bootstrapRecordNumber.MatchString(part) {
			return "", nil, errors.New("development bootstrap capability is not canonical numeric data")
		}
	}
	if toolchain == "go" && parts[0] != "1" {
		return "", nil, errors.New("go bootstrap major version is unsupported")
	}
	return toolchain, parts, nil
}

func parseBootstrapVersion(raw string, precision int) ([]string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != precision {
		return nil, errors.New("development bootstrap version precision is invalid")
	}
	for _, part := range parts {
		if !bootstrapRecordNumber.MatchString(part) {
			return nil, errors.New("development bootstrap version is not canonical")
		}
		if _, err := strconv.Atoi(part); err != nil {
			return nil, errors.New("development bootstrap version is out of range")
		}
	}
	return parts, nil
}
