package edgeclient

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ToolchainReadinessStatus is deliberately a small, closed result vocabulary.
// The detector is advisory metadata only; it never installs or upgrades a tool.
type ToolchainReadinessStatus string

const (
	ToolchainSupported    ToolchainReadinessStatus = "supported"
	ToolchainEdgeRequired ToolchainReadinessStatus = "edge-required"
	ToolchainPinConflict  ToolchainReadinessStatus = "pin-conflict"

	toolchainManifestMaxBytes = 128 << 10
	toolchainFindingLimit     = 32
)

// ToolchainReadinessFinding contains only fixed manifest names and bounded
// values. It intentionally does not return manifest contents or host paths.
type ToolchainReadinessFinding struct {
	Manifest string                   `json:"manifest"`
	Tool     string                   `json:"tool"`
	Status   ToolchainReadinessStatus `json:"status"`
	Pin      string                   `json:"pin,omitempty"`
	Reason   string                   `json:"reason"`
}

// ToolchainReadiness is a read-only local preflight result. A supported result
// means the detected runtime belongs to the fixed L3 baseline; it does not
// promise that dependency sources are already cached in that networkless image.
type ToolchainReadiness struct {
	Status    ToolchainReadinessStatus    `json:"status"`
	Manifests []string                    `json:"manifests"`
	Findings  []ToolchainReadinessFinding `json:"findings"`
}

type toolchainPin struct {
	Manifest string
	Tool     string
	Value    string
}

