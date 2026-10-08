package development

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCommandAcceptanceUsesRealGitHubRunIdentity(t *testing.T) {
	_, contract, _ := commandAcceptanceFixture(t)
	policy := mustPolicy(t, TierIsolatedRunner, ClassIsolatedRunner)
	objective, err := NewObjective("objective-runner-evidence", policy, []StepSpec{{StepID: "validate", Requirements: contract.RequiredCapabilities, AcceptanceContract: &contract}})
	if err != nil {
		t.Fatal(err)
	}
	environment := mustEnvironment(t, "runner-template", ClassIsolatedRunner, 3, "build.make", "toolchain.go")
	pending := completeCommandAttempt(t, objective, contract, environment, "runner-attempt")
	evidence := commandOperationEvidence("", contract, environment, 0)
	body, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	fields["EvidenceProvider"] = "github-run"
	fields["RunnerEffectID"] = strings.Repeat("c", 64)
	fields["RunnerRunID"] = 12345
	fields["RunnerReceiptDigest"] = sourceDigest("d")
	body, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &evidence); err != nil {
		t.Fatal(err)
	}
	receipt, err := NewCommandAcceptanceReceipt(pending, "validate", evidence)
	if err != nil {
		t.Fatalf("verified GitHub run required a fabricated Edge operation: %v", err)
	}
	accepted, err := pending.AcceptWithEvidence([]CommandAcceptanceReceipt{receipt})
	if err != nil {
		t.Fatal(err)
	}
	body, _, err = accepted.MarshalRecord()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseObjectiveRecord(body); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*CommandAcceptanceReceipt){
		func(r *CommandAcceptanceReceipt) { r.OperationID = "eo_" + strings.Repeat("a", 32) },
		func(r *CommandAcceptanceReceipt) { r.RunnerRunID = 0 },
		func(r *CommandAcceptanceReceipt) { r.RunnerRunID = -1 },
		func(r *CommandAcceptanceReceipt) { r.RunnerEffectID = "unbounded-effect" },
		func(r *CommandAcceptanceReceipt) { r.RunnerReceiptDigest = "" },
		func(r *CommandAcceptanceReceipt) { r.EvidenceProvider = "caller-attested" },
		func(r *CommandAcceptanceReceipt) { r.EnvironmentDigest = sourceDigest("f") },
	} {
		bad := receipt.clone()
		mutate(&bad)
		if _, err := pending.AcceptWithEvidence([]CommandAcceptanceReceipt{bad}); err == nil {
			t.Fatal("invalid runner evidence was accepted")
		}
	}
	_, workcellContract, workcell := commandAcceptanceFixture(t)
	workcellObjective, _, _ := commandAcceptanceFixture(t)
	workcellPending := completeCommandAttempt(t, workcellObjective, workcellContract, workcell, "workcell-attempt")
	evidence.EnvironmentDigest = workcell.Digest
	if _, err := NewCommandAcceptanceReceipt(workcellPending, "validate", evidence); err == nil {
		t.Fatal("GitHub receipt accepted for an Edge workcell")
	}
}

