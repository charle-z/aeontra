//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/development"
)

// DevelopmentGoCommandRequirements reuses bounded no-symlink manifest reads.
// Missing root Go manifests retain conservative legacy inference. Numeric
// manager pins remain exact; only a module/workspace go directive is a minimum.
func DevelopmentGoCommandRequirements(ctx context.Context, workspace, sourceDigest string, conservative []development.CapabilityID, runner DevGitCommandRunner) (*development.GoCommandRequirements, error) {
	evidence := &development.GoCommandRequirements{Version: 1, SourceDigest: sourceDigest}
	manifests := make(map[string][]byte, 4)
	var minimumErr error
	for _, name := range []string{"go.mod", "go.work"} {
		content, present, err := readToolchainManifest(workspace, name)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		manifests[name] = content
		minimum, err := developmentGoMinimum(content)
		if err != nil {
			minimumErr = err
			continue
		}
		if minimum == "" {
			// Legacy Go modules without a go directive have no new evidence.
			return nil, nil
		}
		evidence.MinimumVersions = append(evidence.MinimumVersions, development.GoMinimum{Manifest: name, Version: minimum})
	}
	if len(evidence.MinimumVersions) == 0 && minimumErr == nil {
		return nil, nil
	}
	for _, name := range []string{".tool-versions", "mise.toml"} {
		content, present, err := readToolchainManifest(workspace, name)
		if err != nil {
			return nil, err
		}
		if present {
			manifests[name] = content
		}
	}
	// Ignored inputs are outside the fingerprint and do not travel as public
	// Git source. Require every evidence manifest to belong to exact HEAD.
	if runner == nil {
		return nil, errors.New("development Go source inventory is unavailable")
	}
	output, err := runner.Run(ctx, workspace, []string{"ls-tree", "-r", "--full-tree", "-z", "--name-only", "HEAD"}, GitHubCredential{})
	if err != nil {
		return nil, err
	}
	tracked, err := parseProjectSourceContentPaths([]byte(output), maxRegisteredProjectSourceFiles, maxRegisteredProjectSourcePathBytes)
	if err != nil {
		return nil, err
	}
	for name := range manifests {
		if !slices.Contains(tracked, name) {
			// This optional evidence cannot describe the public HEAD. Keep
			// generic inspection and its conservative requirements unchanged.
			return nil, nil
		}
	}
	if minimumErr != nil {
		return nil, minimumErr
	}
	for _, name := range []string{".tool-versions", "mise.toml"} {
		if content, present := manifests[name]; present {
			if err := validateDevelopmentGoManagerSyntax(name, content); err != nil {
				return nil, err
			}
		}
	}
	// Re-read the supported manager observations inside the source-binding
	// window. A dropped/truncated/changed or unsupported Go pin cannot become
	// permission to discard conservative source requirements.
	readiness, err := DetectToolchainReadiness(workspace)
	if err != nil {
		return nil, err
	}
	if _, err := DevelopmentRequirementsFromToolchainReadiness(readiness); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(readiness.Findings))
	for _, finding := range readiness.Findings {
		if finding.Tool != "go" || finding.Manifest == "go.mod" {
			continue
		}
		if (finding.Manifest != ".tool-versions" && finding.Manifest != "mise.toml") ||
			!development.CanonicalGoVersion(exactDevelopmentToolchainVersion(finding.Pin)) {
			return nil, errors.New("development Go pin is unresolved")
		}
		requirement, err := development.VersionRequirement("toolchain.go", exactDevelopmentToolchainVersion(finding.Pin))
		if err != nil {
			return nil, err
		}
		names = append(names, string(requirement.ID))
	}
	requirements, err := development.Requirements(names...)
	if err != nil {
		return nil, err
	}
	for _, requirement := range requirements {
		evidence.ExactRequirements = append(evidence.ExactRequirements, requirement.ID)
	}
	if !evidence.Valid(sourceDigest, conservative) {
		return nil, errors.New("development Go requirement evidence conflicts")
	}
	return evidence, nil
}

func validateDevelopmentGoManagerSyntax(name string, content []byte) error {
	text := string(content)
	if name == "mise.toml" {
		section := strings.Index(text, "[tools]")
		if section < 0 {
			return nil // The conservative detector rejects unsupported mise forms.
		}
		text = text[section+len("[tools]"):]
		if next := strings.Index(text, "\n["); next >= 0 {
			text = text[:next]
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if name == ".tool-versions" {
			if canonicalToolchainName(fields[0]) == "go" && (len(fields) != 2 || exactDevelopmentToolchainVersion(fields[1]) == "") {
				return errors.New("development Go manager pin is invalid")
			}
			continue
		}
		key, value, assigned := strings.Cut(line, "=")
		if assigned && canonicalToolchainName(strings.Trim(strings.TrimSpace(key), "\"'")) == "go" {
			match := toolchainMiseToolPattern.FindStringSubmatch(line)
			value = strings.TrimSpace(value)
			if len(match) != 3 || exactDevelopmentToolchainVersion(match[2]) == "" ||
				(value != "\""+match[2]+"\"" && value != "'"+match[2]+"'") {
				return errors.New("development Go manager pin is invalid")
			}
		}
	}
	return nil
}

func developmentGoMinimum(content []byte) (string, error) {
	minimum := ""
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "//", 2)[0])
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "go" {
			continue
		}
		if minimum != "" || len(fields) != 2 || !development.CanonicalGoVersion(fields[1]) {
			return "", errors.New("development Go minimum is invalid")
		}
		minimum = fields[1]
	}
	return minimum, nil
}