var (
	toolchainGoVersionPattern        = regexp.MustCompile(`(?m)^\s*go\s+([0-9]+(?:\.[0-9]+){1,2})\s*(?:#.*)?$`)
	toolchainRustVersionPattern      = regexp.MustCompile(`(?m)^\s*rust-version\s*=\s*["']([^"']+)["']`)
	toolchainPythonRequirement       = regexp.MustCompile(`(?m)^\s*requires-python\s*=\s*["']([^"']+)["']`)
	toolchainPoetryPythonRequirement = regexp.MustCompile(`(?m)^\s*python\s*=\s*["']([^"']+)["']`)
	toolchainRustChannelPattern      = regexp.MustCompile(`(?m)^\s*channel\s*=\s*["']([^"']+)["']`)
	toolchainMiseToolPattern         = regexp.MustCompile(`^\s*["']?([A-Za-z0-9_.+:-]+)["']?\s*=\s*["']([^"']+)["']`)
	toolchainNumericVersionPattern   = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+){0,3}$`)
	toolchainPythonVersionPattern    = regexp.MustCompile(`([0-9]+)(?:\.([0-9]+))?`)
)

var toolchainManifestNames = []string{
	"rust-toolchain.toml",
	".tool-versions",
	"mise.toml",
	"package.json",
	"go.mod",
	"pyproject.toml",
	"Cargo.toml",
	"pom.xml",
	"build.gradle",
	"build.gradle.kts",
	"settings.gradle",
	"settings.gradle.kts",
	"CMakeLists.txt",
	"Makefile",
}

// DetectToolchainReadiness reads a bounded set of well-known project markers.
// It performs no process execution, network access, writes, package-manager
// calls, or manager bootstrap. Workspace paths must already be local and safe.
func DetectToolchainReadiness(workspace string) (ToolchainReadiness, error) {
	if err := validateToolchainWorkspace(workspace); err != nil {
		return ToolchainReadiness{}, err
	}

	result := ToolchainReadiness{
		Status:    ToolchainSupported,
		Manifests: make([]string, 0, len(toolchainManifestNames)),
		Findings:  make([]ToolchainReadinessFinding, 0, len(toolchainManifestNames)),
	}
	pins := make([]toolchainPin, 0, 12)

	contents := make(map[string][]byte, len(toolchainManifestNames))
	for _, name := range toolchainManifestNames {
		content, present, err := readToolchainManifest(workspace, name)
		if err != nil {
			return ToolchainReadiness{}, err
		}
		if !present {
			continue
		}
		result.Manifests = append(result.Manifests, name)
		contents[name] = content
	}

	addFinding := func(manifest, tool string, status ToolchainReadinessStatus, pin, reason string) {
		if len(result.Findings) >= toolchainFindingLimit {
			return
		}
		result.Findings = append(result.Findings, ToolchainReadinessFinding{
			Manifest: manifest,
			Tool:     tool,
			Status:   status,
			Pin:      boundedToolchainToken(pin),
			Reason:   reason,
		})
		result.raise(status)
	}
	addPin := func(manifest, tool, value string) {
		if canonical := canonicalToolchainVersion(value); canonical != "" {
			pins = append(pins, toolchainPin{Manifest: manifest, Tool: tool, Value: canonical})
		}
	}

	if content, ok := contents["rust-toolchain.toml"]; ok {
		channel := quotedAssignment(content, toolchainRustChannelPattern)
		if channel == "" {
			addFinding("rust-toolchain.toml", "rust", ToolchainEdgeRequired, "", "Rust channel is not a fixed L3 baseline pin")
		} else {
			addPin("rust-toolchain.toml", "rust", channel)
			if baselineToolchainVersion("rust", channel) {
				addFinding("rust-toolchain.toml", "rust", ToolchainSupported, channel, "Rust pin matches the fixed L3 baseline")
			} else {
				addFinding("rust-toolchain.toml", "rust", ToolchainEdgeRequired, channel, "alternate or floating Rust channel requires the persistent Edge toolbox")
			}
		}
	}

	if content, ok := contents[".tool-versions"]; ok {
		for _, pin := range parseToolVersions(content) {
			addPin(".tool-versions", pin.Tool, pin.Value)
			status, reason := statusForToolchainPin(pin.Tool, pin.Value)
			addFinding(".tool-versions", pin.Tool, status, pin.Value, reason)
		}
	}

	if content, ok := contents["mise.toml"]; ok {
		tools := parseMiseTools(content)
		// mise itself is not part of the L3 image. Pins that happen to match
		// the baseline remain Edge-required because the manager is absent.
		if len(tools) == 0 {
			addFinding("mise.toml", "mise", ToolchainEdgeRequired, "", "mise is not installed in L3; use the persistent Edge toolbox")
		} else {
			for _, pin := range tools {
				addPin("mise.toml", pin.Tool, pin.Value)
				addFinding("mise.toml", pin.Tool, ToolchainEdgeRequired, pin.Value, "mise-managed toolchains require the persistent Edge toolbox")
			}
		}
	}

	if content, ok := contents["package.json"]; ok {
		if err := inspectPackageManifest(content, workspace, addPin, addFinding); err != nil {
			return ToolchainReadiness{}, err
		}
	}

	if content, ok := contents["go.mod"]; ok {
		version := firstCapture(toolchainGoVersionPattern, content)
		if version == "" || baselineToolchainVersion("go", version) {
			addFinding("go.mod", "go", ToolchainSupported, version, "Go module targets the fixed L3 Go baseline")
		} else {
			addFinding("go.mod", "go", ToolchainEdgeRequired, version, "Go version is outside the fixed L3 baseline")
		}
	}

	if content, ok := contents["pyproject.toml"]; ok {
		constraint := quotedAssignment(content, toolchainPythonRequirement)
		if constraint == "" {
			constraint = quotedAssignment(content, toolchainPoetryPythonRequirement)
		}
		if pythonConstraintRequiresEdge(constraint) {
			addFinding("pyproject.toml", "python", ToolchainEdgeRequired, constraint, "Python requirement does not include the fixed L3 Python baseline")
		} else {
			addFinding("pyproject.toml", "python", ToolchainSupported, constraint, "Python runtime is in the fixed L3 baseline")
		}
	}

	if content, ok := contents["Cargo.toml"]; ok {
		version := quotedAssignment(content, toolchainRustVersionPattern)
		if version != "" && !baselineToolchainVersion("rust", version) {
			addFinding("Cargo.toml", "rust", ToolchainEdgeRequired, version, "Cargo requires a Rust version outside the fixed L3 baseline")
		} else {
			addFinding("Cargo.toml", "rust", ToolchainSupported, version, "Cargo runtime is in the fixed L3 baseline; dependencies must be pre-staged in L3")
		}
	}

	for _, name := range []string{"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"} {
		if _, ok := contents[name]; ok {
			addFinding(name, "java", ToolchainEdgeRequired, "", "Java/JDK is Edge toolbox-only; it is absent from the L3 image")
		}
	}
	if _, ok := contents["CMakeLists.txt"]; ok {
		addFinding("CMakeLists.txt", "cmake", ToolchainEdgeRequired, "", "CMake is Edge toolbox-only; the L3 image provides the C/C++ compiler baseline but not CMake")
	}
	if _, ok := contents["Makefile"]; ok {
		addFinding("Makefile", "make", ToolchainSupported, "", "make is part of the fixed L3 C/C++ build baseline")
	}

	addPinConflictFindings(&result, pins)
	return result, nil
}

func (result *ToolchainReadiness) raise(status ToolchainReadinessStatus) {
	if status == ToolchainPinConflict || (status == ToolchainEdgeRequired && result.Status == ToolchainSupported) {
		result.Status = status
	}
}

func addPinConflictFindings(result *ToolchainReadiness, pins []toolchainPin) {
	byTool := make(map[string]map[string][]string)
	for _, pin := range pins {
		values := byTool[pin.Tool]
		if values == nil {
			values = make(map[string][]string)
			byTool[pin.Tool] = values
		}
		values[pin.Value] = append(values[pin.Value], pin.Manifest)
	}
	for _, tool := range []string{"rust", "go", "python", "node", "npm", "pnpm", "java", "cmake", "c", "cpp"} {
		values := byTool[tool]
		if len(values) < 2 {
			continue
		}
		ordered := make([]string, 0, len(values))
		for value := range values {
			ordered = append(ordered, value)
		}
		sort.Strings(ordered)
		// The list is intentionally bounded without exposing arbitrary manifest
		// contents.
		if len(ordered) > 4 {
			ordered = ordered[:4]
		}
		result.raise(ToolchainPinConflict)
		if len(result.Findings) >= toolchainFindingLimit {
			return
		}
		result.Findings = append(result.Findings, ToolchainReadinessFinding{
			Manifest: "multiple manifests",
			Tool:     tool,
			Status:   ToolchainPinConflict,
			Pin:      strings.Join(ordered, ","),
			Reason:   "conflicting exact toolchain pins were detected",
		})
	}
}

func validateToolchainWorkspace(workspace string) error {
	workspace = filepath.Clean(strings.TrimSpace(workspace))
	if !filepath.IsAbs(workspace) || workspace == string(os.PathSeparator) || isWindowsMount(workspace) {
		return errors.New("toolchain workspace is unsafe")
	}
	if err := rejectSymlinkPath(workspace); err != nil {
		return err
	}
	info, err := os.Lstat(workspace)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("toolchain workspace is unavailable")
	}
	return nil
}

func readToolchainManifest(workspace, name string) ([]byte, bool, error) {
	path := filepath.Join(workspace, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 0 || info.Size() > toolchainManifestMaxBytes {
		return nil, false, errors.New("toolchain manifest is unsafe")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, false, errors.New("toolchain manifest is unavailable")
	}
	return content, true, nil
}

func inspectPackageManifest(content []byte, workspace string, addPin func(string, string, string), addFinding func(string, string, ToolchainReadinessStatus, string, string)) error {
	var manifest struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return errors.New("package manifest is invalid")
	}
	pnpmLock := regularMarker(workspace, "pnpm-lock.yaml")
	npmLock := regularMarker(workspace, "package-lock.json")
	manager, version := splitPackageManager(manifest.PackageManager)
	lockManager := ""
	if pnpmLock {
		lockManager = "pnpm"
	}
	if npmLock {
		if lockManager != "" {
			addFinding("package.json", "package-manager", ToolchainPinConflict, "", "both pnpm-lock.yaml and package-lock.json are present")
		} else {
			lockManager = "npm"
		}
	}
	if manager != "" {
		addPin("package.json", manager, version)
	}
	if manager != "" && lockManager != "" && manager != lockManager {
		addFinding("package.json", manager, ToolchainPinConflict, version, "packageManager disagrees with the selected lockfile")
		return nil
	}
	if manager == "" {
		manager = lockManager
	}
	if manager == "" {
		addFinding("package.json", "package-manager", ToolchainEdgeRequired, "", "a package lockfile is required for bounded validation")
		return nil
	}
	if manager == "pnpm" {
		addFinding("package.json", "pnpm", ToolchainEdgeRequired, version, "pnpm is not in the fixed L3 image; use the persistent Edge toolbox")
		return nil
	}
	if manager == "npm" {
		addFinding("package.json", "npm", ToolchainSupported, version, "npm is in the fixed L3 Node baseline")
		return nil
	}
	addFinding("package.json", manager, ToolchainEdgeRequired, version, "the selected package manager is not part of the fixed L3 baseline")
	return nil
}

func splitPackageManager(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	manager, version, hasVersion := strings.Cut(value, "@")
	manager = strings.ToLower(strings.TrimSpace(manager))
	if manager == "" {
		return "", ""
	}
	if !hasVersion {
		return manager, ""
	}
	return manager, strings.TrimSpace(version)
}

func parseToolVersions(content []byte) []toolchainPin {
	result := make([]toolchainPin, 0, 8)
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if tool := canonicalToolchainName(fields[0]); tool != "" {
			result = append(result, toolchainPin{Tool: tool, Value: fields[1]})
		}
	}
	return result
}

func parseMiseTools(content []byte) []toolchainPin {
	text := string(content)
	section := strings.Index(text, "[tools]")
	if section < 0 {
		return nil
	}
	sectionText := text[section+len("[tools]"):]
	if next := strings.Index(sectionText, "\n["); next >= 0 {
		sectionText = sectionText[:next]
	}
	result := make([]toolchainPin, 0, 8)
	for _, line := range strings.Split(sectionText, "\n") {
		match := toolchainMiseToolPattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		if tool := canonicalToolchainName(match[1]); tool != "" {
			result = append(result, toolchainPin{Tool: tool, Value: match[2]})
		}
	}
	return result
}

func canonicalToolchainName(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "go", "golang":
		return "go"
	case "rust", "rustc":
		return "rust"
	case "python", "python3":
		return "python"
	case "node", "nodejs":
		return "node"
	case "npm":
		return "npm"
	case "pnpm":
		return "pnpm"
	case "java", "jdk":
		return "java"
	case "cmake":
		return "cmake"
	case "gcc", "cc":
		return "c"
	case "g++", "c++":
		return "cpp"
	case "make":
		return "make"
	default:
		return ""
	}
}

func statusForToolchainPin(tool, value string) (ToolchainReadinessStatus, string) {
	if baselineToolchainVersion(tool, value) {
		return ToolchainSupported, "tool pin matches the fixed L3 baseline"
	}
	return ToolchainEdgeRequired, "alternate, floating, or Edge-only tool pin requires the persistent Edge toolbox"
}

func baselineToolchainVersion(tool, value string) bool {
	canonical := canonicalToolchainVersion(value)
	if canonical == "" {
		return false
	}
	parts := strings.Split(canonical, ".")
	switch tool {
	case "go":
		return versionPrefix(parts, []int{1, 26})
	case "rust":
		return versionPrefix(parts, []int{1, 96})
	case "python":
		return versionPrefix(parts, []int{3, 14})
	case "node":
		return versionPrefix(parts, []int{24})
	case "npm":
		return versionPrefix(parts, []int{12})
	default:
		return false
	}
}

func versionPrefix(parts []string, want []int) bool {
	if len(parts) < len(want) {
		return false
	}
	for index, value := range want {
		parsed, err := strconv.Atoi(parts[index])
		if err != nil || parsed != value {
			return false
		}
	}
	return true
}

func canonicalToolchainVersion(value string) string {
	value = strings.TrimSpace(strings.Trim(value, "\"'"))
	if !toolchainNumericVersionPattern.MatchString(value) {
		return ""
	}
	value = strings.TrimPrefix(strings.ToLower(value), "v")
	parts := strings.Split(value, ".")
	for len(parts) > 1 && parts[len(parts)-1] == "0" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, ".")
}

func quotedAssignment(content []byte, pattern *regexp.Regexp) string {
	return firstCapture(pattern, content)
}

func firstCapture(pattern *regexp.Regexp, content []byte) string {
	match := pattern.FindSubmatch(content)
	if len(match) != 2 {
		return ""
	}
	return boundedToolchainToken(string(match[1]))
}

func pythonConstraintRequiresEdge(constraint string) bool {
	constraint = strings.TrimSpace(strings.ToLower(constraint))
	if constraint == "" {
		return false
	}
	// This intentionally handles only unambiguous requirements. Broad ranges
	// such as ">=3.12,<3.15" include Python 3.14 and remain L3-compatible.
	for _, clause := range strings.FieldsFunc(constraint, func(r rune) bool { return r == ',' || r == '|' || r == ';' }) {
		clause = strings.TrimSpace(clause)
		match := toolchainPythonVersionPattern.FindStringSubmatch(clause)
		if len(match) < 2 {
			continue
		}
		major, majorErr := strconv.Atoi(match[1])
		minor := 0
		var minorErr error
		if match[2] != "" {
			minor, minorErr = strconv.Atoi(match[2])
		}
		if majorErr != nil || minorErr != nil || major != 3 {
			return true
		}
		switch {
		case strings.HasPrefix(clause, ">="):
			if minor > 14 {
				return true
			}
		case strings.HasPrefix(clause, ">"):
			if minor >= 14 {
				return true
			}
		case strings.HasPrefix(clause, "<") && !strings.HasPrefix(clause, "<="):
			if minor <= 14 {
				return true
			}
		case strings.HasPrefix(clause, "<="):
			if minor < 14 {
				return true
			}
		case strings.HasPrefix(clause, "!="):
			if minor == 14 {
				return true
			}
		case strings.HasPrefix(clause, "=="), strings.HasPrefix(clause, "="):
			if minor != 14 {
				return true
			}
		case strings.HasPrefix(clause, "^"), strings.HasPrefix(clause, "~"):
			if minor > 14 {
				return true
			}
		default:
			if minor != 14 {
				return true
			}
		}
	}
	return false
}

func boundedToolchainToken(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 64 {
		return value[:64]
	}
	return value
}
