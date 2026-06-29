package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/carbe/mcp-devbox/internal/audit"
	"github.com/carbe/mcp-devbox/internal/policy"
)

const maxTreeEntries = 400

// keyFiles are project-orienting files surfaced in a context pack when present.
var keyFiles = []string{
	"README.md", "AGENTS.md", "CLAUDE.md",
	"go.mod", "package.json", "pyproject.toml", "Cargo.toml",
	"Makefile", "docker-compose.yml",
}

// BuildContextPack returns relevant repo context in ONE call (the agent-first tool):
// a file tree, key project files, the agent memory, and git status — all jailed and
// redacted. It minimizes MCP roundtrips and tokens versus many small reads.
func (s *Service) BuildContextPack() (string, error) {
	sp := s.log.Start("build_context_pack")
	var b strings.Builder

	fmt.Fprintf(&b, "# Context pack for %s\n\n", filepath.Base(s.root))

	b.WriteString("## File tree\n")
	b.WriteString(s.fileTree())
	b.WriteString("\n")

	b.WriteString("## Key files\n")
	for _, kf := range keyFiles {
		p := filepath.Join(s.root, kf)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		resolved, err := s.pol.CheckRead(p)
		if err != nil {
			continue
		}
		content, err := readContained(resolved)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "\n### %s\n%s\n", kf, content)
	}
	b.WriteString("\n")

	b.WriteString("## Agent memory\n")
	mem, _ := s.MemoryRead()
	b.WriteString(mem)
	b.WriteString("\n\n")

	b.WriteString("## Git status\n")
	if st, err := s.GitStatus(); err == nil {
		b.WriteString(st)
	} else {
		b.WriteString("[git status unavailable]")
	}
	b.WriteString("\n")

	sp.Finish(audit.Allow, "build_context_pack", nil, nil)
	// Single final redaction pass over the whole assembled pack.
	return s.redact(b.String()), nil
}

// fileTree returns a bounded, sorted listing of repo files relative to the root,
// skipping ignored and secret-named directories/files.
func (s *Service) fileTree() string {
	var entries []string
	count := 0
	_ = filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != s.root && (ignoredDirs[name] || policy.IsSecretPath(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if policy.IsSecretPath(path) {
			return nil
		}
		if count >= maxTreeEntries {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(s.root, path)
		entries = append(entries, filepath.ToSlash(rel))
		count++
		return nil
	})
	sort.Strings(entries)
	out := strings.Join(entries, "\n")
	if count >= maxTreeEntries {
		out += fmt.Sprintf("\n[truncated at %d files]", maxTreeEntries)
	}
	return out
}
