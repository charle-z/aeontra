package tools

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

const (
	workspaceCheckpointSchemaVersion = 1
	maxWorkspaceCheckpointBytes      = 4096
	maxCurrentTaskSummaryRunes       = 240
)

type workspaceCheckpoint struct {
	SchemaVersion      int    `json:"schema_version"`
	Repository         string `json:"repository"`
	Branch             string `json:"branch"`
	HeadCommit         string `json:"head_commit"`
	Upstream           string `json:"upstream"`
	UpstreamCommit     string `json:"upstream_commit"`
	OriginMainCommit   string `json:"origin_main_commit"`
	Ahead              int    `json:"ahead"`
	Behind             int    `json:"behind"`
	TreeClean          bool   `json:"tree_clean"`
	StagedFileCount    int    `json:"staged_file_count"`
	ModifiedFileCount  int    `json:"modified_file_count"`
	UntrackedFileCount int    `json:"untracked_file_count"`
	TemporaryFileCount int    `json:"temporary_file_count"`
	DiffFiles          int    `json:"diff_files"`
	Insertions         int    `json:"insertions"`
	Deletions          int    `json:"deletions"`
	CurrentTaskSummary string `json:"current_task_summary"`
}

// WorkspaceCheckpointIn returns a compact reconstruction of local repository state.
// It performs only fixed, read-only Git operations and never fetches or reads source
// file bodies. The one allowed memory field is a bounded, redacted task summary.
func (s *RepositoryCapability) WorkspaceCheckpointIn(repo string) (string, error) {
	sp := s.log.Start("workspace_checkpoint")
	status, err := s.GitCapability.readRepositoryStatus(repo)
	if err != nil {
		sp.Finish(audit.Error, "workspace_checkpoint", nil, err)
		return "", err
	}

	checkpoint := workspaceCheckpoint{
		SchemaVersion:      workspaceCheckpointSchemaVersion,
		Repository:         s.redact(status.Repository),
		Branch:             s.redact(status.Branch),
		HeadCommit:         status.Head,
		Upstream:           s.redact(status.Upstream),
		Ahead:              status.Ahead,
		Behind:             status.Behind,
		TreeClean:          status.Clean,
		StagedFileCount:    len(status.Staged),
		ModifiedFileCount:  len(status.Modified),
		UntrackedFileCount: len(status.Untracked),
		TemporaryFileCount: countTemporaryStatusFiles(status),
	}
	if status.Upstream != "" {
		checkpoint.UpstreamCommit = s.optionalCommit(status.Dir, status.Upstream)
	}
	checkpoint.OriginMainCommit = s.optionalCommit(status.Dir, "origin/main")
	checkpoint.DiffFiles, checkpoint.Insertions, checkpoint.Deletions, err = s.diffStatistics(status.Dir)
	if err != nil {
		sp.Finish(audit.Error, "workspace_checkpoint", []string{status.Dir}, err)
		return "", err
	}
	checkpoint.CurrentTaskSummary = s.currentTaskSummary(status.Dir)

	body, err := json.Marshal(checkpoint)
	if err != nil {
		sp.Finish(audit.Error, "workspace_checkpoint", []string{status.Dir}, err)
		return "", fmt.Errorf("encode workspace checkpoint: %w", err)
	}
	if len(body) > maxWorkspaceCheckpointBytes {
		err := fmt.Errorf("workspace checkpoint exceeds %d bytes", maxWorkspaceCheckpointBytes)
		sp.Finish(audit.Error, "workspace_checkpoint", []string{status.Dir}, err)
		return "", err
	}
	sp.Finish(audit.Allow, "workspace_checkpoint", []string{status.Dir}, nil)
	return string(body), nil
}

func (s *RepositoryCapability) optionalCommit(dir, ref string) string {
	out, err := s.GitCapability.gitRead(dir, "rev-parse", "--verify", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (s *RepositoryCapability) diffStatistics(dir string) (files, insertions, deletions int, err error) {
	out, err := s.GitCapability.gitRead(dir, "diff", "--numstat", "HEAD", "--")
	if err != nil {
		return 0, 0, 0, fmt.Errorf("git diff statistics: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		files++
		if fields[0] != "-" {
			value, _ := strconv.Atoi(fields[0])
			insertions += value
		}
		if fields[1] != "-" {
			value, _ := strconv.Atoi(fields[1])
			deletions += value
		}
	}
	return files, insertions, deletions, nil
}

func (s *RepositoryCapability) currentTaskSummary(dir string) string {
	path := filepath.Join(dir, ".agent-memory", "current-task.md")
	resolved, err := s.pol.CheckRead(path)
	if err != nil {
		return ""
	}
	content, err := readContained(resolved)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.Join(strings.Fields(line), " ")
		runes := []rune(s.redact(line))
		if len(runes) > maxCurrentTaskSummaryRunes {
			runes = runes[:maxCurrentTaskSummaryRunes]
		}
		return string(runes)
	}
	return ""
}

func countTemporaryStatusFiles(status repositoryStatus) int {
	seen := make(map[string]struct{})
	for _, files := range [][]string{status.Staged, status.Modified, status.Untracked} {
		for _, name := range files {
			lower := strings.ToLower(filepath.Base(filepath.ToSlash(name)))
			if strings.HasSuffix(lower, ".tmp") || strings.HasSuffix(lower, ".temp") ||
				strings.HasSuffix(lower, ".swp") || strings.HasSuffix(lower, ".orig") ||
				strings.HasSuffix(lower, ".rej") || strings.HasSuffix(lower, "~") {
				seen[name] = struct{}{}
			}
		}
	}
	return len(seen)
}
