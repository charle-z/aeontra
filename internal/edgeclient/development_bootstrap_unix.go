//go:build !windows

package edgeclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/development"
)

const (
	developmentBootstrapGoMetadataURL = "https://go.dev/dl/?mode=json&include=all"
	developmentBootstrapRustupRoot    = "https://static.rust-lang.org/rustup/archive"
	developmentBootstrapRustDistRoot  = "https://static.rust-lang.org/dist"
	developmentBootstrapRustupVersion = "1.29.0"
	developmentBootstrapMetadataLimit = 16 << 20
	developmentBootstrapSHA256Limit   = 4096
	developmentBootstrapArtifactLimit = 256 << 20
	bootstrapRecipeHardTimeout        = 30 * time.Minute
	bootstrapRecipeKillGrace          = 10 * time.Second
)

var (
	bootstrapSHA256Pattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
	bootstrapNumericPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})$`)
)

// DevelopmentBootstrapResolution is shared with the cross-platform Edge
// request record so the server can persist and validate the exact official
// selection before starting an effect.
type DevelopmentBootstrapResolution = development.BootstrapResolution

// DevelopmentBootstrapRecipe contains the fixed argv and runtime-only
// environment used by the durable project process manager.
type DevelopmentBootstrapRecipe struct {
	Resolution  DevelopmentBootstrapResolution
	Argv        []string
	Environment map[string]string
	Timeout     time.Duration
}

type bootstrapSelector struct {
	toolchain string
	parts     []string
}

type goBootstrapRelease struct {
	Version string            `json:"version"`
	Stable  bool              `json:"stable"`
	Files   []goBootstrapFile `json:"files"`
}

type goBootstrapFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Kind     string `json:"kind"`
}

// ResolveDevelopmentBootstrap accepts only canonical capability IDs. A Go
// minor pin resolves to the highest numeric stable patch from go.dev's bounded
// JSON catalog. Rust currently requires an exact three-part release pin.
func ResolveDevelopmentBootstrap(ctx context.Context, capabilityID development.CapabilityID) (DevelopmentBootstrapResolution, error) {
	if runtime.GOOS != "linux" {
		return DevelopmentBootstrapResolution{}, errors.New("development bootstrap runtime must be Linux")
	}
	selector, err := parseDevelopmentBootstrapSelector(capabilityID)
	if err != nil {
		return DevelopmentBootstrapResolution{}, err
	}
	if ctx == nil {
		return DevelopmentBootstrapResolution{}, errors.New("development bootstrap context is invalid")
	}
	client := developmentBootstrapHTTPClient()
	switch selector.toolchain {
	case "go":
		architecture, err := goBootstrapArchitecture(runtime.GOARCH)
		if err != nil {
			return DevelopmentBootstrapResolution{}, err
		}
		body, err := boundedDevelopmentBootstrapGET(ctx, client, developmentBootstrapGoMetadataURL, developmentBootstrapMetadataLimit, "go.dev")
		if err != nil {
			return DevelopmentBootstrapResolution{}, err
		}
		resolution, err := selectGoBootstrapArtifact(body, capabilityID, architecture)
		if err != nil {
			return DevelopmentBootstrapResolution{}, err
		}
		if err := development.ValidateBootstrapResolution(resolution); err != nil {
			return DevelopmentBootstrapResolution{}, err
		}
		return resolution, nil
	case "rust":
		if len(selector.parts) != 3 {
			return DevelopmentBootstrapResolution{}, errors.New("rust bootstrap requires an exact numeric version")
		}
		version := strings.Join(selector.parts, ".")
		target, err := rustBootstrapTarget(runtime.GOARCH)
		if err != nil {
			return DevelopmentBootstrapResolution{}, err
		}
		installerURL := developmentBootstrapRustupRoot + "/" + developmentBootstrapRustupVersion + "/" + target + "/rustup-init.sha256"
		installerBody, err := boundedDevelopmentBootstrapGET(ctx, client, installerURL, developmentBootstrapSHA256Limit, "static.rust-lang.org")
		if err != nil {
			return DevelopmentBootstrapResolution{}, err
		}
		installerDigest, err := parseOfficialSHA256Sidecar(installerBody, "rustup-init")
		if err != nil {
			return DevelopmentBootstrapResolution{}, err
		}
		manifestURL := developmentBootstrapRustDistRoot + "/channel-rust-" + version + ".toml.sha256"
		manifestBody, err := boundedDevelopmentBootstrapGET(ctx, client, manifestURL, developmentBootstrapSHA256Limit, "static.rust-lang.org")
		if err != nil {
			return DevelopmentBootstrapResolution{}, err
		}
		manifestDigest, err := parseOfficialSHA256Sidecar(manifestBody, "channel-rust-"+version+".toml")
		if err != nil {
			return DevelopmentBootstrapResolution{}, err
		}
		resolution := DevelopmentBootstrapResolution{
			CapabilityID: capabilityID, Toolchain: "rust", Version: version, Platform: target,
			InstallerSHA256: installerDigest, ManifestSHA256: manifestDigest,
		}
		if err := development.ValidateBootstrapResolution(resolution); err != nil {
			return DevelopmentBootstrapResolution{}, err
		}
		return resolution, nil
	default:
		return DevelopmentBootstrapResolution{}, errors.New("development bootstrap capability is unsupported")
	}
}

// DevelopmentBootstrapResolutionDigest is stable over the complete resolved
// selection and can be bound into a private operation or idempotency key.
func DevelopmentBootstrapResolutionDigest(resolution DevelopmentBootstrapResolution) (string, error) {
	return development.BootstrapResolutionDigest(resolution)
}

// BuildDevelopmentBootstrapRecipe turns a saved official selection into a
// fixed /bin/sh argv. The recipe accepts no caller argv, URL, shell, or path.
func BuildDevelopmentBootstrapRecipe(resolution DevelopmentBootstrapResolution) (DevelopmentBootstrapRecipe, error) {
	if err := development.ValidateBootstrapResolution(resolution); err != nil {
		return DevelopmentBootstrapRecipe{}, err
	}
	if err := validateDevelopmentBootstrapRuntime(resolution); err != nil {
		return DevelopmentBootstrapRecipe{}, err
	}
	selector, err := parseDevelopmentBootstrapSelector(resolution.CapabilityID)
	if err != nil {
		return DevelopmentBootstrapRecipe{}, err
	}
	var script string
	var environment map[string]string
	switch resolution.Toolchain {
	case "go":
		if selector.toolchain != "go" || resolution.Platform != "amd64" && resolution.Platform != "arm64" || !bootstrapSHA256Pattern.MatchString(resolution.ArtifactSHA256) || resolution.ArtifactSize < 1 || resolution.ArtifactSize > developmentBootstrapArtifactLimit {
			return DevelopmentBootstrapRecipe{}, errors.New("go bootstrap selection is invalid")
		}
		versionParts, err := canonicalBootstrapNumericVersion(resolution.Version, 3)
		if err != nil || !bootstrapSelectorMatches(selector, versionParts) {
			return DevelopmentBootstrapRecipe{}, errors.New("go bootstrap version is invalid")
		}
		version := strings.Join(versionParts, ".")
		filename := "go" + version + ".linux-" + resolution.Platform + ".tar.gz"
		if resolution.ArtifactFile != filename {
			return DevelopmentBootstrapRecipe{}, errors.New("go bootstrap artifact is invalid")
		}
		script = goDevelopmentBootstrapScript(version, filename, resolution.ArtifactSHA256, resolution.ArtifactSize)
		environment = map[string]string{
			"GOPATH": "/runtime/go", "GOBIN": "/runtime/tools/bin",
			"GOMODCACHE": "/cache/go-mod", "GOCACHE": "/cache/go-build",
		}
	case "rust":
		if selector.toolchain != "rust" || len(selector.parts) != 3 || resolution.Platform != "x86_64-unknown-linux-gnu" && resolution.Platform != "aarch64-unknown-linux-gnu" || !bootstrapSHA256Pattern.MatchString(resolution.InstallerSHA256) || !bootstrapSHA256Pattern.MatchString(resolution.ManifestSHA256) {
			return DevelopmentBootstrapRecipe{}, errors.New("rust bootstrap selection is invalid")
		}
		version := strings.Join(selector.parts, ".")
		if resolution.Version != version {
			return DevelopmentBootstrapRecipe{}, errors.New("rust bootstrap version is invalid")
		}
		script = rustDevelopmentBootstrapScript(version, resolution.Platform, resolution.InstallerSHA256, resolution.ManifestSHA256)
		environment = map[string]string{"CARGO_HOME": "/runtime/cargo", "RUSTUP_HOME": "/runtime/rustup"}
	default:
		return DevelopmentBootstrapRecipe{}, errors.New("development bootstrap toolchain is unsupported")
	}
	return DevelopmentBootstrapRecipe{
		Resolution:  resolution,
		Argv:        []string{"/bin/sh", "-c", script, "mcp-devbox-development-bootstrap"},
		Environment: environment,
		Timeout:     bootstrapRecipeHardTimeout + bootstrapRecipeKillGrace,
	}, nil
}

// DevelopmentBootstrapCapabilities grants only numeric capabilities observed
// by the recipe's post-install version commands and emitted marker. Callers
// must additionally require a known process exit code of zero.
func DevelopmentBootstrapCapabilities(resolution DevelopmentBootstrapResolution, output string) (development.CapabilitySet, error) {
	if err := development.ValidateBootstrapResolution(resolution); err != nil {
		return development.CapabilitySet{}, errors.New("development bootstrap resolution is invalid")
	}
	if err := validateDevelopmentBootstrapRuntime(resolution); err != nil {
		return development.CapabilitySet{}, err
	}
	selector, err := parseDevelopmentBootstrapSelector(resolution.CapabilityID)
	if err != nil {
		return development.CapabilitySet{}, err
	}
	var marker string
	var observedVersion string
	switch resolution.Toolchain {
	case "go":
		parts, versionErr := canonicalBootstrapNumericVersion(resolution.Version, 3)
		if versionErr != nil || selector.toolchain != "go" || !bootstrapSelectorMatches(selector, parts) {
			return development.CapabilitySet{}, errors.New("go bootstrap attestation binding is invalid")
		}
		observedVersion = strings.Join(parts, ".")
		marker = "mcp-devbox-bootstrap-verified=go:" + observedVersion
		if !containsGoBootstrapVersionOutput(output, observedVersion, resolution.Platform) {
			return development.CapabilitySet{}, errors.New("go runtime version was not verified")
		}
	case "rust":
		if selector.toolchain != "rust" || len(selector.parts) != 3 || resolution.Version != strings.Join(selector.parts, ".") {
			return development.CapabilitySet{}, errors.New("rust bootstrap attestation binding is invalid")
		}
		observedVersion = resolution.Version
		marker = "mcp-devbox-bootstrap-verified=rust:" + observedVersion + ":cargo:" + observedVersion
		if !containsRustBootstrapVersionOutput(output, observedVersion, "rustc") || !containsRustBootstrapVersionOutput(output, observedVersion, "cargo") {
			return development.CapabilitySet{}, errors.New("rust runtime versions were not verified")
		}
	default:
		return development.CapabilitySet{}, errors.New("development bootstrap attestation is unsupported")
	}
	if !containsExactBootstrapMarker(output, marker) {
		return development.CapabilitySet{}, errors.New("development bootstrap completion marker is missing")
	}
	var names []string
	bases := []string{"toolchain.go"}
	if resolution.Toolchain == "rust" {
		bases = []string{"toolchain.rust", "toolchain.cargo"}
	}
	for _, base := range bases {
		versioned, err := development.VersionCapabilityIDs(base, observedVersion)
		if err != nil {
			return development.CapabilitySet{}, errors.New("development bootstrap capabilities are invalid")
		}
		for _, id := range versioned {
			names = append(names, string(id))
		}
	}
	return development.NewCapabilitySet(names...)
}

func parseDevelopmentBootstrapSelector(id development.CapabilityID) (bootstrapSelector, error) {
	value := string(id)
	var toolchain, suffix string
	switch {
	case strings.HasPrefix(value, "toolchain.go.v"):
		toolchain, suffix = "go", strings.TrimPrefix(value, "toolchain.go.v")
	case strings.HasPrefix(value, "toolchain.rust.v"):
		toolchain, suffix = "rust", strings.TrimPrefix(value, "toolchain.rust.v")
	default:
		return bootstrapSelector{}, errors.New("development bootstrap capability is unsupported")
	}
	parts := strings.Split(suffix, "-")
	if toolchain == "go" && (len(parts) < 2 || len(parts) > 3) || toolchain == "rust" && len(parts) != 3 {
		return bootstrapSelector{}, errors.New("development bootstrap version precision is unsupported")
	}
	for _, part := range parts {
		if !bootstrapNumericPattern.MatchString(part) {
			return bootstrapSelector{}, errors.New("development bootstrap version is not canonical numeric data")
		}
	}
	if toolchain == "go" && parts[0] != "1" {
		return bootstrapSelector{}, errors.New("go bootstrap major version is unsupported")
	}
	return bootstrapSelector{toolchain: toolchain, parts: parts}, nil
}

func selectGoBootstrapArtifact(body []byte, capabilityID development.CapabilityID, architecture string) (DevelopmentBootstrapResolution, error) {
	selector, err := parseDevelopmentBootstrapSelector(capabilityID)
	if err != nil || selector.toolchain != "go" || architecture != "amd64" && architecture != "arm64" {
		return DevelopmentBootstrapResolution{}, errors.New("go bootstrap selector is unsupported")
	}
	if len(body) == 0 || len(body) > developmentBootstrapMetadataLimit {
		return DevelopmentBootstrapResolution{}, errors.New("go release metadata is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var releases []goBootstrapRelease
	if err := decoder.Decode(&releases); err != nil || len(releases) == 0 || len(releases) > 4096 {
		return DevelopmentBootstrapResolution{}, errors.New("go release metadata is invalid")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return DevelopmentBootstrapResolution{}, errors.New("go release metadata has trailing data")
	}
	var selected *DevelopmentBootstrapResolution
	selectedPatch := -1
	for _, release := range releases {
		if len(release.Files) > 512 {
			return DevelopmentBootstrapResolution{}, errors.New("go release metadata contains too many artifacts")
		}
		parts, err := parseGoReleaseVersion(release.Version)
		if err != nil || !release.Stable || !bootstrapSelectorMatches(selector, parts) {
			continue
		}
		patch, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		for _, file := range release.Files {
			filename := "go" + strings.Join(parts, ".") + ".linux-" + architecture + ".tar.gz"
			if file.OS != "linux" || file.Arch != architecture || file.Kind != "archive" || file.Version != release.Version || file.Filename != filename {
				continue
			}
			if !bootstrapSHA256Pattern.MatchString(file.SHA256) || file.Size < 1 || file.Size > developmentBootstrapArtifactLimit {
				return DevelopmentBootstrapResolution{}, errors.New("go release checksum or size is invalid")
			}
			if selected != nil && patch == selectedPatch {
				return DevelopmentBootstrapResolution{}, errors.New("go release metadata contains a duplicate artifact")
			}
			if selected != nil && patch < selectedPatch {
				continue
			}
			selectedPatch = patch
			selected = &DevelopmentBootstrapResolution{CapabilityID: capabilityID, Toolchain: "go", Version: strings.Join(parts, "."), Platform: architecture,
				ArtifactFile: filename, ArtifactSHA256: file.SHA256, ArtifactSize: file.Size}
		}
	}
	if selected == nil {
		return DevelopmentBootstrapResolution{}, errors.New("requested Go bootstrap version is unavailable")
	}
	return *selected, nil
}

func parseGoReleaseVersion(raw string) ([]string, error) {
	if !strings.HasPrefix(raw, "go") {
		return nil, errors.New("go release version is invalid")
	}
	return canonicalBootstrapNumericVersion(strings.TrimPrefix(raw, "go"), 3)
}

func canonicalBootstrapNumericVersion(raw string, precision int) ([]string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != precision {
		return nil, errors.New("development bootstrap version precision is invalid")
	}
	for _, part := range parts {
		if !bootstrapNumericPattern.MatchString(part) {
			return nil, errors.New("development bootstrap version is invalid")
		}
	}
	return parts, nil
}

func bootstrapSelectorMatches(selector bootstrapSelector, versionParts []string) bool {
	if len(versionParts) != 3 || len(selector.parts) > len(versionParts) {
		return false
	}
	for index, selected := range selector.parts {
		if selected != versionParts[index] {
			return false
		}
	}
	return true
}

func goBootstrapArchitecture(architecture string) (string, error) {
	if architecture != "amd64" && architecture != "arm64" {
		return "", errors.New("go bootstrap architecture is unsupported")
	}
	return architecture, nil
}

func rustBootstrapTarget(architecture string) (string, error) {
	switch architecture {
	case "amd64":
		return "x86_64-unknown-linux-gnu", nil
	case "arm64":
		return "aarch64-unknown-linux-gnu", nil
	default:
		return "", errors.New("rust bootstrap architecture is unsupported")
	}
}

func validateDevelopmentBootstrapRuntime(resolution DevelopmentBootstrapResolution) error {
	if runtime.GOOS != "linux" {
		return errors.New("development bootstrap runtime must be Linux")
	}
	if resolution.Toolchain == "go" {
		architecture, err := goBootstrapArchitecture(runtime.GOARCH)
		if err != nil || resolution.Platform != architecture {
			return errors.New("go bootstrap runtime architecture does not match the resolved artifact")
		}
		return nil
	}
	if resolution.Toolchain == "rust" {
		target, err := rustBootstrapTarget(runtime.GOARCH)
		if err != nil || resolution.Platform != target {
			return errors.New("rust bootstrap runtime architecture does not match the resolved toolchain")
		}
		return nil
	}
	return errors.New("development bootstrap runtime toolchain is unsupported")
}

func parseOfficialSHA256Sidecar(body []byte, expectedFile string) (string, error) {
	if len(body) == 0 || len(body) > developmentBootstrapSHA256Limit {
		return "", errors.New("official checksum metadata is invalid")
	}
	fields := strings.Fields(string(body))
	if len(fields) < 1 || len(fields) > 2 || !bootstrapSHA256Pattern.MatchString(fields[0]) {
		return "", errors.New("official checksum metadata is invalid")
	}
	if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") != expectedFile {
		return "", errors.New("official checksum metadata names another artifact")
	}
	return fields[0], nil
}

func developmentBootstrapHTTPClient() *http.Client {
	standardTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		standardTransport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	transport := standardTransport.Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func boundedDevelopmentBootstrapGET(ctx context.Context, client *http.Client, rawURL string, limit int64, expectedHost string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil || request.URL.Scheme != "https" || request.URL.Host != expectedHost || request.URL.User != nil || request.URL.Fragment != "" {
		return nil, errors.New("official bootstrap metadata endpoint is invalid")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("official bootstrap metadata is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Request == nil || response.Request.URL == nil || response.Request.URL.Scheme != "https" || response.Request.URL.Host != expectedHost {
		return nil, errors.New("official bootstrap metadata response is invalid")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, errors.New("official bootstrap metadata exceeded its limit")
	}
	return body, nil
}

func containsGoBootstrapVersionOutput(output, version, architecture string) bool {
	for _, line := range strings.Split(output, "\n") {
		if line == "go version go"+version+" linux/"+architecture {
			return true
		}
	}
	return false
}

func containsRustBootstrapVersionOutput(output, version, executable string) bool {
	prefix := executable + " " + version + " "
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func containsExactBootstrapMarker(output, marker string) bool {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		if line == marker {
			count++
		}
	}
	return count == 1
}

func goDevelopmentBootstrapScript(version, filename, digest string, size int64) string {
	inner := fmt.Sprintf(`set -eu
