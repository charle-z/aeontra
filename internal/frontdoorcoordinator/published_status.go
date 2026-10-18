package frontdoorcoordinator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	compactStatusDescriptionPrefix   = "mcp-fdc:v2 "
	maxPublishedStatusDescriptionLen = 255
	maxPublishedDeploymentIDLen      = 40
)

var publishedDeploymentIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,40}$`)

type compactPublishedStatus struct {
	Revision       uint64 `json:"r"`
	RequestID      string `json:"q,omitempty"`
	Target         string `json:"t"`
	RecoveryTarget string `json:"y,omitempty"`
	State          string `json:"s"`
	Phase          string `json:"p,omitempty"`
	DeploymentID   string `json:"d,omitempty"`
	Reason         string `json:"x,omitempty"`
	UpdatedAt      int64  `json:"u,omitempty"`
}

func encodePublishedStatus(status Status) (string, error) {
	if err := validateStatus(status); err != nil {
		return "", fmt.Errorf("invalid coordinator status for publication: %w", err)
	}
	if status.State != StateIdle && status.UpdatedAt.IsZero() {
		return "", errors.New("published coordinator status requires an update time")
	}
	if status.DeploymentID != "" {
		if len(status.DeploymentID) > maxPublishedDeploymentIDLen || !publishedDeploymentIDPattern.MatchString(status.DeploymentID) {
			return "", errors.New("published coordinator deployment id is invalid")
		}
	}
	target, err := encodePublishedTarget(status.Target)
	if err != nil {
		return "", err
	}
	recovery, err := encodeOptionalPublishedTarget(status.RecoveryTarget)
	if err != nil {
		return "", err
	}
	state, err := encodePublishedState(status.State)
	if err != nil {
		return "", err
	}
	phase, err := encodePublishedPhase(status.Phase)
	if err != nil {
		return "", err
	}
	reason, err := encodePublishedReason(status.Reason)
	if err != nil {
		return "", err
	}
	envelope := compactPublishedStatus{
		Revision: status.Revision, RequestID: status.RequestID, Target: target,
		RecoveryTarget: recovery, State: state, Phase: phase,
		DeploymentID: status.DeploymentID, Reason: reason,
	}
	if !status.UpdatedAt.IsZero() {
		envelope.UpdatedAt = status.UpdatedAt.UTC().Unix()
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	description := compactStatusDescriptionPrefix + string(data)
	if len(description) > maxPublishedStatusDescriptionLen {
		return "", fmt.Errorf("published coordinator status exceeds %d bytes", maxPublishedStatusDescriptionLen)
	}
	return description, nil
}

func decodeCompactPublishedStatus(raw string) (Status, error) {
	if len(compactStatusDescriptionPrefix)+len(raw) > maxPublishedStatusDescriptionLen {
		return Status{}, fmt.Errorf("published coordinator status exceeds %d bytes", maxPublishedStatusDescriptionLen)
	}
	var envelope compactPublishedStatus
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return Status{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Status{}, errors.New("published coordinator status has trailing data")
		}
		return Status{}, err
	}
	target, err := decodePublishedTarget(envelope.Target)
	if err != nil {
		return Status{}, err
	}
	recovery, err := decodeOptionalPublishedTarget(envelope.RecoveryTarget)
	if err != nil {
		return Status{}, err
	}
	state, err := decodePublishedState(envelope.State)
	if err != nil {
		return Status{}, err
	}
	phase, err := decodePublishedPhase(envelope.Phase)
	if err != nil {
		return Status{}, err
	}
	reason, err := decodePublishedReason(envelope.Reason)
	if err != nil {
		return Status{}, err
	}
	if envelope.DeploymentID != "" {
		if len(envelope.DeploymentID) > maxPublishedDeploymentIDLen || !publishedDeploymentIDPattern.MatchString(envelope.DeploymentID) {
			return Status{}, errors.New("published coordinator deployment id is invalid")
		}
	}
	status := Status{
		SchemaVersion:  1,
		Revision:       envelope.Revision,
		RequestID:      envelope.RequestID,
		Target:         target,
		RecoveryTarget: recovery,
		State:          state,
		Phase:          phase,
		DeploymentID:   envelope.DeploymentID,
		Reason:         reason,
	}
	if envelope.UpdatedAt != 0 {
		status.UpdatedAt = time.Unix(envelope.UpdatedAt, 0).UTC()
	}
	if status.State != StateIdle && status.UpdatedAt.IsZero() {
		return Status{}, errors.New("published coordinator status requires an update time")
	}
	if err := validateStatus(status); err != nil {
		return Status{}, fmt.Errorf("invalid published coordinator status: %w", err)
	}
	return status, nil
}

func encodePublishedTarget(target Target) (string, error) {
	switch target {
	case TargetIdle:
		return "i", nil
	case TargetCutover:
		return "c", nil
	case TargetRollback:
		return "r", nil
	default:
		return "", errors.New("published coordinator target is invalid")
	}
}

func encodeOptionalPublishedTarget(target Target) (string, error) {
	if target == "" {
		return "", nil
	}
	return encodePublishedTarget(target)
}

func decodePublishedTarget(code string) (Target, error) {
	switch code {
	case "i":
		return TargetIdle, nil
	case "c":
		return TargetCutover, nil
	case "r":
		return TargetRollback, nil
	default:
		return "", errors.New("published coordinator target code is invalid")
	}
}

func decodeOptionalPublishedTarget(code string) (Target, error) {
	if code == "" {
		return "", nil
	}
	return decodePublishedTarget(code)
}

func encodePublishedState(state State) (string, error) {
	switch state {
	case StateIdle:
		return "i", nil
	case StateQueued:
		return "q", nil
	case StateRunning:
		return "r", nil
	case StateCompensating:
		return "c", nil
	case StateSucceeded:
		return "s", nil
	case StateFailed:
		return "f", nil
	default:
		return "", errors.New("published coordinator state is invalid")
	}
}

func decodePublishedState(code string) (State, error) {
	switch code {
	case "i":
		return StateIdle, nil
	case "q":
		return StateQueued, nil
	case "r":
		return StateRunning, nil
	case "c":
		return StateCompensating, nil
	case "s":
		return StateSucceeded, nil
	case "f":
		return StateFailed, nil
	default:
		return "", errors.New("published coordinator state code is invalid")
	}
}

func encodePublishedPhase(phase Phase) (string, error) {
	switch phase {
	case PhaseNone:
		return "", nil
	case PhaseAddBackendOrigin:
		return "a", nil
	case PhaseSwitchFrontBackend:
		return "b", nil
	case PhaseReleasePublicBackend:
		return "c", nil
	case PhaseAssignPublicFront:
		return "d", nil
	case PhaseMoveFrontTemporary:
		return "e", nil
	case PhaseRestorePublicBackend:
		return "f", nil
	case PhaseSwitchFrontPublicBackend:
		return "g", nil
	case PhaseRemoveAlternateBackend:
		return "h", nil
	case PhaseComplete:
		return "z", nil
	default:
		return "", errors.New("published coordinator phase is invalid")
	}
}

func decodePublishedPhase(code string) (Phase, error) {
	switch code {
	case "":
		return PhaseNone, nil
	case "a":
		return PhaseAddBackendOrigin, nil
	case "b":
		return PhaseSwitchFrontBackend, nil
	case "c":
		return PhaseReleasePublicBackend, nil
	case "d":
		return PhaseAssignPublicFront, nil
	case "e":
		return PhaseMoveFrontTemporary, nil
	case "f":
		return PhaseRestorePublicBackend, nil
	case "g":
		return PhaseSwitchFrontPublicBackend, nil
	case "h":
		return PhaseRemoveAlternateBackend, nil
	case "z":
		return PhaseComplete, nil
	default:
		return "", errors.New("published coordinator phase code is invalid")
	}
}

func encodePublishedReason(reason string) (string, error) {
	if reason == "" {
		return "", nil
	}
	base := reason
	suffixCode := ""
	for suffix, code := range map[string]string{
		"_compensation_topology_invalid": "t",
		"_compensation_budget_exhausted": "b",
		"_compensation_unavailable":      "u",
		"_compensated":                   "c",
	} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
			suffixCode = code
			break
		}
	}
	baseCode := ""
	switch base {
	case "topology_invalid":
		baseCode = "i"
	case "topology_read_failed":
		baseCode = "r"
	case "topology_read_budget_exhausted":
		baseCode = "q"
	case "transition_budget_exhausted":
		baseCode = "e"
	default:
		if strings.HasSuffix(base, "_failed") {
			phase := Phase(strings.TrimSuffix(base, "_failed"))
			phaseCode, err := encodePublishedPhase(phase)
			if err == nil && phaseCode != "" && phase != PhaseComplete {
				baseCode = "f" + phaseCode
			}
		}
	}
	if baseCode == "" {
		return "", errors.New("published coordinator reason is outside the fixed contract")
	}
	if suffixCode == "" {
		return baseCode, nil
	}
	return baseCode + "." + suffixCode, nil
}

func decodePublishedReason(code string) (string, error) {
	if code == "" {
		return "", nil
	}
	parts := strings.Split(code, ".")
	if len(parts) > 2 || parts[0] == "" {
		return "", errors.New("published coordinator reason code is invalid")
	}
	base := ""
	switch parts[0] {
	case "i":
		base = "topology_invalid"
	case "r":
		base = "topology_read_failed"
	case "q":
		base = "topology_read_budget_exhausted"
	case "e":
		base = "transition_budget_exhausted"
	default:
		if len(parts[0]) == 2 && strings.HasPrefix(parts[0], "f") {
			phase, err := decodePublishedPhase(parts[0][1:])
			if err == nil && phase != PhaseNone && phase != PhaseComplete {
				base = string(phase) + "_failed"
			}
		}
	}
	if base == "" {
		return "", errors.New("published coordinator reason base code is invalid")
	}
	if len(parts) == 1 {
		return base, nil
	}
	suffix := ""
	switch parts[1] {
	case "t":
		suffix = "_compensation_topology_invalid"
	case "b":
		suffix = "_compensation_budget_exhausted"
	case "u":
		suffix = "_compensation_unavailable"
	case "c":
		suffix = "_compensated"
	default:
		return "", errors.New("published coordinator reason suffix code is invalid")
	}
	return base + suffix, nil
}
