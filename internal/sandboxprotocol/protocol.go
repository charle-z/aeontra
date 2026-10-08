package sandboxprotocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
)

const (
	Backend              = "rootless-podman"
	LegacyProfileVersion = "l3-v1"
	ProfileVersion       = "l3-v2"
)

type Status struct {
	Available      bool   `json:"available"`
	Backend        string `json:"backend"`
	Rootless       bool   `json:"rootless"`
	NetworkProfile string `json:"network_profile"`
	ImageDigest    string `json:"image_digest"`
	ProfileVersion string `json:"profile_version"`
}

type Request struct {
	SchemaVersion  int               `json:"schema_version"`
	ProfileVersion string            `json:"profile_version,omitempty"`
	IdempotencyKey string            `json:"idempotency_key"`
	RequestDigest  string            `json:"request_digest"`
	WorkspaceID    string            `json:"workspace_id"`
	WorkspaceScope string            `json:"workspace_scope,omitempty"`
	RelativeDir    string            `json:"relative_dir"`
	Argv           []string          `json:"argv"`
	Environment    map[string]string `json:"environment,omitempty"`
	NetworkProfile string            `json:"network_profile"`
	TimeoutMS      int64             `json:"timeout_ms"`
	CPUMillis      int               `json:"cpu_millis"`
	MemoryMiB      int               `json:"memory_mib"`
	ProcessLimit   int               `json:"process_limit"`
	OutputBytes    int               `json:"output_bytes"`
}

// Error is the bounded public failure contract returned by the private executor.
// Code and Message are selected by the executor; raw engine, path and host details
// never cross the private boundary.
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)

func ValidError(value Error) bool {
	return errorCodePattern.MatchString(value.Code) && value.Message != "" && len(value.Message) <= 512
}

type Response struct {
	IdempotencyKey string `json:"idempotency_key"`
	RequestDigest  string `json:"request_digest"`
	ExitCode       int    `json:"exit_code"`
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
	DurationMS     int64  `json:"duration_ms"`
	Truncated      bool   `json:"truncated"`
}

func Digest(request Request) (string, error) {
	request.RequestDigest = ""
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