umask 077
export GOPATH=/runtime/go GOBIN=/runtime/tools/bin GOMODCACHE=/cache/go-mod GOCACHE=/cache/go-build
version=%q
artifact=%q
digest=%q
artifact_size=%d
toolroot=/runtime/tools
versions="$toolroot/go/.versions"
bindir="$toolroot/bin"
destination="$versions/go$version"
[ -d /runtime ] && [ ! -L /runtime ] || exit 70
if [ ! -e "$toolroot" ]; then mkdir "$toolroot"; fi
[ -d "$toolroot" ] && [ ! -L "$toolroot" ] || exit 70
if [ ! -e "$toolroot/go" ]; then mkdir "$toolroot/go"; fi
[ -d "$toolroot/go" ] && [ ! -L "$toolroot/go" ] || exit 70
if [ ! -e "$versions" ]; then mkdir "$versions"; fi
[ -d "$versions" ] && [ ! -L "$versions" ] || exit 70
if [ ! -e "$bindir" ]; then mkdir "$bindir"; fi
[ -d "$bindir" ] && [ ! -L "$bindir" ] || exit 70
if [ -e "$destination" ] || [ -L "$destination" ]; then
  [ -d "$destination" ] && [ ! -L "$destination" ] && [ -x "$destination/bin/go" ] && [ ! -L "$destination/bin/go" ] || exit 70
  observed=$("$destination/bin/go" version)
  case "$observed" in "go version go$version "*) ;; *) exit 70 ;; esac