func TestCommandObjectiveRequiresAttemptBoundAcceptanceReceipt(t *testing.T) {
	objective, contract, environment := commandAcceptanceFixture(t)
	planned, _, err := objective.PlanAttempt("validate", "attempt-1", contract.SourceDigest, []EnvironmentAttestation{environment})
	if err != nil {
		t.Fatal(err)
	}
	started, err := planned.StartAttempt("validate")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := started.CompleteAttempt("validate")
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != ObjectiveAcceptancePending {
		t.Fatalf("successful command moved objective to %s, want acceptance_pending", pending.State)
	}
	if _, err := pending.Accept(); err == nil {
		t.Fatal("legacy acceptance accepted an objective with a command contract")
	}
	if _, err := pending.AcceptWithEvidence(nil); err == nil {
		t.Fatal("command objective accepted without an evidence receipt")
	}

	receipt, err := NewCommandAcceptanceReceipt(pending, "validate", commandOperationEvidence("eo_0123456789abcdef0123456789abcdef", contract, environment, 0))
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := pending.AcceptWithEvidence([]CommandAcceptanceReceipt{receipt})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.State != ObjectiveAccepted || accepted.Steps[0].AcceptanceReceipt == nil {
		t.Fatalf("valid command evidence was not retained: %+v", accepted)
	}
	if accepted.Steps[0].AcceptanceReceipt.AttemptID != "attempt-1" ||
		accepted.Steps[0].AcceptanceReceipt.EnvironmentDigest != environment.Digest ||
		accepted.Steps[0].AcceptanceReceipt.CommandDigest != contract.CommandDigest {
		t.Fatalf("receipt did not bind the executed attempt: %+v", accepted.Steps[0].AcceptanceReceipt)
	}
	if err := ValidateTransition(pending, accepted); err != nil {
		t.Fatalf("valid receipt transition rejected: %v", err)
	}
	body, _, err := accepted.MarshalRecord()
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := ParseObjectiveRecord(body)
	if err != nil || recovered.Steps[0].AcceptanceReceipt == nil || !reflect.DeepEqual(*recovered.Steps[0].AcceptanceReceipt, receipt) {
		t.Fatalf("accepted receipt did not survive record round trip: %+v err=%v", recovered, err)
	}

	rewritten := accepted.clone()
	rewritten.Revision++
	rewritten.Steps[0].AcceptanceReceipt.OperationID = "eo_ffffffffffffffffffffffffffffffff"
	if err := ValidateTransition(accepted, rewritten); err == nil {
		t.Fatal("transition rewrote immutable acceptance evidence")
	}
}

