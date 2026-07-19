package edgeclient

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	HTBLabBrokerSocketName   = "htb-lab-broker.sock"
	HTBLabBrokerSandboxURL   = "http://unix/v1/ssh-exec"
	htbLabBrokerRequestLimit = int64(64 << 10)
	htbLabSSHSourceLimit     = int64(8 << 20)
	htbLabSSHOutputLimit     = int64(4 << 20)
)

var htbLabSSHUsernamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)

type HTBLabSSHRequest struct {
	Username       string `json:"username"`
	Source         string `json:"source"`
	ExtractAfter   string `json:"extract_after"`
	Command        string `json:"command"`
	SaveOutput     string `json:"save_output,omitempty"`
	PasswordStdin  bool   `json:"password_stdin,omitempty"`
	PTY            bool   `json:"pty,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	Port           int    `json:"port,omitempty"`
}

type HTBLabSSHResponse struct {
	Status    string `json:"status"`
	Target    string `json:"target"`
	Username  string `json:"username"`
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
	SavedPath string `json:"saved_path,omitempty"`
	Bytes     int    `json:"bytes,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

func validateHTBLabSSHRequest(request HTBLabSSHRequest) (HTBLabSSHRequest, error) {
	request.Username = strings.TrimSpace(request.Username)
	request.Source = filepath.Clean(strings.TrimSpace(request.Source))
	request.ExtractAfter = strings.Trim(request.ExtractAfter, "\r\n")
	request.Command = strings.TrimSpace(request.Command)
	request.SaveOutput = filepath.Clean(strings.TrimSpace(request.SaveOutput))
	if !htbLabSSHUsernamePattern.MatchString(request.Username) {
		return HTBLabSSHRequest{}, errors.New("lab SSH username is invalid")
	}
	if request.Source == "." || filepath.IsAbs(request.Source) || request.Source == ".." || strings.HasPrefix(request.Source, ".."+string(os.PathSeparator)) {
		return HTBLabSSHRequest{}, errors.New("lab credential source is invalid")
	}
	top := strings.Split(filepath.ToSlash(request.Source), "/")[0]
	if top != "loot" && top != "scans" && top != "tmp" {
		return HTBLabSSHRequest{}, errors.New("lab credential source is invalid")
	}
	if request.ExtractAfter == "" || len(request.ExtractAfter) > 256 || strings.ContainsAny(request.ExtractAfter, "\r\n") {
		return HTBLabSSHRequest{}, errors.New("lab credential prefix is invalid")
	}
	if request.Command == "" || len(request.Command) > 16<<10 || strings.ContainsRune(request.Command, 0) || strings.ContainsAny(request.Command, "\r\n") {
		return HTBLabSSHRequest{}, errors.New("lab SSH command is invalid")
	}
	if request.SaveOutput == "." {
		request.SaveOutput = ""
	}
	if request.SaveOutput != "" {
		if filepath.IsAbs(request.SaveOutput) || request.SaveOutput == ".." || strings.HasPrefix(request.SaveOutput, ".."+string(os.PathSeparator)) {
			return HTBLabSSHRequest{}, errors.New("lab SSH output path is invalid")
		}
		top = strings.Split(filepath.ToSlash(request.SaveOutput), "/")[0]
		if top != "loot" && top != "reports" && top != "tmp" {
			return HTBLabSSHRequest{}, errors.New("lab SSH output path is invalid")
		}
	}
	if request.Port == 0 {
		request.Port = 22
	}
	if request.Port < 1 || request.Port > 65535 {
		return HTBLabSSHRequest{}, errors.New("lab SSH port is invalid")
	}
	if request.TimeoutSeconds == 0 {
		request.TimeoutSeconds = 120
	}
	if request.TimeoutSeconds < 5 || request.TimeoutSeconds > 600 {
		return HTBLabSSHRequest{}, errors.New("lab SSH timeout is invalid")
	}
	return request, nil
}

func extractHTBLabCredential(workspace string, request HTBLabSSHRequest) ([]byte, error) {
	body, err := readHTBLabArtifact(workspace, request.Source, htbLabSSHSourceLimit)
	if err != nil {
		return nil, err
	}
	defer zeroHTBBytes(body)
	matches := make([][]byte, 0, 2)
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if !matchesHTBLabCredentialPrefix(line, request.ExtractAfter) {
			continue
		}
		value := bytes.TrimSpace(line[len(request.ExtractAfter):])
		if len(value) == 0 || len(value) > 1024 || bytes.ContainsAny(value, "\x00\r\n") {
			continue
		}
		matches = append(matches, append([]byte(nil), value...))
	}
	if len(matches) != 1 {
		for _, match := range matches {
			zeroHTBBytes(match)
		}
		return nil, errors.New("lab credential extraction must produce exactly one value")
	}
	return matches[0], nil
}

func matchesHTBLabCredentialPrefix(line []byte, prefix string) bool {
	value := []byte(prefix)
	if !bytes.HasPrefix(line, value) {
		return false
	}
	if len(line) == len(value) || len(value) == 0 {
		return len(line) > len(value)
	}
	last := value[len(value)-1]
	if last == ' ' || last == '\t' || last == ':' || last == '=' {
		return true
	}
	next := line[len(value)]
	return next == ' ' || next == '\t' || next == ':' || next == '='
}

func newHTBLabSSHResponse(workspace string, request HTBLabSSHRequest, target string, stdout, stderr []byte) (HTBLabSSHResponse, error) {
	response := HTBLabSSHResponse{Status: "ok", Target: target, Username: request.Username}
	if request.SaveOutput == "" {
		response.Stdout = string(stdout)
		response.Stderr = string(stderr)
		return response, nil
	}
	if err := writeHTBLabOutput(workspace, request.SaveOutput, stdout); err != nil {
		return HTBLabSSHResponse{}, err
	}
	digest := sha256.Sum256(stdout)
	response.SavedPath = filepath.ToSlash(request.SaveOutput)
	response.Bytes = len(stdout)
	response.SHA256 = hex.EncodeToString(digest[:])
	return response, nil
}

func decodeHTBLabBrokerRequest(reader io.Reader) (HTBLabSSHRequest, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, htbLabBrokerRequestLimit))
	decoder.DisallowUnknownFields()
	var request HTBLabSSHRequest
	if err := decoder.Decode(&request); err != nil {
		return HTBLabSSHRequest{}, errors.New("lab broker request is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return HTBLabSSHRequest{}, errors.New("lab broker request has trailing data")
	}
	return validateHTBLabSSHRequest(request)
}

func zeroHTBBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func htbLabTimeout(request HTBLabSSHRequest) time.Duration {
	return time.Duration(request.TimeoutSeconds) * time.Second
}
