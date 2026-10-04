package edge

import (
	"github.com/charle-z/mcp-devbox/internal/development"
	"strings"
)

const OperationProjectDevelopmentInspect OperationKind = "project_development_inspect"

type ProjectDevelopmentInspection struct {
	Version               int                                `json:"version"`
	ProjectGeneration     uint64                             `json:"project_generation"`
	SourceDigest          string                             `json:"source_digest"`
	SourceHead            string                             `json:"source_head,omitempty"`
	SourceClean           bool                               `json:"source_clean"`
	SourceEvidenceKnown   bool                               `json:"source_evidence_known"`
	Requirements          []development.CapabilityID         `json:"requirements"`
	Environments          []development.EnvironmentRecord    `json:"environments"`
	GoCommandRequirements *development.GoCommandRequirements `json:"go_command_requirements,omitempty"`
}

func validProjectDevelopmentInspection(result OperationResult) bool {
	inspection := result.DevelopmentInspection
	if inspection == nil || inspection.Version != 1 || inspection.ProjectGeneration == 0 ||
		inspection.ProjectGeneration > 1<<63-1 || !operationCatalogPattern.MatchString(inspection.SourceDigest) || len(inspection.Requirements) > development.MaxEnvironmentCatalogEntries ||
		len(inspection.Environments) == 0 || len(inspection.Environments) > 4 {
		return false
	}
	if inspection.SourceEvidenceKnown {
		if !projectSnapshotCommitPattern.MatchString(inspection.SourceHead) {
			return false
		}
	} else if inspection.SourceHead != "" || inspection.SourceClean {
		return false
	}
	if inspection.GoCommandRequirements != nil && (!inspection.SourceEvidenceKnown || !inspection.SourceClean ||
		!inspection.GoCommandRequirements.Valid(inspection.SourceDigest, inspection.Requirements)) {
		return false
	}
	ids := make([]string, 0, len(inspection.Requirements))
	for _, id := range inspection.Requirements {
		ids = append(ids, string(id))
	}
	canonical, err := development.NewCapabilitySet(ids...)
	if err != nil || len(canonical.IDs()) != len(inspection.Requirements) {
		return false
	}
	for i, id := range canonical.IDs() {
		if id != inspection.Requirements[i] {
			return false
		}
	}
	environments := make([]development.EnvironmentAttestation, 0, len(inspection.Environments))
	for _, record := range inspection.Environments {
		attestation, err := record.Attestation()
		if err != nil {
			return false
		}
		switch attestation.Class {
		case development.ClassWorkcell:
			if attestation.EnvironmentID != "workcell:"+result.WorkspaceID || attestation.Generation != inspection.ProjectGeneration {
				return false
			}
		case development.ClassRootlessRuntime:
			if attestation.EnvironmentID != "rootless:"+result.WorkspaceID || attestation.Generation != inspection.ProjectGeneration {
				return false
			}
		case development.ClassToolbox:
			// Toolbox identity is owned and validated by the target's manager.
			if !strings.HasPrefix(attestation.EnvironmentID, "toolbox:") || !projectToolboxIDPattern.MatchString(strings.TrimPrefix(attestation.EnvironmentID, "toolbox:")) {
				return false
			}
		default:
			// A workcell device cannot claim authority of an external VM broker.
			return false
		}
		environments = append(environments, attestation)
	}
	if _, err := development.NewEnvironmentCatalog(environments...); err != nil {
		return false
	}
	metadata := result
	metadata.DevelopmentInspection = nil
	return validProjectOperationResult(metadata)
}