func TestCommandAcceptanceFailsClosedOnSourceAndCapabilityDrift(t *testing.T) {
	objective, contract, environment := commandAcceptanceFixture(t)
	if _, _, err := objective.PlanAttempt("validate", "attempt-wrong-source", sourceDigest("b"), []EnvironmentAttestation{environment}); err == nil {
		t.Fatal("command contract accepted a different source digest")
	}
	planned, _, err := objective.PlanAttempt("validate", "attempt-code-failure", contract.SourceDigest, []EnvironmentAttestation{environment})
	if err != nil {
		t.Fatal(err)
	}
	started, err := planned.StartAttempt("validate")
	if err != nil {
		t.Fatal(err)
	}
	failed, _, err := started.FailAttempt("validate", FailureCode)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := failed.PlanAttempt("validate", "attempt-changed-source", sourceDigest("b"), []EnvironmentAttestation{environment}); err == nil {
		t.Fatal("code retry reused an immutable command contract for changed source")
	}
	if _, err := failed.RefineRequirements("validate", mustRequirements(t, "git.metadata.full")); err == nil {
		t.Fatal("failed command objective refined capabilities without a new contract")
	}

	newContract, err := NewCommandAcceptanceContract(
		[]string{"go", "test", "./..."}, sourceDigest("b"),
		"mb_ffffffffffffffffffffffffffffffff", sourceDigest("f"), contract.RequiredCapabilities,
		contract.ArtifactRefs,
	)
	if err != nil {
		t.Fatal(err)
	}
	newObjective, err := NewObjective("objective-command-revision", failed.Policy, []StepSpec{{
		StepID: "validate", Requirements: contract.RequiredCapabilities, AcceptanceContract: &newContract,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := newObjective.PlanAttempt("validate", "attempt-new-contract", newContract.SourceDigest, []EnvironmentAttestation{environment}); err != nil {
		t.Fatalf("new source with a new staged command contract was rejected: %v", err)
	}

	missingCapability := mustEnvironment(t, "workcell-missing", ClassWorkcell, 1, "toolchain.go")
	if _, _, err := objective.PlanAttempt("validate", "attempt-missing-capability", contract.SourceDigest, []EnvironmentAttestation{missingCapability}); err == nil {
		t.Fatal("command contract planned in an environment missing a bound requirement")
	}
}

func TestCommandAcceptanceRejectsSpoofedOrStaleReceipts(t *testing.T) {
	objective, contract, environment := commandAcceptanceFixture(t)
	pending := completeCommandAttempt(t, objective, contract, environment, "attempt-current")
	valid, err := NewCommandAcceptanceReceipt(pending, "validate", commandOperationEvidence("eo_11111111111111111111111111111111", contract, environment, 0))
	if err != nil {
		t.Fatal(err)
	}

	mutations := []struct {
		name   string
		change func(*CommandAcceptanceReceipt)
	}{
		{name: "wrong objective", change: func(r *CommandAcceptanceReceipt) { r.ObjectiveID = "objective-forged" }},
		{name: "wrong step", change: func(r *CommandAcceptanceReceipt) { r.StepID = "other-step" }},
		{name: "stale attempt", change: func(r *CommandAcceptanceReceipt) { r.AttemptID = "attempt-old" }},
		{name: "wrong source", change: func(r *CommandAcceptanceReceipt) { r.SourceDigest = sourceDigest("b") }},
		{name: "wrong environment", change: func(r *CommandAcceptanceReceipt) { r.EnvironmentDigest = sourceDigest("c") }},
		{name: "wrong command", change: func(r *CommandAcceptanceReceipt) { r.CommandDigest = sourceDigest("d") }},
		{name: "wrong contract", change: func(r *CommandAcceptanceReceipt) { r.ContractDigest = sourceDigest("e") }},
		{name: "wrong private body", change: func(r *CommandAcceptanceReceipt) { r.PrivateBodyRef = "mb_ffffffffffffffffffffffffffffffff" }},
		{name: "wrong private body digest", change: func(r *CommandAcceptanceReceipt) { r.PrivateBodyDigest = sourceDigest("f") }},
		{name: "unknown artifact", change: func(r *CommandAcceptanceReceipt) { r.ArtifactRefs = []string{"ba_ffffffffffffffffffffffffffffffff"} }},
		{name: "nonzero exit", change: func(r *CommandAcceptanceReceipt) { r.ExitCode = 1 }},
		{name: "missing operation identity", change: func(r *CommandAcceptanceReceipt) { r.OperationID = "" }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			spoofed := valid.clone()
			test.change(&spoofed)
			if _, err := pending.AcceptWithEvidence([]CommandAcceptanceReceipt{spoofed}); err == nil {
				t.Fatal("acceptance trusted a receipt with mismatched execution evidence")
			}
		})
	}
	if _, err := NewCommandAcceptanceReceipt(pending, "validate", commandOperationEvidence("eo_22222222222222222222222222222222", contract, environment, 1)); err == nil {
		t.Fatal("nonzero command result produced a passing receipt")
	}
	wrongObservedEvidence := commandOperationEvidence("eo_22222222222222222222222222222223", contract, environment, 0)
	wrongObservedEvidence.CommandDigest = sourceDigest("9")
	if _, err := NewCommandAcceptanceReceipt(pending, "validate", wrongObservedEvidence); err == nil {
		t.Fatal("dispatcher evidence for another command was rebound to the contract")
	}
	wrongObservedEvidence = commandOperationEvidence("eo_22222222222222222222222222222224", contract, environment, 0)
	wrongObservedEvidence.SourceDigest = sourceDigest("8")
	if _, err := NewCommandAcceptanceReceipt(pending, "validate", wrongObservedEvidence); err == nil {
		t.Fatal("dispatcher evidence for another source was rebound to the contract")
	}
	wrongObservedEvidence = commandOperationEvidence("eo_22222222222222222222222222222225", contract, environment, 0)
	wrongObservedEvidence.EnvironmentDigest = sourceDigest("7")
	if _, err := NewCommandAcceptanceReceipt(pending, "validate", wrongObservedEvidence); err == nil {
		t.Fatal("dispatcher evidence for another environment was rebound to the attempt")
	}
	if _, err := pending.AcceptWithEvidence([]CommandAcceptanceReceipt{valid, valid}); err == nil {
		t.Fatal("duplicate receipt accepted")
	}
}

func TestCommandAcceptanceContractAndReceiptValidation(t *testing.T) {
	policy := mustPolicy(t, TierWorkcell, ClassWorkcell)
	requirements := mustRequirements(t, "toolchain.go")
	validContract, err := NewCommandAcceptanceContract(
		[]string{"go", "test", "./..."}, sourceDigest("a"),
		"mb_0123456789abcdef0123456789abcdef", sourceDigest("b"), requirements,
		[]string{"ba_0123456789abcdef0123456789abcdef"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCommandAcceptanceContract([]string{"go", "test\x00", "./..."}, sourceDigest("a"), "mb_0123456789abcdef0123456789abcdef", sourceDigest("b"), requirements, nil); err == nil {
		t.Fatal("command digest accepted an argv element containing NUL")
	}
	if _, err := NewCommandAcceptanceContract([]string{"go", "test"}, "sha256:ABC", "mb_0123456789abcdef0123456789abcdef", sourceDigest("b"), requirements, nil); err == nil {
		t.Fatal("contract accepted a noncanonical source digest")
	}
	if _, err := NewCommandAcceptanceContract([]string{"go", "test"}, sourceDigest("a"), "https://example.invalid/goal", sourceDigest("b"), requirements, nil); err == nil {
		t.Fatal("contract accepted a URL instead of an opaque private body reference")
	}
	if _, err := NewCommandAcceptanceContract([]string{"go", "test"}, sourceDigest("a"), "mb_0123456789abcdef0123456789abcdef", sourceDigest("b"), requirements, []string{"../private/path"}); err == nil {
		t.Fatal("contract accepted a path instead of an opaque artifact reference")
	}

	wrongRequirements := mustRequirements(t, "build.make")
	step := StepSpec{StepID: "validate", Requirements: wrongRequirements, AcceptanceContract: &validContract}
	if _, err := NewObjective("objective-bad-capabilities", policy, []StepSpec{step}); err == nil {
		t.Fatal("objective accepted a contract with a different capability set")
	}
	if !strings.HasPrefix(validContract.CommandDigest, "sha256:") {
		t.Fatalf("command digest is not canonical: %q", validContract.CommandDigest)
	}
	otherArgvDigest, err := CommandDigest([]string{"go test", "./..."})
	if err != nil || otherArgvDigest == validContract.CommandDigest {
		t.Fatalf("command digest did not preserve argv boundaries: digest=%q err=%v", otherArgvDigest, err)
	}
	otherPolicy := mustPolicy(t, TierIsolatedRunner, ClassWorkcell, ClassIsolatedRunner)
	if validContract.digest("objective-command-acceptance", "validate", policy) == validContract.digest("objective-command-acceptance", "validate", otherPolicy) {
		t.Fatal("contract digest did not bind the immutable authority policy")
	}

	matching, err := NewObjective("objective-contract-snapshot", policy, []StepSpec{{
		StepID: "validate", Requirements: requirements, AcceptanceContract: &validContract,
	}})
	if err != nil {
		t.Fatal(err)
	}
	environment := mustEnvironment(t, "contract-snapshot", ClassWorkcell, 1, "toolchain.go")
	planned, _, err := matching.PlanAttempt("validate", "contract-attempt", validContract.SourceDigest, []EnvironmentAttestation{environment})
	if err != nil {
		t.Fatal(err)
	}
	planned.Steps[0].Attempts[0].SourceDigest = sourceDigest("c")
	if planned.Valid() {
		t.Fatal("objective snapshot accepted an attempt outside its command source contract")
	}

	semanticStep := StepSpec{StepID: "review", Requirements: requirements}
	if _, err := NewObjective("objective-mixed-acceptance", policy, []StepSpec{
		{StepID: "validate", Requirements: requirements, AcceptanceContract: &validContract}, semanticStep,
	}); err == nil {
		t.Fatal("objective mixed command evidence acceptance with semantic acceptance")
	}
}

func TestCommandContractAndReceiptAreDeepCloned(t *testing.T) {
	objective, contract, environment := commandAcceptanceFixture(t)
	clonedObjective := objective.clone()
	clonedObjective.Steps[0].AcceptanceContract.ArtifactRefs[0] = "ba_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	if objective.Steps[0].AcceptanceContract.ArtifactRefs[0] == clonedObjective.Steps[0].AcceptanceContract.ArtifactRefs[0] {
		t.Fatal("objective clone shares mutable contract references")
	}
	pending := completeCommandAttempt(t, objective, contract, environment, "attempt-clone")
	receipt, err := NewCommandAcceptanceReceipt(pending, "validate", commandOperationEvidence("eo_33333333333333333333333333333333", contract, environment, 0))
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := pending.AcceptWithEvidence([]CommandAcceptanceReceipt{receipt})
	if err != nil {
		t.Fatal(err)
	}
	cloned := accepted.clone()
	cloned.Steps[0].AcceptanceReceipt.ArtifactRefs[0] = "ba_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if accepted.Steps[0].AcceptanceReceipt.ArtifactRefs[0] == cloned.Steps[0].AcceptanceReceipt.ArtifactRefs[0] {
		t.Fatal("receipt artifact references share mutable storage")
	}
}

func TestLegacyObjectiveRecordsRemainReadableAndAcceptancePendingIsNotAutoAccepted(t *testing.T) {
	policy := mustPolicy(t, TierWorkcell, ClassWorkcell)
	legacy, err := NewObjective("objective-legacy-acceptance", policy, []StepSpec{{
		StepID: "validate", Requirements: mustRequirements(t, "toolchain.go"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := legacy.MarshalRecord()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "acceptance_contract") || strings.Contains(string(body), "acceptance_receipt") {
		t.Fatalf("legacy record acquired command evidence fields: %s", body)
	}
	recovered, err := ParseObjectiveRecord(body)
	if err != nil || recovered.Steps[0].AcceptanceContract != nil || recovered.Steps[0].AcceptanceReceipt != nil {
		t.Fatalf("legacy record is no longer inspectable: %+v err=%v", recovered, err)
	}

	workcell := mustEnvironment(t, "legacy-workcell", ClassWorkcell, 1, "toolchain.go")
	planned, _, err := recovered.PlanAttempt("validate", "legacy-attempt", sourceDigest("a"), []EnvironmentAttestation{workcell})
	if err != nil {
		t.Fatal(err)
	}
	started, err := planned.StartAttempt("validate")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := started.CompleteAttempt("validate")
	if err != nil || pending.State != ObjectiveAcceptancePending {
		t.Fatalf("execution success automatically accepted a legacy objective: %+v err=%v", pending, err)
	}
}

func commandAcceptanceFixture(t *testing.T) (Objective, CommandAcceptanceContract, EnvironmentAttestation) {
	t.Helper()
	policy := mustPolicy(t, TierWorkcell, ClassWorkcell)
	requirements := mustRequirements(t, "build.make", "toolchain.go")
	contract, err := NewCommandAcceptanceContract(
		[]string{"go", "test", "./..."}, sourceDigest("a"),
		"mb_0123456789abcdef0123456789abcdef", sourceDigest("b"), requirements,
		[]string{"ba_0123456789abcdef0123456789abcdef"},
	)
	if err != nil {
		t.Fatal(err)
	}
	objective, err := NewObjective("objective-command-acceptance", policy, []StepSpec{{
		StepID: "validate", Requirements: requirements, AcceptanceContract: &contract,
	}})
	if err != nil {
		t.Fatal(err)
	}
	environment := mustEnvironment(t, "workcell-command", ClassWorkcell, 7, "build.make", "toolchain.go")
	return objective, contract, environment
}

func completeCommandAttempt(t *testing.T, objective Objective, contract CommandAcceptanceContract, environment EnvironmentAttestation, attemptID string) Objective {
	t.Helper()
	planned, _, err := objective.PlanAttempt("validate", attemptID, contract.SourceDigest, []EnvironmentAttestation{environment})
	if err != nil {
		t.Fatal(err)
	}
	started, err := planned.StartAttempt("validate")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := started.CompleteAttempt("validate")
	if err != nil {
		t.Fatal(err)
	}
	return pending
}

func commandOperationEvidence(operationID string, contract CommandAcceptanceContract, environment EnvironmentAttestation, exitCode int) CommandOperationEvidence {
	return CommandOperationEvidence{
		OperationID:       operationID,
		CommandDigest:     contract.CommandDigest,
		SourceDigest:      contract.SourceDigest,
		EnvironmentDigest: environment.Digest,
		PrivateBodyRef:    contract.PrivateBodyRef,
		PrivateBodyDigest: contract.PrivateBodyDigest,
		ExitCode:          exitCode,
		ArtifactRefs:      append([]string(nil), contract.ArtifactRefs...),
	}
}
