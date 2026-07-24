//go:build !windows

package buildspike

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

type BuildRequest struct {
	WorkspaceRoot string
	ContextPath   string
	Dockerfile    string
	OutputRoot    string
	Commit        string
	NoCache       bool
}

type BuildCommandPlan struct {
	Executable  string
	Args        []string
	Environment []string
}

type ArtifactPlan struct {
	Path   string
	Commit string
}

func BuildCommand(config Config, request BuildRequest) (BuildCommandPlan, ArtifactPlan, error) {
	if err := config.validateForPlan(); err != nil {
		return BuildCommandPlan{}, ArtifactPlan{}, err
	}
	if !filepath.IsAbs(request.WorkspaceRoot) || !filepath.IsAbs(request.ContextPath) || !filepath.IsAbs(request.OutputRoot) || filepath.Clean(request.WorkspaceRoot) != request.WorkspaceRoot || filepath.Clean(request.ContextPath) != request.ContextPath || filepath.Clean(request.OutputRoot) != request.OutputRoot || !commitPattern.MatchString(request.Commit) {
		return BuildCommandPlan{}, ArtifactPlan{}, errors.New("buildspike: build request is invalid")
	}
	contextRelative, err := filepath.Rel(request.WorkspaceRoot, request.ContextPath)
	if err != nil || contextRelative == "." || contextRelative == ".." || filepath.IsAbs(contextRelative) || strings.HasPrefix(contextRelative, ".."+string(filepath.Separator)) {
		return BuildCommandPlan{}, ArtifactPlan{}, errors.New("buildspike: context escapes workspace root")
	}
	if request.Dockerfile != "Dockerfile" || strings.ContainsAny(request.Dockerfile, `/\\`) {
		return BuildCommandPlan{}, ArtifactPlan{}, errors.New("buildspike: Dockerfile name is invalid")
	}
	for _, path := range []string{request.WorkspaceRoot, request.ContextPath, request.OutputRoot} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return BuildCommandPlan{}, ArtifactPlan{}, errors.New("buildspike: build path is unsafe")
		}
	}
	outputRelative, err := filepath.Rel(request.ContextPath, request.OutputRoot)
	if err == nil && outputRelative != ".." && !strings.HasPrefix(outputRelative, ".."+string(filepath.Separator)) {
		return BuildCommandPlan{}, ArtifactPlan{}, errors.New("buildspike: output root overlaps context")
	}
	artifact := ArtifactPlan{Path: filepath.Join(request.OutputRoot, request.Commit+".oci.tar"), Commit: request.Commit}
	args := []string{
		"--addr", "unix://" + filepath.Join(config.RuntimeRoot, "buildkit", "buildkitd.sock"),
		"build",
		"--frontend", "dockerfile.v0",
		"--local", "context=" + request.ContextPath,
		"--local", "dockerfile=" + request.ContextPath,
		"--opt", "filename=" + request.Dockerfile,
		"--export-cache", "type=local,dest=" + filepath.Join(config.CacheRoot, "export") + ",mode=max,reset=true",
		"--import-cache", "type=local,src=" + filepath.Join(config.CacheRoot, "export"),
		"--output", "type=oci,dest=" + artifact.Path,
	}
	if request.NoCache {
		args = append(args, "--no-cache")
	}
	environment := []string{
		"HOME=" + config.StateRoot,
		"XDG_RUNTIME_DIR=" + config.RuntimeRoot,
		"BUILDKIT_HOST=unix://" + filepath.Join(config.RuntimeRoot, "buildkit", "buildkitd.sock"),
	}
	return BuildCommandPlan{Executable: config.BuildctlPath, Args: args, Environment: environment}, artifact, nil
}
