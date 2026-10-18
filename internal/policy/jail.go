// Package policy is the security core of mcp-devbox. It is built and tested before
// any MCP tool. Every tool consults policy; tools never re-implement these checks.
package policy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutsideJail is returned when a path resolves outside every configured root.
var ErrOutsideJail = errors.New("policy: path is outside the workspace jail")

// Jail confines filesystem and command-execution paths to a set of project roots.
// Roots are resolved (symlinks + absolute + cleaned) once at construction; checks
// resolve inputs against the live filesystem so symlink escapes are caught.
type Jail struct {
	roots []string // absolute, symlink-resolved, cleaned
}

// NewJail builds a Jail from absolute root paths. Each root is symlink-resolved if
// it exists (so the containment comparison is against real paths). A root that does
// not yet exist is kept cleaned-but-unresolved.
func NewJail(roots []string) (*Jail, error) {
	if len(roots) == 0 {
		return nil, errors.New("policy: jail requires at least one root")
	}
	resolved := make([]string, 0, len(roots))
	for _, r := range roots {
		if !filepath.IsAbs(r) {
			return nil, errors.New("policy: jail root must be absolute: " + r)
		}
		rr := r
		if real, err := filepath.EvalSymlinks(r); err == nil {
			rr = real
		}
		resolved = append(resolved, filepath.Clean(rr))
	}
	return &Jail{roots: resolved}, nil
}

// Resolve maps an input path (absolute, or relative to a root) to a real, contained
// absolute path. It returns ErrOutsideJail if the resolved path — after following
// symlinks on its longest existing prefix — is not within any root. Non-existent
// targets (e.g. a file a patch will create) are allowed as long as their location
// is inside a root.
func (j *Jail) Resolve(input string) (string, error) {
	if input == "" {
		return "", ErrOutsideJail
	}
	for _, root := range j.roots {
		var cand string
		if filepath.IsAbs(input) {
			cand = filepath.Clean(input)
		} else {
			cand = filepath.Clean(filepath.Join(root, input))
		}
		real := resolveExisting(cand)
		if withinRoot(real, root) {
			return real, nil
		}
	}
	return "", ErrOutsideJail
}

// Contains reports whether an already-absolute path is inside the jail.
func (j *Jail) Contains(input string) bool {
	_, err := j.Resolve(input)
	return err == nil
}

// Roots returns a copy of the configured roots (for diagnostics; callers cannot
// mutate the jail through it).
func (j *Jail) Roots() []string {
	out := make([]string, len(j.roots))
	copy(out, j.roots)
	return out
}

// resolveExisting follows symlinks on the longest existing prefix of p, then
// re-appends the non-existent remainder. This catches symlink escape even when the
// final path component does not exist yet.
func resolveExisting(p string) string {
	p = filepath.Clean(p)
	rem := ""
	cur := p
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Clean(filepath.Join(resolved, rem))
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p // nothing along the path resolves; use the cleaned input
		}
		rem = filepath.Join(filepath.Base(cur), rem)
		cur = parent
	}
}

// withinRoot reports whether p is root or a descendant of root, using a
// segment-aware relative check (so "/repo-evil" is NOT considered inside "/repo").
func withinRoot(p, root string) bool {
	if p == root {
		return true
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	if filepath.IsAbs(rel) {
		return false
	}
	return true
}
