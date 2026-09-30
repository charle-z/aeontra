package development

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestCapabilityAttestationDigestChangesWithGenerationAndCapabilities(t *testing.T) {
	base := mustEnvironment(t, "edge-workcell", ClassWorkcell, 1, "toolchain.go", "build.make")
	nextGeneration := mustEnvironment(t, "edge-workcell", ClassWorkcell, 2, "toolchain.go", "build.make")
	expanded := mustEnvironment(t, "edge-workcell", ClassWorkcell, 1, "toolchain.go", "build.make", "git.metadata.full")
	if base.Digest == nextGeneration.Digest || base.Digest == expanded.Digest {
		t.Fatal("attestation digest did not bind generation and capabilities")
	}
	if !base.Valid() || !nextGeneration.Valid() || !expanded.Valid() {
		t.Fatal("valid environment attestation rejected")
	}
}

func TestResolverKeepsCurrentEnvironmentWhenItSatisfiesRequirements(t *testing.T) {
	requirements := mustRequirements(t, "toolchain.go", "build.make")
	current := mustEnvironment(t, "trusted-workcell", ClassWorkcell, 1, "toolchain.go", "build.make")
	isolated := mustEnvironment(t, "isolated-linux", ClassIsolatedRunner, 1, "toolchain.go", "build.make", "namespace.user.nested")
	policy := mustPolicy(t, TierIsolatedRunner, ClassWorkcell, ClassIsolatedRunner)

	resolution, err := Resolve(requirements, &current, []EnvironmentAttestation{isolated}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Environment.Digest != current.Digest || resolution.Migrated {
		t.Fatalf("resolver moved a satisfiable current attempt: %+v", resolution)
	}
}

func TestResolverChoosesLowestAuthoritySingleEnvironment(t *testing.T) {
	requirements := mustRequirements(t, "toolchain.go", "service.postgres")
	toolbox := mustEnvironment(t, "project-toolbox", ClassToolbox, 1, "toolchain.go", "service.postgres")
	isolated := mustEnvironment(t, "isolated-linux", ClassIsolatedRunner, 1, "toolchain.go", "service.postgres", "namespace.user.nested")
	policy := mustPolicy(t, TierIsolatedRunner, ClassToolbox, ClassIsolatedRunner)

	resolution, err := Resolve(requirements, nil, []EnvironmentAttestation{isolated, toolbox}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Environment.EnvironmentID != toolbox.EnvironmentID {
		t.Fatalf("selected %s, want toolbox", resolution.Environment.EnvironmentID)
	}
}

func TestResolverNeverCombinesCapabilitiesAcrossEnvironments(t *testing.T) {
	requirements := mustRequirements(t, "namespace.user.nested", "ci.github-actions.cache")
	userns := mustEnvironment(t, "runner-userns", ClassIsolatedRunner, 1, "namespace.user.nested")
	cache := mustEnvironment(t, "runner-cache", ClassExternalValidationRunner, 1, "ci.github-actions.cache")
	policy := mustPolicy(t, TierIsolatedRunner, ClassIsolatedRunner, ClassExternalValidationRunner)

	_, err := Resolve(requirements, nil, []EnvironmentAttestation{userns, cache}, policy)
	var resolutionErr *ResolutionError
	if !errors.As(err, &resolutionErr) || resolutionErr.Reason != ResolutionFailureCapabilities || len(resolutionErr.Missing) != 1 {
		t.Fatalf("split authority was not rejected: %T %v", err, err)
	}
}

func TestResolverPolicyEnvelopeBlocksUnapprovedClass(t *testing.T) {
	requirements := mustRequirements(t, "namespace.user.nested")
	isolated := mustEnvironment(t, "isolated-linux", ClassIsolatedRunner, 1, "namespace.user.nested")
	policy := mustPolicy(t, TierToolbox, ClassWorkcell, ClassToolbox)

	_, err := Resolve(requirements, nil, []EnvironmentAttestation{isolated}, policy)
	var resolutionErr *ResolutionError
	if !errors.As(err, &resolutionErr) || resolutionErr.Reason != ResolutionFailurePolicy {
		t.Fatalf("unapproved runner did not fail policy: %T %v", err, err)
	}
}

func TestUnknownRequirementDoesNotGrantAuthority(t *testing.T) {
	requirements := mustRequirements(t, "host.root-shell")
	workcell := mustEnvironment(t, "trusted-workcell", ClassWorkcell, 1, "toolchain.go")
	policy := mustPolicy(t, TierWorkcell, ClassWorkcell)

	_, err := Resolve(requirements, nil, []EnvironmentAttestation{workcell}, policy)
	var resolutionErr *ResolutionError
	if !errors.As(err, &resolutionErr) || !slices.Contains(resolutionErr.Missing, CapabilityID("host.root-shell")) {
		t.Fatalf("unknown requirement unexpectedly became available: %T %v", err, err)
	}
}

func TestCapabilityMigrationCreatesNewAttemptWithoutMutatingOldTarget(t *testing.T) {
	workcell := mustEnvironment(t, "trusted-workcell", ClassWorkcell, 1, "toolchain.go", "build.make")
	runner := mustEnvironment(t, "isolated-linux", ClassIsolatedRunner, 4, "toolchain.go", "build.make", "namespace.user.nested")

	first, err := NewExecutionAttempt("attempt-1", "validate", sourceDigest("a"), workcell)
	if err != nil {
		t.Fatal(err)
	}
	first, err = first.Transition(AttemptRunning)
	if err != nil {
		t.Fatal(err)
	}
	first, err = first.Fail(FailureKernelSemantics)
	if err != nil {
		t.Fatal(err)
	}
	next, err := MigrateExecutionAttempt("attempt-2", sourceDigest("a"), first, runner)
	if err != nil {
		t.Fatal(err)
	}
	if first.EnvironmentID != workcell.EnvironmentID || first.EnvironmentDigest != workcell.Digest {
		t.Fatal("previous attempt target mutated")
	}
	if next.ParentAttemptID != first.AttemptID || next.EnvironmentID != runner.EnvironmentID ||
		next.EnvironmentDigest != runner.Digest || next.SourceDigest != first.SourceDigest || next.State != AttemptPlanned {
		t.Fatalf("unexpected migrated attempt: %+v", next)
	}
}

func TestCodeFailureRequiresChangedSourceAtSameAuthority(t *testing.T) {
	workcell := mustEnvironment(t, "trusted-workcell", ClassWorkcell, 1, "toolchain.go")
	runner := mustEnvironment(t, "isolated-linux", ClassIsolatedRunner, 1, "toolchain.go", "namespace.user.nested")
	first, _ := NewExecutionAttempt("attempt-1", "tests", sourceDigest("a"), workcell)
	first, _ = first.Transition(AttemptRunning)
	first, _ = first.Fail(FailureCode)

	if _, err := ContinueExecutionAttempt("attempt-2", sourceDigest("b"), first, runner); err == nil {
		t.Fatal("code failure was allowed to escalate execution authority")
	}
	next, err := ContinueExecutionAttempt("attempt-2", sourceDigest("b"), first, workcell)
	if err != nil {
		t.Fatal(err)
	}
	if next.SourceDigest == first.SourceDigest || next.EnvironmentDigest != first.EnvironmentDigest {
		t.Fatalf("code retry did not bind changed source and stable authority: %+v", next)
	}
}

func TestCapabilityFailureCannotReplayUnchangedEnvironment(t *testing.T) {
	workcell := mustEnvironment(t, "trusted-workcell", ClassWorkcell, 1, "toolchain.go")
	first, _ := NewExecutionAttempt("attempt-1", "tests", sourceDigest("a"), workcell)
	first, _ = first.Transition(AttemptRunning)
	first, _ = first.Fail(FailureCapabilityMissing)

	if _, err := MigrateExecutionAttempt("attempt-2", sourceDigest("a"), first, workcell); err == nil {
		t.Fatal("unchanged capability failure was allowed to replay")
	}
}

func TestExternalTransientRetryRequiresSameSourceAndEnvironment(t *testing.T) {
	workcell := mustEnvironment(t, "trusted-workcell", ClassWorkcell, 1, "toolchain.go")
	first, _ := NewExecutionAttempt("attempt-1", "tests", sourceDigest("a"), workcell)
	first, _ = first.Transition(AttemptRunning)
	first, _ = first.Fail(FailureExternalTransient)

	next, err := ContinueExecutionAttempt("attempt-2", sourceDigest("a"), first, workcell)
	if err != nil {
		t.Fatal(err)
	}
	if next.ParentAttemptID != first.AttemptID {
		t.Fatalf("transient retry lost parent identity: %+v", next)
	}
	if _, err := ContinueExecutionAttempt("attempt-3", sourceDigest("b"), first, workcell); err == nil {
		t.Fatal("transient retry accepted changed source")
	}
}

func TestBuildKitValidateAllFixtureRequiresKernelAndCICompatibleRunner(t *testing.T) {
	requirements := buildKitValidateAllRequirements(t)
	l3 := mustEnvironment(t, "l3", ClassWorkcell, 7,
		"toolchain.go", "build.make", "git.metadata.full",
	)
	rootless := mustEnvironment(t, "parrot-rootless", ClassRootlessRuntime, 12,
		"toolchain.go", "build.make", "container.docker.rootless", "filesystem.workspace-bind", "git.metadata.full",
	)
	isolated := mustEnvironment(t, "linux-ci-vm", ClassIsolatedRunner, 3,
		"toolchain.go", "build.make", "container.docker.rootless", "filesystem.workspace-bind",
		"namespace.user.nested", "idmap.subuid", "cgroup.v2.delegated", "git.metadata.full",
		"ci.github-actions.runtime", "ci.github-actions.cache", "service.containerd", "worker.stargz",
	)
	policy := mustPolicy(t, TierIsolatedRunner, ClassWorkcell, ClassRootlessRuntime, ClassIsolatedRunner)

	resolution, err := Resolve(requirements, &l3, []EnvironmentAttestation{l3, rootless, isolated}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Environment.EnvironmentID != isolated.EnvironmentID || !resolution.Migrated {
		t.Fatalf("validate-all did not route to the compatible runner: %+v", resolution)
	}

	_, err = Resolve(requirements, &l3, []EnvironmentAttestation{l3, rootless}, policy)
	var resolutionErr *ResolutionError
	if !errors.As(err, &resolutionErr) || resolutionErr.Reason != ResolutionFailureCapabilities {
		t.Fatalf("missing kernel/CI runner was not classified as a capability gap: %T %v", err, err)
	}
	for _, required := range []CapabilityID{
		"namespace.user.nested",
		"ci.github-actions.runtime",
		"ci.github-actions.cache",
	} {
		if !slices.Contains(resolutionErr.Missing, required) {
			t.Fatalf("missing capability %s not reported: %v", required, resolutionErr.Missing)
		}
	}
}

func TestObjectiveRefinesBuildKitRequirementsAndCreatesNewAttempt(t *testing.T) {
	l3 := mustEnvironment(t, "l3", ClassWorkcell, 7, "toolchain.go", "build.make", "git.metadata.full")
	isolated := mustEnvironment(t, "linux-ci-vm", ClassIsolatedRunner, 3,
		"toolchain.go", "build.make", "container.docker.rootless", "filesystem.workspace-bind",
		"namespace.user.nested", "idmap.subuid", "cgroup.v2.delegated", "git.metadata.full",
		"ci.github-actions.runtime", "ci.github-actions.cache", "service.containerd", "worker.stargz",
	)
	policy := mustPolicy(t, TierIsolatedRunner, ClassWorkcell, ClassIsolatedRunner)
	objective, err := NewObjective("buildkit-7206-validation", policy, []StepSpec{{
		StepID:       "validate-all",
		Requirements: mustRequirements(t, "toolchain.go", "build.make"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	objective, firstResolution, err := objective.PlanAttempt("validate-all", "attempt-1", sourceDigest("a"), []EnvironmentAttestation{l3, isolated})
	if err != nil {
		t.Fatal(err)
	}
	if firstResolution.Environment.EnvironmentID != l3.EnvironmentID {
		t.Fatalf("initial minimal requirements did not use L3: %+v", firstResolution)
	}
	objective, _ = objective.StartAttempt("validate-all")
	objective, action, err := objective.FailAttempt("validate-all", FailureKernelSemantics)
	if err != nil || action != ActionProvisionOrMigrate {
		t.Fatalf("kernel failure action=%s err=%v", action, err)
	}
	objective, err = objective.RefineRequirements("validate-all", buildKitValidateAllRequirements(t))
	if err != nil {
		t.Fatal(err)
	}
	objective, secondResolution, err := objective.PlanAttempt("validate-all", "attempt-2", sourceDigest("a"), []EnvironmentAttestation{l3, isolated})
	if err != nil {
		t.Fatal(err)
	}
	if !secondResolution.Migrated || secondResolution.Environment.EnvironmentID != isolated.EnvironmentID {
		t.Fatalf("refined requirements did not migrate: %+v", secondResolution)
	}
	if got := objective.Steps[0].Attempts; len(got) != 2 || got[0].EnvironmentID != l3.EnvironmentID ||
		got[1].EnvironmentID != isolated.EnvironmentID || got[1].ParentAttemptID != got[0].AttemptID {
		t.Fatalf("attempt history was not preserved: %+v", got)
	}
}

func TestObjectiveNeverAcceptsRuntimeCompletionByItself(t *testing.T) {
	workcell := mustEnvironment(t, "l3", ClassWorkcell, 1, "toolchain.go")
	policy := mustPolicy(t, TierWorkcell, ClassWorkcell)
	objective, _ := NewObjective("objective-1", policy, []StepSpec{{
		StepID: "test", Requirements: mustRequirements(t, "toolchain.go"),
	}})
	objective, _, _ = objective.PlanAttempt("test", "attempt-1", sourceDigest("a"), []EnvironmentAttestation{workcell})
	objective, _ = objective.StartAttempt("test")
	objective, err := objective.CompleteAttempt("test")
	if err != nil {
		t.Fatal(err)
	}
	if objective.State != ObjectiveAcceptancePending {
		t.Fatalf("completed attempt state=%s want acceptance_pending", objective.State)
	}
	if _, err := objective.Accept(); err != nil {
		t.Fatal(err)
	}
}

func TestFailureClassesHaveClosedContinuationPolicy(t *testing.T) {
	cases := map[FailureClass]ContinuationAction{
		FailureCode:                 ActionFixCode,
		FailureCapabilityMissing:    ActionProvisionOrMigrate,
		FailureCapabilityDrift:      ActionProvisionOrMigrate,
		FailureDependencyMissing:    ActionProvisionOrMigrate,
		FailureServiceUnavailable:   ActionProvisionOrMigrate,
		FailureKernelSemantics:      ActionProvisionOrMigrate,
		FailureCIContract:           ActionProvisionOrMigrate,
		FailureFilesystemMapping:    ActionProvisionOrMigrate,
		FailurePlatformMismatch:     ActionProvisionOrMigrate,
		FailureExternalTransient:    ActionRetrySameEnvironment,
		FailurePolicyDenied:         ActionStopPolicy,
		FailureReconciliationNeeded: ActionReconcile,
	}
	for class, want := range cases {
		got, ok := ContinuationForFailure(class)
		if !ok || got != want {
			t.Fatalf("failure %s action=%s ok=%t want=%s", class, got, ok, want)
		}
	}
	if _, ok := ContinuationForFailure("mystery"); ok {
		t.Fatal("unknown failure class was accepted")
	}
}

func buildKitValidateAllRequirements(t *testing.T) []Requirement {
	t.Helper()
	return mustRequirements(t,
		"toolchain.go",
		"build.make",
		"container.docker.rootless",
		"filesystem.workspace-bind",
		"namespace.user.nested",
		"idmap.subuid",
		"cgroup.v2.delegated",
		"git.metadata.full",
		"ci.github-actions.runtime",
		"ci.github-actions.cache",
		"service.containerd",
		"worker.stargz",
	)
}

func sourceDigest(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func mustCapabilities(t *testing.T, raw ...string) CapabilitySet {
	t.Helper()
	set, err := NewCapabilitySet(raw...)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func mustRequirements(t *testing.T, raw ...string) []Requirement {
	t.Helper()
	requirements, err := Requirements(raw...)
	if err != nil {
		t.Fatal(err)
	}
	return requirements
}

func mustEnvironment(t *testing.T, id string, class ExecutionClass, generation uint64, capabilities ...string) EnvironmentAttestation {
	t.Helper()
	environment, err := NewEnvironmentAttestation(id, class, generation, mustCapabilities(t, capabilities...))
	if err != nil {
		t.Fatal(err)
	}
	return environment
}

func mustPolicy(t *testing.T, max AuthorityTier, classes ...ExecutionClass) ResolutionPolicy {
	t.Helper()
	policy, err := NewResolutionPolicy(max, classes...)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}
