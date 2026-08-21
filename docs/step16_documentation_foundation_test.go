package docs_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestStep16ReadmeIsProductEntryPoint(t *testing.T) {
	readme := readDoc(t, "../README.md")

	for _, heading := range []string{
		"## What Aeontra is",
		"## The problem it solves",
		"## Maintainer-operated demo",
		"## How it works",
		"## Authority model",
		"## Main capabilities",
		"## What it cannot do",
		"## Supported architectures",
		"## Local quick start",
		"## Deployment summary",
		"## Configuration",
		"## Security",
		"## Verification",
		"## Documentation map",
		"## License and vulnerability reporting",
	} {
		if !strings.Contains(readme, heading) {
			t.Errorf("README missing required heading %q", heading)
		}
	}

	for _, required := range []string{
		"Aeontra exposes scoped, auditable software-development operations",
		"Administrators define repository roots, command policy",
		"profiles at startup",
		"documented controls limit authority",
		"guarantee correct model output",
		"docs/configuration.md",
		"docs/security.md",
		"SECURITY.md",
		"docs/tools.md",
		"docs/baselines/",
		"/version",
		"system_runtime_info",
		"local stdio",
		"HTTP control plane",
		"VPS + Edge",
		"preview",
		"single-use plan",
		"revalidation",
		"audit",
		"Apache License, Version 2.0",
		"docs/provenance.md",
	} {
		if !strings.Contains(strings.ToLower(readme), strings.ToLower(required)) {
			t.Errorf("README missing product marker %q", required)
		}
	}

	for _, stale := range []string{"**P8.1", "**P9 Brain", "**P11.2", "**P12", "**P13", "**P14", "**P15", "## Status", "## How to build"} {
		if strings.Contains(readme, stale) {
			t.Errorf("README still behaves like a phase diary: %q", stale)
		}
	}
	for _, slogan := range []string{"secure by default", "secure-by-default, not secure", "useful hands for software work", "the model reasons;"} {
		if strings.Contains(strings.ToLower(readme), slogan) {
			t.Errorf("README contains stock positioning phrase %q", slogan)
		}
	}
}

func TestBrandCompatibilityBoundaryPreservesTechnicalContracts(t *testing.T) {
	readme := readDoc(t, "../README.md")
	boundary := readDoc(t, "brand-compatibility.md")
	docMap := readDoc(t, "documentation-map.md")

	for _, marker := range []string{"# Aeontra", "mcp-devbox", "mcp-edge", "docs/brand-compatibility.md"} {
		if !strings.Contains(readme, marker) {
			t.Errorf("README missing brand compatibility marker %q", marker)
		}
	}
	for _, marker := range []string{
		"Aeontra is the public product name",
		"charle-z/aeontra",
		"github.com/charle-z/mcp-devbox",
		"MCP_DEVBOX_*",
		"/opt/mcp-devbox",
		"compatibility migration",
		"clean installation and in-place upgrade",
	} {
		if !strings.Contains(boundary, marker) {
			t.Errorf("brand compatibility boundary missing %q", marker)
		}
	}
	if !strings.Contains(docMap, "`docs/brand-compatibility.md`") {
		t.Fatal("documentation map does not register brand compatibility owner")
	}
}

func TestStep16ConfigurationDocumentsRuntimeInputs(t *testing.T) {
	configuration := readDoc(t, "configuration.md")
	envSource := readDoc(t, "../internal/app/env.go")

	variablePattern := regexp.MustCompile(`"((?:MCP_DEVBOX|GITHUB|COOLIFY)_[A-Z0-9_]+|CONSOLE_TIMEZONE|SOURCE_COMMIT)"`)
	variables := map[string]struct{}{}
	for _, match := range variablePattern.FindAllStringSubmatch(envSource, -1) {
		variables[match[1]] = struct{}{}
	}
	for _, name := range []string{
		"MCP_DEVBOX_VALIDATION_RUNNER_ADDR",
		"MCP_DEVBOX_VALIDATION_RUNNER_ROOT",
		"MCP_DEVBOX_VALIDATION_RUNNER_HOST_ROOT",
		"MCP_DEVBOX_VALIDATION_RUNNER_IMAGE",
		"MCP_DEVBOX_VALIDATION_RUNNER_STORE",
		"MCP_DEVBOX_VALIDATION_RUNNER_USER",
		"MCP_DEVBOX_VALIDATION_RUNNER_TIMEOUT",
		"GIT_SHA",
		"BUILD_TIME",
		"BUILD_GOMAXPROCS",
		"BUILD_GO_PARALLELISM",
		"BUILD_UV_THREADPOOL_SIZE",
	} {
		variables[name] = struct{}{}
	}

	names := make([]string, 0, len(variables))
	for name := range variables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.Contains(configuration, "`"+name+"`") {
			t.Errorf("configuration.md does not document %s", name)
		}
	}

	for _, required := range []string{
		"Flags override environment variables",
		"environment variables override secure defaults",
		"Local stdio — read-only",
		"Local stdio — ask",
		"Local HTTP",
		"VPS/Coolify with OAuth",
		"Global builder",
		"Persistent Brain",
		"Observability and durable state",
		"Control plane + Edge",
		"Privileged profiles",
		"/repos",
		"/state",
		"/brain",
		"8765",
		"0700",
		"0600",
		"outside the repository jail",
		"config/mcp-devbox.env.sample",
	} {
		if !containsNormalizedProse(configuration, required) {
			t.Errorf("configuration.md missing contract %q", required)
		}
	}
}

