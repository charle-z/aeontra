package tools

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/carbe/mcp-devbox/internal/audit"
	"github.com/carbe/mcp-devbox/internal/policy"
)

// ignoredDirs are skipped during search/scan: noise (.git internals, deps) — never
// useful and large. Secret paths are skipped separately via policy.IsSecretPath.
var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, ".idea": true, ".vscode": true,
}

const maxSearchMatches = 200

// SearchCode searches the jail for a regular expression. It skips ignored dirs and
// secret-named paths entirely, never reads outside the jail, and redacts every
// returned line so a search cannot be used to surface a secret value.
func (s *Service) SearchCode(query string) (string, error) {
	sp := s.log.Start("search_code")
	re, err := regexp.Compile(query)
	if err != nil {
		sp.Finish(audit.Error, summarize(query), nil, err)
		return "", fmt.Errorf("invalid search pattern: %w", err)
	}

	var b strings.Builder
	matches := 0
	walkErr := filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		name := d.Name()
		if d.IsDir() {
			if ignoredDirs[name] || policy.IsSecretPath(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if policy.IsSecretPath(path) {
			return nil
		}
		// Policy gate per file (defense in depth; also catches symlinked entries).
		if _, err := s.pol.CheckRead(path); err != nil {
			return nil
		}
		if matches >= maxSearchMatches {
			return filepath.SkipAll
		}
		searchFile(&b, s, re, path, &matches)
		return nil
	})
	if walkErr != nil {
		sp.Finish(audit.Error, summarize(query), nil, walkErr)
		return "", walkErr
	}
	if matches >= maxSearchMatches {
		fmt.Fprintf(&b, "\n[truncated at %d matches]\n", maxSearchMatches)
	}
	sp.Finish(audit.Allow, summarize(query), nil, nil)
	return s.redact(b.String()), nil
}

// searchFile scans one file and writes "relpath:line: text" for each match,
// incrementing the shared match counter via total.
func searchFile(b *strings.Builder, s *Service, re *regexp.Regexp, path string, total *int) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	rel, _ := filepath.Rel(s.root, path)
	rel = filepath.ToSlash(rel)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if strings.IndexByte(text, 0) >= 0 {
			return // binary; stop scanning this file
		}
		if re.MatchString(text) {
			if *total >= maxSearchMatches {
				return
			}
			fmt.Fprintf(b, "%s:%d: %s\n", rel, line, strings.TrimSpace(text))
			*total++
		}
	}
}
