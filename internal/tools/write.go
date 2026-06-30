package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CreateFile creates a NEW file (it refuses to overwrite an existing one). It does
// NOT do a blind full-file write: it builds a unified diff and runs it through the
// patch-first pipeline (ApplyPatch), so the jail, secret-deny, `git apply --check`
// validation, and ask-mode approval all apply exactly as for any other write. This
// keeps the "patch-first, no full-file writes" invariant intact while giving the
// agent an ergonomic create tool. Use apply_patch to modify existing files.
func (s *Service) CreateFile(path, content string, approve bool) (string, error) {
	// Early gate: jail + secret-deny + write posture. Also gives the resolved path
	// so we can refuse to clobber an existing file.
	resolved, _, err := s.pol.CheckWrite(path)
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(resolved); statErr == nil {
		return "", fmt.Errorf("file already exists (use apply_patch to modify): %s", path)
	}
	rel, err := filepath.Rel(s.root, resolved)
	if err != nil {
		return "", err
	}
	diff := newFileDiff(filepath.ToSlash(rel), content)
	return s.ApplyPatch(diff, approve)
}

// newFileDiff builds a git "new file" unified diff that creates relPath with the
// given content. It handles the empty-file and missing-trailing-newline cases the
// way git expects, so `git apply --check` accepts it.
func newFileDiff(relPath, content string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", relPath, relPath)
	b.WriteString("new file mode 100644\n")
	b.WriteString("--- /dev/null\n")
	fmt.Fprintf(&b, "+++ b/%s\n", relPath)
	if content == "" {
		return b.String() // empty new file: no hunk needed
	}
	endsWithNewline := strings.HasSuffix(content, "\n")
	body := content
	if endsWithNewline {
		body = strings.TrimSuffix(body, "\n")
	}
	lines := strings.Split(body, "\n")
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
	for i, ln := range lines {
		b.WriteString("+")
		b.WriteString(ln)
		b.WriteString("\n")
		if !endsWithNewline && i == len(lines)-1 {
			b.WriteString("\\ No newline at end of file\n")
		}
	}
	return b.String()
}