func TestStep16SampleConfigurationStartsSafe(t *testing.T) {
	sample := readDoc(t, "../config/mcp-devbox.env.sample")
	for _, required := range []string{
		"MCP_DEVBOX_MODE=read-only",
		"MCP_DEVBOX_PRIVILEGED_TASKS=false",
		"MCP_DEVBOX_ROOT=/repos",
		"MCP_DEVBOX_STATE_ROOT=/state",
		"REPLACE_WITH_",
	} {
		if !strings.Contains(sample, required) {
			t.Errorf("sample configuration missing safe marker %q", required)
		}
	}
	for _, forbidden := range []string{"MCP_DEVBOX_MODE=allow", "MCP_DEVBOX_PRIVILEGED_TASKS=true"} {
		if strings.Contains(sample, forbidden) {
			t.Errorf("sample configuration contains unsafe default %q", forbidden)
		}
	}
}

func TestStep16SecurityAndDocumentationMapHaveCanonicalRoles(t *testing.T) {
	policy := readDoc(t, "../SECURITY.md")
	model := readDoc(t, "security.md")
	docMap := readDoc(t, "documentation-map.md")

	for _, required := range []string{"Reporting a vulnerability", "Supported versions", "Disclosure policy", "docs/security.md", "Apache License, Version 2.0"} {
		if !strings.Contains(policy, required) {
			t.Errorf("SECURITY.md missing public-policy marker %q", required)
		}
	}
	for _, heading := range []string{
		"## Trust boundaries", "## Threat model", "## Authority model",
		"## Direct operations", "## Consequential actions and plans",
		"## Secret handling and grants", "## Authentication and public exposure",
		"## Edge trust and signed releases", "## Isolation profiles",
		"## Persistence and storage", "## Redaction, audit, and observability",
		"## Secure deployment checklist", "## Known limitations",
		"## Security tests and evidence",
	} {
		if !strings.Contains(model, heading) {
			t.Errorf("docs/security.md missing heading %q", heading)
		}
	}
	for _, surface := range []string{"Public control plane", "Edge sandbox", "Trusted Linux workcell", "Authorized target-locked workspace", "Development Edge Git broker"} {
		if !strings.Contains(model, surface) {
			t.Errorf("docs/security.md missing surface %q", surface)
		}
	}
	for _, required := range []string{
		"`README.md`", "`docs/configuration.md`", "`docs/security.md`", "`SECURITY.md`",
		"`docs/tools.md`", "`/version`", "`system_runtime_info`", "`docs/baselines/`", "historical evidence",
	} {
		if !strings.Contains(docMap, required) {
			t.Errorf("documentation-map.md missing canonical source %q", required)
		}
	}
}

func TestOpenSourceFoundationRecordsLicenseAndProvenance(t *testing.T) {
	license := readDoc(t, "../LICENSE")
	notice := readDoc(t, "../NOTICE")
	copyright := readDoc(t, "../COPYRIGHT")
	provenance := readDoc(t, "provenance.md")
	docMap := readDoc(t, "documentation-map.md")

	for _, marker := range []string{"Apache License", "Version 2.0", "TERMS AND CONDITIONS"} {
		if !strings.Contains(license, marker) {
			t.Errorf("LICENSE missing Apache-2.0 marker %q", marker)
		}
	}
	for _, marker := range []string{"Aeontra", "Copyright 2026 Carlos Acosta", "mcp-devbox", "mcp-edge"} {
		if !strings.Contains(notice, marker) {
			t.Errorf("NOTICE missing attribution marker %q", marker)
		}
	}
	if !strings.Contains(copyright, "Licensed under the Apache License, Version 2.0") {
		t.Fatal("COPYRIGHT does not point to Apache-2.0")
	}
	for _, marker := range []string{"mcp-devbox@localhost", "edge@mcp-devbox.local", "t@t", "Developer Certificate of Origin 1.1"} {
		if !strings.Contains(provenance, marker) {
			t.Errorf("provenance boundary missing %q", marker)
		}
	}
	for _, marker := range []string{"`LICENSE`, `NOTICE`, and `COPYRIGHT`", "`docs/provenance.md`"} {
		if !strings.Contains(docMap, marker) {
			t.Errorf("documentation map missing legal source %q", marker)
		}
	}
}
