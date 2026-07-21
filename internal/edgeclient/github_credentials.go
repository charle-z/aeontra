package edgeclient

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const githubCredentialFile = "github.json"

var (
	githubOwnerPattern     = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	ErrGitHubNotConfigured = errors.New("local GitHub authority is not configured")
)

type GitHubCredential struct {
	SchemaVersion int    `json:"schema_version"`
	Owner         string `json:"owner"`
	Token         string `json:"token"`
}

type GitHubCredentialStatus struct {
	Configured bool   `json:"configured"`
	Owner      string `json:"owner,omitempty"`
}

func ConfigureGitHubCredential(stateRoot, owner string, input io.Reader) (GitHubCredentialStatus, error) {
	stateRoot = filepath.Clean(strings.TrimSpace(stateRoot))
	owner = strings.TrimSpace(owner)
	if !filepath.IsAbs(stateRoot) || !githubOwnerPattern.MatchString(owner) || input == nil {
		return GitHubCredentialStatus{}, errors.New("GitHub credential configuration is invalid")
	}
	if err := preparePrivateRoot(stateRoot); err != nil {
		return GitHubCredentialStatus{}, errors.New("private Edge state root is unsafe")
	}
	reader := bufio.NewReader(io.LimitReader(input, 1025))
	tokenBytes, err := io.ReadAll(reader)
	if err != nil {
		return GitHubCredentialStatus{}, errors.New("GitHub token is unavailable")
	}
	token := strings.TrimSpace(string(tokenBytes))
	for index := range tokenBytes {
		tokenBytes[index] = 0
	}
	if !validGitHubToken(token) {
		return GitHubCredentialStatus{}, errors.New("valid GitHub token required on stdin")
	}
	credential := GitHubCredential{SchemaVersion: 1, Owner: owner, Token: token}
	body, err := json.Marshal(credential)
	if err != nil {
		return GitHubCredentialStatus{}, errors.New("GitHub credential encoding failed")
	}
	path := filepath.Join(stateRoot, githubCredentialFile)
	temporary, err := os.CreateTemp(stateRoot, ".github-*.tmp")
	if err != nil {
		return GitHubCredentialStatus{}, errors.New("GitHub credential staging failed")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return GitHubCredentialStatus{}, errors.New("GitHub credential permissions failed")
	}
	if _, err := temporary.Write(body); err != nil || temporary.Sync() != nil || temporary.Close() != nil {
		return GitHubCredentialStatus{}, errors.New("GitHub credential persistence failed")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return GitHubCredentialStatus{}, errors.New("GitHub credential activation failed")
	}
	return GitHubCredentialStatus{Configured: true, Owner: owner}, nil
}

func LoadGitHubCredential(stateRoot string) (GitHubCredential, error) {
	path := filepath.Join(filepath.Clean(strings.TrimSpace(stateRoot)), githubCredentialFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return GitHubCredential{}, ErrGitHubNotConfigured
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(info) {
		return GitHubCredential{}, errors.New("local GitHub credential file is unsafe")
	}
	body, err := os.ReadFile(path)
	if err != nil || len(body) > 1024 {
		return GitHubCredential{}, errors.New("local GitHub credential is unavailable")
	}
	var credential GitHubCredential
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&credential) != nil || credential.SchemaVersion != 1 || !githubOwnerPattern.MatchString(credential.Owner) || !validGitHubToken(credential.Token) {
		return GitHubCredential{}, errors.New("local GitHub credential is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return GitHubCredential{}, errors.New("local GitHub credential has trailing data")
	}
	return credential, nil
}

func LocalGitHubCredentialStatus(stateRoot string) (GitHubCredentialStatus, error) {
	credential, err := LoadGitHubCredential(stateRoot)
	if errors.Is(err, ErrGitHubNotConfigured) {
		return GitHubCredentialStatus{}, nil
	}
	if err != nil {
		return GitHubCredentialStatus{}, err
	}
	return GitHubCredentialStatus{Configured: true, Owner: credential.Owner}, nil
}

func validGitHubToken(token string) bool {
	if len(token) < 20 || len(token) > 1024 || strings.TrimSpace(token) != token {
		return false
	}
	return !strings.ContainsFunc(token, func(r rune) bool { return r <= 0x20 || r == 0x7f })
}
