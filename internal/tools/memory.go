package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/carbe/mcp-devbox/internal/audit"
	"github.com/carbe/mcp-devbox/internal/config"
)

// memoryDir is the agent-agnostic repo memory directory.
const memoryDir = ".agent-memory"

// MemoryRead returns the repo's agent memory: all Markdown under .agent-memory/,
// concatenated and redacted. It is a read and works in any mode. Missing memory is
// not an error — it returns a short note.
func (s *Service) MemoryRead() (string, error) {
	sp := s.log.Start("memory_read")
	base := filepath.Join(s.root, memoryDir)
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		sp.Finish(audit.Allow, "memory_read", nil, nil)
		return "[no agent memory yet: create " + memoryDir + "/ to persist project state]", nil
	}

	var files []string
	_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)

	var b strings.Builder
	for _, f := range files {
		// Defense in depth: route through the read gate (jail + secret-path).
		resolved, err := s.pol.CheckRead(f)
		if err != nil {
			continue
		}
		content, err := readContained(resolved)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(s.root, f)
		fmt.Fprintf(&b, "===== %s =====\n%s\n\n", filepath.ToSlash(rel), content)
	}
	if b.Len() == 0 {
		b.WriteString("[" + memoryDir + "/ exists but contains no Markdown memory]")
	}
	sp.Finish(audit.Allow, "memory_read", files, nil)
	return s.redact(b.String()), nil
}

// MemoryUpdateHandoff writes a handoff note into .agent-memory/handoffs/: a
// timestamped file plus latest.md. This is a write, so it is denied in read-only
// mode (constitution Article I.1). The note is secret-scanned/redacted before
// persisting so secrets are never written into memory. Writes are confined to the
// memory directory inside the jail.
func (s *Service) MemoryUpdateHandoff(content string) (string, error) {
	sp := s.log.Start("memory_update_handoff")
	if s.pol.Mode() == config.ModeReadOnly {
		err := fmt.Errorf("memory_update_handoff blocked: server is read-only")
		sp.Finish(audit.Deny, "memory_update_handoff", nil, err)
		return "", err
	}

	safe, _ := s.pol.Redact(content)
	dir := filepath.Join(s.root, memoryDir, "handoffs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		sp.Finish(audit.Error, "memory_update_handoff", nil, err)
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	body := fmt.Sprintf("# Handoff %s\n\n%s\n", stamp, strings.TrimSpace(safe))
	tsFile := filepath.Join(dir, stamp+".md")
	latest := filepath.Join(dir, "latest.md")

	written := []string{tsFile, latest}
	for _, f := range written {
		// Confirm the destination is inside the jail before writing.
		if !filepathWithin(s.root, f) {
			err := fmt.Errorf("handoff path escaped the jail: %s", f)
			sp.Finish(audit.Deny, "memory_update_handoff", nil, err)
			return "", err
		}
		if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
			sp.Finish(audit.Error, "memory_update_handoff", written, err)
			return "", err
		}
	}
	sp.Finish(audit.Allow, "memory_update_handoff", written, nil)
	rel, _ := filepath.Rel(s.root, tsFile)
	return "Handoff written to " + filepath.ToSlash(rel) + " (and latest.md)", nil
}

// filepathWithin reports whether target is inside base (both absolute, cleaned).
func filepathWithin(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