else
  stage=$(mktemp -d "$versions/.stage-go$version.XXXXXX")
  archive="$stage/go.tar.gz"
  effective="$stage/download-url"
  trap 'rm -f -- "${archive:-}" "${effective:-}"' EXIT HUP INT TERM
  curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --max-redirs 1 --tlsv1.2 --connect-timeout 15 --max-time 900 --max-filesize "$artifact_size" --output "$archive" --write-out '%%{url_effective}' "https://go.dev/dl/$artifact" >"$effective"
  downloaded_url=$(cat "$effective")
  case "$downloaded_url" in https://go.dev/dl/*|https://dl.google.com/go/*) ;; *) exit 70 ;; esac
  printf '%%s  %%s\n' "$digest" "$archive" | sha256sum -c - >/dev/null
  tar -xzf "$archive" -C "$stage"
  [ -x "$stage/go/bin/go" ] && [ ! -L "$stage/go/bin/go" ] || exit 70
  observed=$("$stage/go/bin/go" version)
  case "$observed" in "go version go$version "*) ;; *) exit 70 ;; esac
  [ ! -e "$destination" ] && [ ! -L "$destination" ] || exit 70
  mv -T -- "$stage/go" "$destination"
  rm -f -- "$archive" "$effective"
  rmdir "$stage"
  trap - EXIT HUP INT TERM
