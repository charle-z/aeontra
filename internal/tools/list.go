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

const maxListEntries = 300

// ListDir lists one jailed directory without reading file contents. It skips secret
// names and noisy internals, and marks directories that look like Git repositories.
func (s *Service) ListDir(path string) (string, error) {
	sp := s.log.Start("list_dir")
	dir, err := s.workdir(path)
	if err != nil {
		sp.Finish(audit.Deny, "list_dir", nil, err)
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		sp.Finish(audit.Error, "list_dir", []string{dir}, err)
		return "", err
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		child := filepath.Join(dir, name)
		if policy.IsSecretPath(child) || ignoredDirs[name] {
			continue
		}
		if _, err := s.pol.CheckRead(child); err != nil {
			continue
		}
		label := name
		if entry.IsDir() {
			label += "/"
			if looksLikeGitRepo(child) {
				label += " [git]"
			}
		}
		lines = append(lines, label)
		if len(lines) >= maxListEntries {
			break
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		sp.Finish(audit.Allow, "list_dir", []string{dir}, nil)
		return "[empty]", nil
	}
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if len(lines) >= maxListEntries {
		fmt.Fprintf(&b, "[truncated at %d entries]\n", maxListEntries)
	}
	sp.Finish(audit.Allow, "list_dir", []string{dir}, nil)
	return s.redact(strings.TrimRight(b.String(), "\n")), nil
}

func looksLikeGitRepo(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && st.IsDir()
}
