package edge

import (
	"errors"
	"reflect"

	"github.com/charle-z/mcp-devbox/internal/development"
)

const (
	OperationProjectDevelopmentBootstrapResolve OperationKind = "project_development_bootstrap_resolve"
	OperationProjectDevelopmentBootstrapStart   OperationKind = "project_development_bootstrap_start"
)

// ProjectDevelopmentBootstrapBinding contains only server-owned selection
// metadata. A repository or public MCP caller cannot submit an installer,
// download URL, shell command, or host path through this operation.
type ProjectDevelopmentBootstrapBinding struct {
	Version          int                              `json:"version"`
	Anchor           development.WorkspaceAnchor      `json:"anchor"`
	CapabilityID     development.CapabilityID         `json:"capability_id"`
	Resolution       *development.BootstrapResolution `json:"resolution,omitempty"`
	ResolutionDigest string                           `json:"resolution_digest,omitempty"`
}

func (binding ProjectDevelopmentBootstrapBinding) Valid(resolved bool) bool {
	if binding.Version != 1 || !binding.Anchor.Valid() {
		return false
	}
	// Validate an unresolved selector using a canonical, non-executable
	// sample. The resolver supplies the actual official artifact afterward.
	if !development.BootstrapCapabilitySupported(binding.CapabilityID) {
		return false
	}
	if !resolved {
		return binding.Resolution == nil && binding.ResolutionDigest == ""
	}
	if binding.Resolution == nil || binding.Resolution.CapabilityID != binding.CapabilityID {
		return false
	}
	digest, err := development.BootstrapResolutionDigest(*binding.Resolution)
	return err == nil && digest == binding.ResolutionDigest
}

func normalizeDevelopmentBootstrapRequest(kind OperationKind, request OperationRequest) (OperationRequest, error) {
	if request.DevelopmentBootstrap == nil || !request.DevelopmentBootstrap.Valid(kind == OperationProjectDevelopmentBootstrapStart) {
		return OperationRequest{}, errors.New("development bootstrap binding is invalid")
	}
	base := request
	base.DevelopmentBootstrap = nil
	if err := validateDevelopmentRecovery(request); err != nil {
		return OperationRequest{}, err
	}
	if kind != OperationProjectDevelopmentBootstrapStart && request.DevelopmentRecoveryOperationID != "" {
		return OperationRequest{}, errors.New("development resolve cannot recover a process")
	}
	base.DevelopmentRecoveryOperationID = ""
	base.DevelopmentRecoveryIdempotencyKey = ""
	// Reuse the complete closed project inspection contract. No executable
	// fields or incidental authority from another operation are accepted.
	normalized, err := validateOperationRequestWithProjectExec(OperationProjectDevelopmentInspect, base)
	if err != nil {
		return OperationRequest{}, err
	}
	normalized.DevelopmentBootstrap = request.DevelopmentBootstrap
	normalized.DevelopmentRecoveryOperationID = request.DevelopmentRecoveryOperationID
	normalized.DevelopmentRecoveryIdempotencyKey = request.DevelopmentRecoveryIdempotencyKey
	return normalized, nil
}

func validDevelopmentBootstrapResult(kind OperationKind, result OperationResult) bool {
	binding := result.DevelopmentBootstrap
	if binding == nil || !binding.Valid(true) || result.DevelopmentInspection != nil || result.DevelopmentCommand != nil ||
		binding.Anchor.WorkspaceID != result.WorkspaceID || binding.Anchor.Owner != result.ProjectOwner || binding.Anchor.Repository != result.ProjectRepository {
		return false
	}
	base := result
	base.DevelopmentBootstrap = nil
	if kind == OperationProjectDevelopmentBootstrapStart {
		return validProjectProcessResult(base)
	}
	return kind == OperationProjectDevelopmentBootstrapResolve && !hasProjectProcessResult(base) &&
		(validProjectOperationResult(base) || validRegisteredProjectOperationResult(base))
}

// BootstrapBindingsEqual compares canonical selections without reconstructing
// source bytes or allowing a retry to substitute a newer floating resolution.
func BootstrapBindingsEqual(left, right *ProjectDevelopmentBootstrapBinding) bool {
	return left != nil && right != nil && left.Valid(true) && right.Valid(true) && reflect.DeepEqual(left, right)
}