fi
[ -x "$destination/bin/go" ] && [ ! -L "$destination/bin/go" ] || exit 70
observed=$("$destination/bin/go" version)
case "$observed" in "go version go$version "*) ;; *) exit 70 ;; esac
if [ -e "$bindir/go" ] && [ ! -L "$bindir/go" ]; then exit 70; fi
if [ -L "$bindir/go" ]; then
  current=$(readlink "$bindir/go")
  case "$current" in "$versions"/go1.*/bin/go) ;; *) exit 70 ;; esac
fi
link=$(mktemp "$bindir/.go-link.XXXXXX")
rm -f -- "$link"
ln -s "$destination/bin/go" "$link"
mv -Tf -- "$link" "$bindir/go"
observed=$("$bindir/go" version)
case "$observed" in "go version go$version "*) ;; *) exit 70 ;; esac
printf '%%s\nmcp-devbox-bootstrap-verified=go:%%s\n' "$observed" "$version"
`, version, filename, digest, size)
	return developmentBootstrapLockedScript(inner)
}

func rustDevelopmentBootstrapScript(version, target, installerDigest, manifestDigest string) string {
	inner := fmt.Sprintf(`set -eu
umask 077
export CARGO_HOME=/runtime/cargo RUSTUP_HOME=/runtime/rustup
export RUSTUP_DIST_SERVER=https://static.rust-lang.org
version=%q
target=%q
installer_digest=%q
manifest_digest=%q
stage=$(mktemp -d /runtime/.stage-rust-%s.XXXXXX)
installer="$stage/rustup-init"
installer_sum="$stage/rustup-init.sha256"
manifest="$stage/channel-rust-$version.toml"
manifest_sum="$stage/channel-rust-$version.toml.sha256"
trap 'rm -f -- "${installer:-}" "${installer_sum:-}" "${manifest:-}" "${manifest_sum:-}"' EXIT HUP INT TERM
set -f
curl --fail --silent --show-error --proto '=https' --tlsv1.2 --connect-timeout 15 --max-time 900 --max-filesize 4096 --output "$installer_sum" "https://static.rust-lang.org/rustup/archive/%s/$target/rustup-init.sha256"
set -- $(cat "$installer_sum")
sidecar_digest=${1:-}
case "$sidecar_digest" in *[!0-9a-f]*|'') exit 70 ;; esac
[ "${#sidecar_digest}" -eq 64 ] && [ "$sidecar_digest" = "$installer_digest" ] || exit 70
curl --fail --silent --show-error --proto '=https' --tlsv1.2 --connect-timeout 15 --max-time 900 --max-filesize 67108864 --output "$installer" "https://static.rust-lang.org/rustup/archive/%s/$target/rustup-init"
printf '%%s  %%s\n' "$installer_digest" "$installer" | sha256sum -c - >/dev/null
chmod 700 "$installer"
curl --fail --silent --show-error --proto '=https' --tlsv1.2 --connect-timeout 15 --max-time 900 --max-filesize 4096 --output "$manifest_sum" "https://static.rust-lang.org/dist/channel-rust-$version.toml.sha256"
set -- $(cat "$manifest_sum")
manifest_sidecar=${1:-}
case "$manifest_sidecar" in *[!0-9a-f]*|'') exit 70 ;; esac
[ "${#manifest_sidecar}" -eq 64 ] && [ "$manifest_sidecar" = "$manifest_digest" ] || exit 70
curl --fail --silent --show-error --proto '=https' --tlsv1.2 --connect-timeout 15 --max-time 900 --max-filesize 8388608 --output "$manifest" "https://static.rust-lang.org/dist/channel-rust-$version.toml"
printf '%%s  %%s\n' "$manifest_digest" "$manifest" | sha256sum -c - >/dev/null
[ ! -L /runtime/cargo ] && [ ! -L /runtime/rustup ] || exit 70
"$installer" -y --no-modify-path --default-toolchain none --profile minimal --default-host "$target"
rustup=/runtime/cargo/bin/rustup
[ -x "$rustup" ] && [ ! -L "$rustup" ] || exit 70
# The resolution binds the official manifest digest and this recipe verifies
# that versioned manifest before install. rustup independently fetches the same
# exact versioned path over the fixed TLS origin and verifies each component
# against the fetched manifest's checksums.
"$rustup" toolchain install "$version" --profile minimal --no-self-update
"$rustup" default "$version"
rustc_output=$(RUSTUP_TOOLCHAIN="$version" /runtime/cargo/bin/rustc --version)
cargo_output=$(RUSTUP_TOOLCHAIN="$version" /runtime/cargo/bin/cargo --version)
case "$rustc_output" in "rustc $version "*) ;; *) exit 70 ;; esac
case "$cargo_output" in "cargo $version "*) ;; *) exit 70 ;; esac
printf '%%s\n%%s\nmcp-devbox-bootstrap-verified=rust:%%s:cargo:%%s\n' "$rustc_output" "$cargo_output" "$version" "$version"
rm -f -- "$installer" "$installer_sum" "$manifest" "$manifest_sum"
rmdir "$stage"
trap - EXIT HUP INT TERM
	`, version, target, installerDigest, manifestDigest, version, developmentBootstrapRustupVersion, developmentBootstrapRustupVersion)
	return developmentBootstrapLockedScript(inner)
}

func developmentBootstrapLockedScript(inner string) string {
	return `set -eu
umask 077
for required_tool in timeout flock curl sha256sum tar mktemp mv ln readlink chmod rmdir rm cat mkdir; do
  command -v "$required_tool" >/dev/null 2>&1 || exit 69
done
[ -d /runtime ] && [ ! -L /runtime ] || exit 70
lock=/runtime/.mcp-devbox-development-bootstrap.lock
[ ! -L "$lock" ] || exit 70
if [ ! -e "$lock" ]; then (umask 077; : >"$lock"); fi
[ -f "$lock" ] && [ ! -L "$lock" ] || exit 70
chmod 600 "$lock"
exec timeout --signal=TERM --kill-after=10s 30m flock --exclusive "$lock" /bin/sh -c ` + shellSingleQuote(inner)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
