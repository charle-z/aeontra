package edgeclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DevGitBrokerSocketName = "dev-git-broker.sock"
	devGitPlanTTL          = 5 * time.Minute
)

var (
	devGitSimplePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	devGitCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
	devGitPlanPattern   = regexp.MustCompile(`^dp_[a-f0-9]{32}$`)
)

type DevGitBrokerConfig struct {
	SocketPath string
	StateRoot  string
	Workspace  Workspace
	RuntimeID  string
	ExpiresAt  time.Time
	ToolPath   string
	Credential GitHubCredential
	Runner     DevGitCommandRunner
}

type DevGitCommandRunner interface {
	Run(context.Context, string, []string, GitHubCredential) (string, error)
}

type devGitTransportRunner interface {
	VerifyRemoteAncestor(context.Context, string, string, string, string, string, GitHubCredential) error
	PublishCommit(context.Context, string, string, string, string, GitHubCredential) (string, error)
}

type DevGitRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Repository  string `json:"repository,omitempty"`
	Directory   string `json:"directory,omitempty"`
	Branch      string `json:"branch,omitempty"`
	PlanID      string `json:"plan_id,omitempty"`
}

type DevGitResponse struct {
	Status      string    `json:"status"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
	Repository  string    `json:"repository,omitempty"`
	Directory   string    `json:"directory,omitempty"`
	Branch      string    `json:"branch,omitempty"`
	Head        string    `json:"head,omitempty"`
	RemoteHead  string    `json:"remote_head,omitempty"`
	PlanID      string    `json:"plan_id,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Published   bool      `json:"published,omitempty"`
	ErrorCode   string    `json:"error_code,omitempty"`
}

type devGitPublishPlan struct {
	ID, WorkspaceID, Directory, Branch, Head, RemoteHead, RemoteURL string
	ExpiresAt                                                       time.Time
	Used                                                            bool
}

type devGitBroker struct {
	config DevGitBrokerConfig
	mu     sync.Mutex
	plans  map[string]devGitPublishPlan
	now    func() time.Time
	calls  atomic.Uint32
}

func decodeDevGitRequest(reader io.Reader) (DevGitRequest, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, 16<<10))
	decoder.DisallowUnknownFields()
	var request DevGitRequest
	if decoder.Decode(&request) != nil {
		return DevGitRequest{}, errors.New("development Git request is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return DevGitRequest{}, errors.New("development Git request has trailing data")
	}
	return request, nil
}

func validDevGitBranch(branch string) bool {
	return len(branch) >= 2 && len(branch) <= 128 && !strings.HasPrefix(branch, "-") &&
		!strings.ContainsAny(branch, "\\ ~^:?*[") && !strings.Contains(branch, "..") &&
		!strings.Contains(branch, "//") && !strings.Contains(branch, "@{") &&
		!strings.HasSuffix(branch, "/") && !strings.HasSuffix(branch, ".") && !strings.HasSuffix(branch, ".lock")
}

func validateDevGitCloneRequest(request DevGitRequest, workspace Workspace) (DevGitRequest, error) {
	if request.WorkspaceID != workspace.ID || request.PlanID != "" || !devGitSimplePattern.MatchString(request.Repository) ||
		!devGitSimplePattern.MatchString(request.Directory) || !validDevGitBranch(request.Branch) {
		return DevGitRequest{}, errors.New("development Git clone request is invalid")
	}
	return request, nil
}

func validateDevGitPreviewRequest(request DevGitRequest, workspace Workspace) (DevGitRequest, error) {
	if request.WorkspaceID != workspace.ID || request.Repository != "" || request.PlanID != "" ||
		!devGitSimplePattern.MatchString(request.Directory) || !validDevGitBranch(request.Branch) {
		return DevGitRequest{}, errors.New("development Git preview request is invalid")
	}
	return request, nil
}

func validateDevGitPublishRequest(request DevGitRequest, workspace Workspace) (DevGitRequest, error) {
	if request.WorkspaceID != workspace.ID || request.Repository != "" || request.Directory != "" || request.Branch != "" || !devGitPlanPattern.MatchString(request.PlanID) {
		return DevGitRequest{}, errors.New("development Git publish request is invalid")
	}
	return request, nil
}

func (broker *devGitBroker) repositoryPath(directory string) (string, error) {
	if !devGitSimplePattern.MatchString(directory) {
		return "", errors.New("development repository directory is invalid")
	}
	target := filepath.Join(broker.config.Workspace.Path, directory)
	if !pathInside(broker.config.Workspace.Path, target) || filepath.Dir(target) != filepath.Clean(broker.config.Workspace.Path) {
		return "", errors.New("development repository escaped the workspace")
	}
	return target, nil
}

func newDevGitPlanID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "dp_" + hex.EncodeToString(raw), nil
}
