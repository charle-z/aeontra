package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
)

// memoryDir is the agent-agnostic repo memory directory.
const memoryDir = ".agent-memory"

var memoryWriteSections = map[string]string{
	"current-task": "current-task.md",
	"plan":         "plan.md",
	"decisions":    "decisions.md",
	"reflections":  "reflections.md",
}

// MemoryRead returns the repo's agent memory: all Markdown under .agent-memory/,
// concatenated and redacted. It is a read and works in any mode. Missing memory is
// not an error — it returns a short note.
func (s *RepositoryCapability) MemoryRead() (string, error) {
	return s.MemoryReadIn("")
}

// MemoryReadIn reads memory under an optional selected repo/workdir inside the jail.
func (s *RepositoryCapability) MemoryReadIn(repo string) (string, error) {
	sp := s.log.Start("memory_read")
	dir, err := s.workdir(repo)
	if err != nil {
		sp.Finish(audit.Deny, "memory_read", nil, err)
		return "", err
	}
	base := filepath.Join(dir, memoryDir)
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
		rel, _ := filepath.Rel(dir, f)
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
func (s *RepositoryCapability) MemoryUpdateHandoff(content string) (string, error) {
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

// MemoryWrite writes one structured memory section under .agent-memory/. Sections
// are closed-set names, not paths. The destination still goes through the Policy
// write gate so jail, secret-path deny, and mode posture remain centralized.
func (s *RepositoryCapability) MemoryWrite(section, content string, approve bool) (string, error) {
	return s.MemoryWriteIn("", section, content, approve)
}

// MemoryWriteIn writes structured memory under an optional selected repo/workdir
// inside the jail.
func (s *RepositoryCapability) MemoryWriteIn(repo, section, content string, approve bool) (string, error) {
	sp := s.log.Start("memory_write")
	dir, err := s.workdir(repo)
	if err != nil {
		sp.Finish(audit.Deny, "memory_write", nil, err)
		return "", err
	}

	key := strings.ToLower(strings.TrimSpace(section))
	filename, ok := memoryWriteSections[key]
	if !ok {
		err := fmt.Errorf("unknown memory section %q (allowed: current-task, plan, decisions, reflections)", section)
		sp.Finish(audit.Deny, "memory_write", nil, err)
		return "", err
	}
	if strings.TrimSpace(content) == "" {
		err := fmt.Errorf("memory content is required")
		sp.Finish(audit.Error, "memory_write", nil, err)
		return "", err
	}

	dest := filepath.Join(dir, memoryDir, filename)
	resolved, needsApproval, err := s.pol.CheckWrite(dest)
	if err != nil {
		sp.Finish(audit.Deny, key, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, key, []string{resolved}, nil)
		return "APPROVAL REQUIRED: memory_write would update " + filepath.ToSlash(filepath.Join(memoryDir, filename)) + ". Re-invoke with approve=true.", nil
	}

	safe, _ := s.pol.Redact(content)
	body := strings.TrimSpace(safe) + "\n"
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		sp.Finish(audit.Error, key, []string{resolved}, err)
		return "", err
	}
	if err := os.WriteFile(resolved, []byte(body), 0o644); err != nil {
		sp.Finish(audit.Error, key, []string{resolved}, err)
		return "", err
	}
	sp.Finish(audit.Allow, key, []string{resolved}, nil)
	rel, _ := filepath.Rel(dir, resolved)
	return "Memory section written to " + filepath.ToSlash(rel), nil
}

// filepathWithin reports whether target is inside base (both absolute, cleaned).
func filepathWithin(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
