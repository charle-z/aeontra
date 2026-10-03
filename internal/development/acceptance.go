package development

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
)

const (
	commandDigestDomain      = "aeontra-development-command-v1\x00"
	acceptanceDigestDomain   = "aeontra-development-acceptance-contract-v1\x00"
	maxAcceptanceArgvCount   = 256
	maxAcceptanceArgvBytes   = 64 << 10
	maxAcceptanceArtifactRef = 32
)

var (
	privateGoalRefPattern = regexp.MustCompile(`^mb_[a-f0-9]{32}$`)
	operationRefPattern   = regexp.MustCompile(`^eo_[a-f0-9]{32}$`)
	runnerEffectPattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// CommandAcceptanceContract binds a command to one immutable source and a
// private staged body. Argv and source contents remain outside the objective
// record. RequiredCapabilities must exactly match the objective step.
type CommandAcceptanceContract struct {
	CommandDigest        string        `json:"command_digest"`
	SourceDigest         string        `json:"source_digest"`
	PrivateBodyRef       string        `json:"private_body_ref"`
	PrivateBodyDigest    string        `json:"private_body_digest"`
	RequiredCapabilities []Requirement `json:"required_capabilities"`
	ArtifactRefs         []string      `json:"artifact_refs,omitempty"`
}

// CommandAcceptanceReceipt records the dispatcher-verified result for one
// exact command attempt. The caller must authenticate the Edge operation,
// target, request key, workspace, source and environment before constructing a
// receipt; this value is binding evidence, not a cryptographic attestation.
type CommandAcceptanceReceipt struct {
	OperationID         string   `json:"operation_id"`
	EvidenceProvider    string   `json:"evidence_provider,omitempty"`
	RunnerEffectID      string   `json:"runner_effect_id,omitempty"`
	RunnerRunID         int64    `json:"runner_run_id,omitempty"`
	RunnerReceiptDigest string   `json:"runner_receipt_digest,omitempty"`
	ObjectiveID         string   `json:"objective_id"`
	StepID              string   `json:"step_id"`
	AttemptID           string   `json:"attempt_id"`
	ContractDigest      string   `json:"contract_digest"`
	CommandDigest       string   `json:"command_digest"`
	SourceDigest        string   `json:"source_digest"`
	EnvironmentDigest   string   `json:"environment_digest"`
	PrivateBodyRef      string   `json:"private_body_ref"`
	PrivateBodyDigest   string   `json:"private_body_digest"`
	ExitCode            int      `json:"exit_code"`
	ArtifactRefs        []string `json:"artifact_refs,omitempty"`
}

// CommandOperationEvidence contains fields observed by the internal dispatcher
// after it authenticates one terminal Edge operation. It is not an
// authentication token and must never be accepted directly from an MCP caller.
type CommandOperationEvidence struct {
	OperationID         string
	EvidenceProvider    string
	RunnerEffectID      string
	RunnerRunID         int64
	RunnerReceiptDigest string
	CommandDigest       string
	SourceDigest        string
	EnvironmentDigest   string
	PrivateBodyRef      string
	PrivateBodyDigest   string
	ExitCode            int
	ArtifactRefs        []string
}

// NewCommandAcceptanceContract hashes argv as a length-delimited sequence and
// stores only its digest. The private body must have been staged before this
// contract is attached to a durable objective.
func NewCommandAcceptanceContract(argv []string, sourceDigest, privateBodyRef, privateBodyDigest string, requiredCapabilities []Requirement, artifactRefs []string) (CommandAcceptanceContract, error) {
	commandDigest, err := CommandDigest(argv)
	if err != nil {
		return CommandAcceptanceContract{}, err
	}
	contract := CommandAcceptanceContract{
		CommandDigest:        commandDigest,
		SourceDigest:         strings.TrimSpace(sourceDigest),
		PrivateBodyRef:       strings.TrimSpace(privateBodyRef),
		PrivateBodyDigest:    strings.TrimSpace(privateBodyDigest),
		RequiredCapabilities: append([]Requirement(nil), requiredCapabilities...),
		ArtifactRefs:         append([]string(nil), artifactRefs...),
	}
	if normalized, err := normalizeRequirementList(contract.RequiredCapabilities); err == nil {
		contract.RequiredCapabilities = normalized
	} else {
		return CommandAcceptanceContract{}, err
	}
	if normalized, err := normalizeOpaqueReferences(contract.ArtifactRefs); err == nil {
		contract.ArtifactRefs = normalized
	} else {
		return CommandAcceptanceContract{}, err
	}
	if !contract.valid() {
		return CommandAcceptanceContract{}, errors.New("development command acceptance contract is invalid")
	}
	return contract, nil
}

// CommandDigest returns a domain-separated digest of the exact argv sequence.
// Empty arguments are preserved; NUL bytes and unbounded inputs are rejected.
func CommandDigest(argv []string) (string, error) {
	if len(argv) == 0 || len(argv) > maxAcceptanceArgvCount {
		return "", errors.New("development command argv is invalid")
	}
	total := 0
	for _, argument := range argv {
		if strings.IndexByte(argument, 0) >= 0 || len(argument) > maxAcceptanceArgvBytes-total {
			return "", errors.New("development command argv is invalid")
		}
		total += len(argument)
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(commandDigestDomain))
	var length [8]byte
	for _, argument := range argv {
		binary.BigEndian.PutUint64(length[:], uint64(len(argument)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(argument))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

// NewCommandAcceptanceReceipt is for the internal dispatcher after it has
// verified the terminal successful Edge operation and its bindings. The core
// validates the observed command/source/environment/body against the durable
// contract and derives objective, step, attempt and contract identity from state.
func NewCommandAcceptanceReceipt(objective Objective, stepID string, evidence CommandOperationEvidence) (CommandAcceptanceReceipt, error) {
	if !objective.Valid() || objective.State != ObjectiveAcceptancePending || evidence.ExitCode != 0 {
		return CommandAcceptanceReceipt{}, errors.New("development command acceptance evidence is invalid")
	}
	index := objective.stepIndex(stepID)
	if index < 0 {
		return CommandAcceptanceReceipt{}, errors.New("development command acceptance evidence is invalid")
	}
	step := objective.Steps[index]
	if step.AcceptanceContract == nil || len(step.Attempts) == 0 {
		return CommandAcceptanceReceipt{}, errors.New("development command acceptance evidence is invalid")
	}
	attempt := step.Attempts[len(step.Attempts)-1]
	if attempt.State != AttemptSucceeded {
		return CommandAcceptanceReceipt{}, errors.New("development command acceptance evidence is invalid")
	}
	contract := step.AcceptanceContract
	receipt := CommandAcceptanceReceipt{
		OperationID:         evidence.OperationID,
		EvidenceProvider:    evidence.EvidenceProvider,
		RunnerEffectID:      evidence.RunnerEffectID,
		RunnerRunID:         evidence.RunnerRunID,
		RunnerReceiptDigest: evidence.RunnerReceiptDigest,
		ObjectiveID:         objective.ObjectiveID,
		StepID:              step.StepID,
		AttemptID:           attempt.AttemptID,
		ContractDigest:      contract.digest(objective.ObjectiveID, step.StepID, objective.Policy),
		CommandDigest:       evidence.CommandDigest,
		SourceDigest:        evidence.SourceDigest,
		EnvironmentDigest:   evidence.EnvironmentDigest,
		PrivateBodyRef:      evidence.PrivateBodyRef,
		PrivateBodyDigest:   evidence.PrivateBodyDigest,
		ExitCode:            evidence.ExitCode,
		ArtifactRefs:        append([]string(nil), evidence.ArtifactRefs...),
	}
	if !receipt.validFor(objective, index) {
		return CommandAcceptanceReceipt{}, errors.New("development command acceptance evidence is invalid")
	}
	return receipt, nil
}

func (contract CommandAcceptanceContract) valid() bool {
	if !sourceDigestPattern.MatchString(contract.CommandDigest) ||
		!sourceDigestPattern.MatchString(contract.SourceDigest) ||
		!privateGoalRefPattern.MatchString(contract.PrivateBodyRef) ||
		!sourceDigestPattern.MatchString(contract.PrivateBodyDigest) ||
		len(contract.RequiredCapabilities) > MaxEnvironmentCatalogEntries ||
		len(contract.ArtifactRefs) > maxAcceptanceArtifactRef {
		return false
	}
	requirements, err := normalizeRequirementList(contract.RequiredCapabilities)
	if err != nil || !sameRequirements(contract.RequiredCapabilities, requirements) {
		return false
	}
	artifacts, err := normalizeOpaqueReferences(contract.ArtifactRefs)
	return err == nil && sameStrings(contract.ArtifactRefs, artifacts)
}

func (contract CommandAcceptanceContract) validFor(policy ResolutionPolicy, requirements []Requirement) bool {
	return contract.valid() && policy.valid() && sameRequirements(contract.RequiredCapabilities, requirements)
}

func (contract CommandAcceptanceContract) clone() CommandAcceptanceContract {
	contract.RequiredCapabilities = append([]Requirement(nil), contract.RequiredCapabilities...)
	contract.ArtifactRefs = append([]string(nil), contract.ArtifactRefs...)
	return contract
}

func sameCommandAcceptanceContract(left, right *CommandAcceptanceContract) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.CommandDigest == right.CommandDigest && left.SourceDigest == right.SourceDigest &&
		left.PrivateBodyRef == right.PrivateBodyRef && left.PrivateBodyDigest == right.PrivateBodyDigest &&
		sameRequirements(left.RequiredCapabilities, right.RequiredCapabilities) &&
		sameStrings(left.ArtifactRefs, right.ArtifactRefs)
}

func sameCommandAcceptanceContractMode(steps []ObjectiveStep) bool {
	contracted := 0
	for _, step := range steps {
		if step.AcceptanceContract != nil {
			contracted++
		}
	}
	return contracted == 0 || contracted == len(steps)
}

func (contract CommandAcceptanceContract) digest(objectiveID, stepID string, policy ResolutionPolicy) string {
	if !contract.valid() || !identityPattern.MatchString(objectiveID) || !identityPattern.MatchString(stepID) || !policy.valid() {
		return ""
	}
	record := struct {
		ObjectiveID          string           `json:"objective_id"`
		StepID               string           `json:"step_id"`
		MaxTier              AuthorityTier    `json:"max_tier"`
		AllowedClasses       []ExecutionClass `json:"allowed_classes"`
		CommandDigest        string           `json:"command_digest"`
		SourceDigest         string           `json:"source_digest"`
		PrivateBodyRef       string           `json:"private_body_ref"`
		PrivateBodyDigest    string           `json:"private_body_digest"`
		RequiredCapabilities []string         `json:"required_capabilities"`
		ArtifactRefs         []string         `json:"artifact_refs"`
	}{
		ObjectiveID:          objectiveID,
		StepID:               stepID,
		MaxTier:              policy.MaxTier(),
		AllowedClasses:       policy.AllowedClasses(),
		CommandDigest:        contract.CommandDigest,
		SourceDigest:         contract.SourceDigest,
		PrivateBodyRef:       contract.PrivateBodyRef,
		PrivateBodyDigest:    contract.PrivateBodyDigest,
		RequiredCapabilities: make([]string, 0, len(contract.RequiredCapabilities)),
		ArtifactRefs:         append([]string(nil), contract.ArtifactRefs...),
	}
	for _, requirement := range contract.RequiredCapabilities {
		record.RequiredCapabilities = append(record.RequiredCapabilities, string(requirement.ID))
	}
	body, err := json.Marshal(record)
	if err != nil {
		return ""
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(acceptanceDigestDomain))
	_, _ = hash.Write(body)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func (receipt CommandAcceptanceReceipt) validFor(objective Objective, stepIndex int) bool {
	if stepIndex < 0 || stepIndex >= len(objective.Steps) ||
		receipt.ObjectiveID != objective.ObjectiveID || receipt.ExitCode != 0 {
		return false
	}
	step := objective.Steps[stepIndex]
	if step.AcceptanceContract == nil || step.StepID != receipt.StepID || len(step.Attempts) == 0 {
		return false
	}
	attempt := step.Attempts[len(step.Attempts)-1]
	contract := step.AcceptanceContract
	return receipt.validExecutionIdentity(attempt.Class) && attempt.State == AttemptSucceeded && receipt.AttemptID == attempt.AttemptID &&
		receipt.ContractDigest == contract.digest(objective.ObjectiveID, step.StepID, objective.Policy) &&
		receipt.CommandDigest == contract.CommandDigest && receipt.SourceDigest == contract.SourceDigest &&
		receipt.SourceDigest == attempt.SourceDigest && receipt.EnvironmentDigest == attempt.EnvironmentDigest &&
		receipt.PrivateBodyRef == contract.PrivateBodyRef && receipt.PrivateBodyDigest == contract.PrivateBodyDigest &&
		sameStrings(receipt.ArtifactRefs, contract.ArtifactRefs)
}

// An Edge operation and a GitHub run have different authorities and identity
// namespaces. The dispatcher authenticates either source before supplying this
// binding evidence; a workflow never fabricates an Edge operation identifier.
func (receipt CommandAcceptanceReceipt) validExecutionIdentity(class ExecutionClass) bool {
	if receipt.EvidenceProvider == "github-run" {
		return class == ClassIsolatedRunner && receipt.OperationID == "" &&
			runnerEffectPattern.MatchString(receipt.RunnerEffectID) && receipt.RunnerRunID > 0 &&
			sourceDigestPattern.MatchString(receipt.RunnerReceiptDigest)
	}
	return receipt.EvidenceProvider == "" && class != ClassIsolatedRunner &&
		operationRefPattern.MatchString(receipt.OperationID) && receipt.RunnerEffectID == "" &&
		receipt.RunnerRunID == 0 && receipt.RunnerReceiptDigest == ""
}

func (receipt CommandAcceptanceReceipt) clone() CommandAcceptanceReceipt {
	receipt.ArtifactRefs = append([]string(nil), receipt.ArtifactRefs...)
	return receipt
}

func (receipt CommandAcceptanceReceipt) clonePointer() *CommandAcceptanceReceipt {
	copy := receipt.clone()
	return &copy
}

func normalizeOpaqueReferences(references []string) ([]string, error) {
	if len(references) > maxAcceptanceArtifactRef {
		return nil, errors.New("development artifact reference bound exceeded")
	}
	canonical := append([]string(nil), references...)
	for _, reference := range canonical {
		if !identityPattern.MatchString(reference) || strings.Contains(reference, "..") || strings.Contains(reference, "://") {
			return nil, errors.New("development artifact reference is invalid")
		}
	}
	slices.Sort(canonical)
	for index := 1; index < len(canonical); index++ {
		if canonical[index] == canonical[index-1] {
			return nil, errors.New("development artifact reference is duplicated")
		}
	}
	return canonical, nil
}

func sameStrings(left, right []string) bool {
	return slices.Equal(left, right)
}
