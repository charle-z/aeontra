package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/policy"
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
func (s *RepositoryCapability) BuildContextPack() (string, error) {
	return s.BuildContextPackIn("")
}

// BuildContextPackIn is BuildContextPack scoped to an optional repo/workdir inside
// the jail. This lets a /repos root produce context for one selected child repo.
func (s *RepositoryCapability) BuildContextPackIn(repo string) (string, error) {
	sp := s.log.Start("build_context_pack")
	dir, err := s.workdir(repo)
	if err != nil {
		sp.Finish(audit.Deny, "build_context_pack", nil, err)
		return "", err
	}
	var b strings.Builder

	fmt.Fprintf(&b, "# Context pack for %s\n\n", filepath.Base(dir))

	b.WriteString("## File tree\n")
	b.WriteString(s.fileTreeIn(dir))
	b.WriteString("\n")

	b.WriteString("## Key files\n")
	for _, kf := range keyFiles {
		p := filepath.Join(dir, kf)
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
	mem, _ := s.MemoryReadIn(repo)
	b.WriteString(mem)
	b.WriteString("\n\n")

	b.WriteString("## Git status\n")
	if st, err := s.GitCapability.RepoStatus(repo); err == nil {
		b.WriteString(st)
	} else {
		b.WriteString("[git status unavailable]")
	}
	b.WriteString("\n")

	sp.Finish(audit.Allow, "build_context_pack", nil, nil)
	// Single final redaction pass over the whole assembled pack.
	return s.redact(b.String()), nil
}

// fileTreeIn returns a bounded, sorted listing of repo files relative to the root,
// skipping ignored and secret-named directories/files.
func (s *RepositoryCapability) fileTreeIn(root string) string {
	var entries []string
	count := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (ignoredDirs[name] || policy.IsSecretPath(path)) {
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
		rel, _ := filepath.Rel(root, path)
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
