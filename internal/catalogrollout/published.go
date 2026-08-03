package catalogrollout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const PublishedStatusPrefix = "mcp-cr:v1 "
const maxPublishedStatusLength = 255

var publishedReasonPattern = regexp.MustCompile(`^[a-z0-9_: -]{0,96}$`)
var publishedDeploymentPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{0,64}$`)

type compactStatus struct {
	Revision     uint64 `json:"r"`
	RequestID    string `json:"q"`
	State        State  `json:"s"`
	Phase        Phase  `json:"p,omitempty"`
	DeploymentID string `json:"d,omitempty"`
	Reason       string `json:"x,omitempty"`
	UpdatedAt    int64  `json:"u"`
}

func EncodePublishedStatus(status Status) (string, error) {
	if status.State == StateIdle || status.Request.RequestID == "" || status.UpdatedAt.IsZero() {
		return "", errors.New("catalog rollout status is not publishable")
	}
	if !publishedDeploymentPattern.MatchString(status.DeploymentID) || !publishedReasonPattern.MatchString(status.Reason) {
		return "", errors.New("catalog rollout status contains an invalid bounded field")
	}
	envelope := compactStatus{
		Revision: status.Revision, RequestID: status.Request.RequestID,
		State: status.State, Phase: status.Phase, DeploymentID: status.DeploymentID,
		Reason: status.Reason, UpdatedAt: status.UpdatedAt.UTC().Unix(),
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	description := PublishedStatusPrefix + string(data)
	if len(description) > maxPublishedStatusLength {
		return "", fmt.Errorf("published catalog rollout status exceeds %d bytes", maxPublishedStatusLength)
	}
	return description, nil
}

func DecodePublishedStatus(description string) (Status, bool, error) {
	description = strings.TrimSpace(description)
	if !strings.HasPrefix(description, PublishedStatusPrefix) {
		return Status{}, false, nil
	}
	if len(description) > maxPublishedStatusLength {
		return Status{}, true, errors.New("published catalog rollout status is too large")
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(description, PublishedStatusPrefix)))
	decoder.DisallowUnknownFields()
	var envelope compactStatus
	if err := decoder.Decode(&envelope); err != nil {
		return Status{}, true, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Status{}, true, errors.New("published catalog rollout status has trailing data")
	}
	if !requestPattern.MatchString(envelope.RequestID) || envelope.State == StateIdle || envelope.UpdatedAt <= 0 ||
		!publishedDeploymentPattern.MatchString(envelope.DeploymentID) || !publishedReasonPattern.MatchString(envelope.Reason) {
		return Status{}, true, errors.New("published catalog rollout status is invalid")
	}
	return Status{
		SchemaVersion: 1, Revision: envelope.Revision,
		Request: Request{RequestID: envelope.RequestID}, State: envelope.State,
		Phase: envelope.Phase, DeploymentID: envelope.DeploymentID,
		Reason: envelope.Reason, UpdatedAt: time.Unix(envelope.UpdatedAt, 0).UTC(),
	}, true, nil
}
