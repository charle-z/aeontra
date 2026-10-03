package edge

import (
	"errors"
	"regexp"
	"slices"
	"strconv"

	"github.com/charle-z/mcp-devbox/internal/development"
)

const OperationProjectDevelopmentCommandStart OperationKind = "project_development_command_start"

var developmentBodyRefPattern = regexp.MustCompile(`^mb_[a-f0-9]{32}$`)

// NormalizeDevelopmentCommand applies the existing developer command policy
// before any goal is staged or operation queued. It adds no host authority.
func NormalizeDevelopmentCommand(request OperationRequest) (OperationRequest, error) {
	timeout := request.TimeoutSeconds
	if timeout < 1 || timeout > 86400 || request.DevelopmentCommand != nil {
		return OperationRequest{}, errors.New("development command timeout is invalid")
	}
	request.TimeoutSeconds = 0
	normalized, err := validateOperationRequestWithProjectExec(OperationProjectProcessStart, request)
	if err != nil {
		return OperationRequest{}, err
	}
	normalized.TimeoutSeconds = timeout
	if !developmentCommandFitsTimeoutWrapper(normalized.Argv, timeout) {
		return OperationRequest{}, errors.New("development command exceeds bounded timeout wrapper contract")
	}
	return normalized, nil
}

// ProjectDevelopmentCommandBinding is private coordinator-to-Edge metadata.
// Clients cannot supply a receipt or authorize a different workspace with it.
type ProjectDevelopmentCommandBinding struct {
	Version           int                         `json:"version"`
	Anchor            development.WorkspaceAnchor `json:"anchor"`
	SourceDigest      string                      `json:"source_digest"`
	EnvironmentDigest string                      `json:"environment_digest"`
	CommandDigest     string                      `json:"command_digest"`
	PrivateBodyRef    string                      `json:"private_body_ref"`
	PrivateBodyDigest string                      `json:"private_body_digest"`
	Requirements      []development.CapabilityID  `json:"requirements"`
	TimeoutSeconds    int                         `json:"timeout_seconds"`
}

func (binding ProjectDevelopmentCommandBinding) Valid() bool {
	if binding.Version != 1 || !binding.Anchor.Valid() || binding.TimeoutSeconds < 1 || binding.TimeoutSeconds > 86400 ||
		!operationCatalogPattern.MatchString(binding.SourceDigest) || !operationCatalogPattern.MatchString(binding.EnvironmentDigest) ||
		!operationCatalogPattern.MatchString(binding.CommandDigest) || !operationCatalogPattern.MatchString(binding.PrivateBodyDigest) ||
		!developmentBodyRefPattern.MatchString(binding.PrivateBodyRef) {
		return false
	}
	ids := make([]string, len(binding.Requirements))
	for i, id := range binding.Requirements {
		ids[i] = string(id)
	}
	set, err := development.NewCapabilitySet(ids...)
	return err == nil && slices.Equal(set.IDs(), binding.Requirements)
}

func normalizeProjectDevelopmentCommandRequest(request OperationRequest) (OperationRequest, error) {
	binding := request.DevelopmentCommand
	if binding == nil || !binding.Valid() {
		return OperationRequest{}, errors.New("development command binding is invalid")
	}
	base := request
	base.DevelopmentCommand = nil
	if err := validateDevelopmentRecovery(request); err != nil {
		return OperationRequest{}, err
	}
	base.DevelopmentRecoveryOperationID = ""
	base.DevelopmentRecoveryIdempotencyKey = ""
	normalized, err := validateOperationRequestWithProjectExec(OperationProjectProcessStart, base)
	if err != nil {
		return OperationRequest{}, err
	}
	digest, err := development.CommandDigest(normalized.Argv)
	if err != nil || digest != binding.CommandDigest || !developmentCommandFitsTimeoutWrapper(normalized.Argv, binding.TimeoutSeconds) {
		return OperationRequest{}, errors.New("development command digest mismatch")
	}
	normalized.DevelopmentCommand = binding
	normalized.DevelopmentRecoveryOperationID = request.DevelopmentRecoveryOperationID
	normalized.DevelopmentRecoveryIdempotencyKey = request.DevelopmentRecoveryIdempotencyKey
	return normalized, nil
}

func developmentCommandFitsTimeoutWrapper(argv []string, timeout int) bool {
	if len(argv)+4 > 128 {
		return false
	}
	bytes := len("timeout") + len("--signal=TERM") + len("--kill-after=10s") + len(strconv.Itoa(timeout)) + 1
	for _, argument := range argv {
		bytes += len(argument)
	}
	return bytes <= maxProjectExecArgvBytes
}

func validateDevelopmentRecovery(request OperationRequest) error {
	if request.DevelopmentRecoveryOperationID == "" && request.DevelopmentRecoveryIdempotencyKey == "" {
		return nil
	}
	if !operationIDPattern.MatchString(request.DevelopmentRecoveryOperationID) ||
		!projectOperationIdempotencyPattern.MatchString(request.DevelopmentRecoveryIdempotencyKey) ||
		request.IdempotencyKey == request.DevelopmentRecoveryIdempotencyKey {
		return errors.New("development recovery identity is invalid")
	}
	return nil
}

func validProjectDevelopmentCommandResult(result OperationResult) bool {
	if result.DevelopmentCommand == nil || !result.DevelopmentCommand.Valid() || result.DevelopmentInspection != nil ||
		result.WorkspaceID != result.DevelopmentCommand.Anchor.WorkspaceID ||
		result.ProjectOwner != result.DevelopmentCommand.Anchor.Owner || result.ProjectRepository != result.DevelopmentCommand.Anchor.Repository {
		return false
	}
	metadata := result
	metadata.DevelopmentCommand = nil
	return validProjectProcessResult(metadata)
}
