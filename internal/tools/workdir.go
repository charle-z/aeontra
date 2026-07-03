package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// workdir resolves an optional tool working directory through the Policy jail.
// Empty means the service root. Non-empty may be absolute or relative to the root.
func (s *Service) workdir(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return s.root, nil
	}
	resolved, err := s.pol.CheckRead(input)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("working directory must be a directory: %s", filepath.Clean(input))
	}
	return resolved, nil
}
