package sandboxprotocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const (
	Backend        = "rootless-podman"
	ProfileVersion = "l3-v1"
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
	IdempotencyKey string            `json:"idempotency_key"`
	RequestDigest  string            `json:"request_digest"`
	WorkspaceID    string            `json:"workspace_id"`
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
