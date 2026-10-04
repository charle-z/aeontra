package mcpserver

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/tools"
	"github.com/charle-z/mcp-devbox/internal/workqueue"
)

type goRequirementsRunner struct{ *developmentRunnerTestProvider }

func (runner *goRequirementsRunner) ConfiguredTemplateAttestation() (development.EnvironmentAttestation, error) {
	if !runner.calibrated {
		return runner.developmentRunnerTestProvider.ConfiguredTemplateAttestation()
	}
	ids, _ := development.VersionCapabilityIDs("toolchain.go", "1.26.8")
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, string(id))
	}
	caps, _ := development.NewCapabilitySet(names...)
	return development.NewEnvironmentAttestation("github-hosted-template-test", development.ClassIsolatedRunner, 1, caps)
}

func TestProjectDevelopmentGoRequirementsPreserveExplicitAndLegacyConstraints(t *testing.T) {
	for _, test := range []struct {
		name, minimum, workspaceMinimum, exactPin, explicit string
		legacy, sourceChanged                               bool
		want                                                workqueue.DevelopmentRequestState
	}{
		{name: "explicit actual version", minimum: "1.26.6", explicit: "toolchain.go.v1-26-8", want: workqueue.DevelopmentRequestCompleted},
		{name: "explicit older exact patch", minimum: "1.26.6", explicit: "toolchain.go.v1-26-6", want: workqueue.DevelopmentRequestAwaitingReasoning},
		{name: "explicit pnpm", minimum: "1.26.6", explicit: "toolchain.pnpm.v10-13-1", want: workqueue.DevelopmentRequestAwaitingReasoning},
		{name: "repository older exact pin", minimum: "1.26.6", exactPin: "1.26.6", want: workqueue.DevelopmentRequestAwaitingReasoning},
		{name: "higher minimum", minimum: "1.26.9", want: workqueue.DevelopmentRequestAwaitingReasoning},
		{name: "higher workspace minimum", minimum: "1.26.6", workspaceMinimum: "1.26.9", want: workqueue.DevelopmentRequestAwaitingReasoning},
		{name: "newer minor minimum", minimum: "1.27.0", want: workqueue.DevelopmentRequestAwaitingReasoning},
		{name: "older minor minimum", minimum: "1.25.9", want: workqueue.DevelopmentRequestCompleted},
		{name: "old Edge evidence", minimum: "1.26.6", legacy: true, want: workqueue.DevelopmentRequestAwaitingReasoning},
		{name: "source drift", minimum: "1.26.6", sourceChanged: true, want: workqueue.DevelopmentRequestAwaitingReasoning},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, edges, _ := developmentServer(t)
			edges.sourceChanged = test.sourceChanged
			server.WithEdgeStore(&developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
				if op.Kind != edge.OperationProjectDevelopmentInspect {
					return op
				}
				inspection := op.Result.DevelopmentInspection
				minimum, _ := development.VersionRequirement("toolchain.go", test.minimum)
				conservative := []string{string(minimum.ID), "toolchain.pnpm.v10-13-1"}
				evidence := &development.GoCommandRequirements{Version: 1, SourceDigest: inspection.SourceDigest,
					MinimumVersions: []development.GoMinimum{{Manifest: "go.mod", Version: test.minimum}}}
				if test.workspaceMinimum != "" {
					evidence.MinimumVersions = append(evidence.MinimumVersions, development.GoMinimum{Manifest: "go.work", Version: test.workspaceMinimum})
				}
				if test.exactPin != "" {
					pin, _ := development.VersionRequirement("toolchain.go", test.exactPin)
					evidence.ExactRequirements = []development.CapabilityID{pin.ID}
					conservative = append(conservative, string(pin.ID))
				}
				set, _ := development.NewCapabilitySet(conservative...)
				inspection.Requirements = set.IDs()
				if !test.legacy {
					inspection.GoCommandRequirements = evidence
				}
				return op
			}})
			runner := &goRequirementsRunner{&developmentRunnerTestProvider{queue: server.workQueue, calibrated: true}}
			server.developmentRunner = runner
			required := []string{}
			if test.explicit != "" {
				required = append(required, test.explicit)
			}
			body, _ := json.Marshal(projectDevelopmentStartParams{Alias: "project", Target: "parrot", IdempotencyKey: "runner-go-constraints-001", Argv: []string{"go", "test", "./...", "-count=1"}, TimeoutSeconds: 4200, Requirements: required, RunnerProfile: tools.DevelopmentRunnerProfile})
			output, err := server.handleProjectDevelopmentStart(body)
			if err != nil {
				t.Fatal(err)
			}
			var view projectDevelopmentView
			if err := json.Unmarshal([]byte(output), &view); err != nil {
				t.Fatal(err)
			}
			developmentRounds(t, server, 3)
			request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
			objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
			if request.State != test.want {
				t.Fatalf("request=%+v want=%s", request, test.want)
			}
			if test.explicit != "" && !slices.ContainsFunc(objective.Steps[0].Requirements, func(required development.Requirement) bool { return string(required.ID) == test.explicit }) {
				t.Fatal("explicit caller requirement dropped")
			}
			if test.want == workqueue.DevelopmentRequestAwaitingReasoning && !test.sourceChanged && (runner.posts != 0 || request.Reason != workqueue.DevelopmentRequestReasonNewRequirement) {
				t.Fatalf("unsupported requirement dispatched: posts=%d request=%+v", runner.posts, request)
			}
			if test.sourceChanged && objective.State == development.ObjectiveAccepted {
				t.Fatal("source drift accepted")
			}
			if edges.starts != 0 {
				t.Fatal("isolated request dispatched local workcell")
			}
		})
	}
}

func TestDevelopmentGoSourceInferenceIsExactCommandScoped(t *testing.T) {
	source := "sha256:" + strings.Repeat("a", 64)
	inspection := &edge.ProjectDevelopmentInspection{SourceDigest: source, SourceClean: true, SourceEvidenceKnown: true,
		Requirements:          []development.CapabilityID{"toolchain.go.v1-26-6", "toolchain.pnpm.v10-13-1"},
		GoCommandRequirements: &development.GoCommandRequirements{Version: 1, SourceDigest: source, MinimumVersions: []development.GoMinimum{{Manifest: "go.mod", Version: "1.26.6"}}}}
	command := projectDevelopmentBody{Argv: []string{"go", "test", "./...", "-count=1"}, TimeoutSeconds: 4200, RunnerProfile: tools.DevelopmentRunnerProfile}
	for name, mutate := range map[string]func(*projectDevelopmentBody){
		"Make":               func(c *projectDevelopmentBody) { c.Argv = []string{"make", "validate-all"} },
		"unknown":            func(c *projectDevelopmentBody) { c.Argv = []string{"custom", "check"} },
		"other Go argv":      func(c *projectDevelopmentBody) { c.Argv = []string{"go", "test", "./..."} },
		"cwd":                func(c *projectDevelopmentBody) { c.CWD = "subdir" },
		"stdin":              func(c *projectDevelopmentBody) { c.Stdin = "input" },
		"environment":        func(c *projectDevelopmentBody) { c.Environment = map[string]string{"CGO_ENABLED": "0"} },
		"timeout":            func(c *projectDevelopmentBody) { c.TimeoutSeconds = 4199 },
		"no explicit runner": func(c *projectDevelopmentBody) { c.RunnerProfile = "" },
		"other runner":       func(c *projectDevelopmentBody) { c.RunnerProfile = "unregistered" },
	} {
		t.Run(name, func(t *testing.T) {
			other := command
			mutate(&other)
			got, err := developmentCommandSourceRequirements(other, inspection)
			if err != nil || !reflect.DeepEqual(got, inspection.Requirements) {
				t.Fatalf("generic inference changed: %v %v", got, err)
			}
		})
	}
	inspection.GoCommandRequirements.SourceDigest = "sha256:" + strings.Repeat("b", 64)
	if _, err := developmentCommandSourceRequirements(command, inspection); err == nil {
		t.Fatal("foreign source metadata accepted")
	}
}

func TestProjectDevelopmentStoredGoObjectiveIsNotMigrated(t *testing.T) {
	server, edges, _ := developmentServer(t)
	newEvidence := false
	server.WithEdgeStore(&developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
		if op.Kind == edge.OperationProjectDevelopmentInspect {
			inspection := op.Result.DevelopmentInspection
			inspection.Requirements = []development.CapabilityID{"toolchain.go.v1-26-6", "toolchain.pnpm.v10-13-1"}
			if newEvidence {
				inspection.GoCommandRequirements = &development.GoCommandRequirements{Version: 1, SourceDigest: inspection.SourceDigest,
					MinimumVersions: []development.GoMinimum{{Manifest: "go.mod", Version: "1.26.6"}}}
			}
		}
		return op
	}})
	runner := &goRequirementsRunner{&developmentRunnerTestProvider{queue: server.workQueue, calibrated: true}}
	server.developmentRunner = runner
	view := runnerDevelopmentStart(t, server, "runner-go-old-objective-001")
	developmentRounds(t, server, 3)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	before, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
	if request.State != workqueue.DevelopmentRequestAwaitingReasoning {
		t.Fatal("fixture did not retain legacy blocked objective")
	}
	newEvidence = true
	developmentRounds(t, server, 3)
	after, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
	if !reflect.DeepEqual(before, after) || runner.posts != 0 {
		t.Fatal("new inference silently rewrote a stored objective")
	}
}

func (runner *goRequirementsRunner) Start(ctx context.Context, request tools.DevelopmentRunnerRequest) (tools.DevelopmentRunnerResult, error) {
	result, err := runner.developmentRunnerTestProvider.Start(ctx, request)
	attestation, _ := runner.ConfiguredTemplateAttestation()
	result.Capabilities = attestation.Capabilities.IDs()
	return result, err
}

func (runner *goRequirementsRunner) Reconcile(ctx context.Context, request tools.DevelopmentRunnerRequest) (tools.DevelopmentRunnerResult, error) {
	return runner.Start(ctx, request)
}

func TestProjectDevelopmentGoModuleMinimumUsesActualRunnerVersion(t *testing.T) {
	server, edges, _ := developmentServer(t)
	server.WithEdgeStore(&developmentScenarioEdge{developmentTestEdge: edges, observe: func(op edge.Operation) edge.Operation {
		if op.Kind == edge.OperationProjectDevelopmentInspect {
			op.Result.DevelopmentInspection.Requirements = []development.CapabilityID{"toolchain.go.v1-26-6", "toolchain.pnpm.v10-13-1"}
			body, _ := json.Marshal(op.Result.DevelopmentInspection)
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(body, &fields)
			fields["go_command_requirements"] = json.RawMessage(`{"version":1,"source_digest":"sha256:` + strings.Repeat("b", 64) + `","exact_requirements":[],"minimum_versions":[{"manifest":"go.mod","version":"1.26.6"}]}`)
			body, _ = json.Marshal(fields)
			if err := json.Unmarshal(body, op.Result.DevelopmentInspection); err != nil {
				t.Fatal(err)
			}
		}
		return op
	}})
	runner := &goRequirementsRunner{&developmentRunnerTestProvider{queue: server.workQueue, calibrated: true}}
	server.developmentRunner = runner
	view := runnerDevelopmentStart(t, server, "runner-go-minimum-001")
	developmentRounds(t, server, 3)
	request, _, _ := server.workQueue.DevelopmentRequest(view.RequestID)
	objective, _, _ := server.workQueue.DevelopmentObjective(request.ObjectiveID)
	if request.State != workqueue.DevelopmentRequestCompleted || runner.posts != 1 || edges.starts != 0 {
		t.Fatalf("minimum incorrectly blocks measured newer runner: request=%+v posts=%d", request, runner.posts)
	}
	if got := objective.Steps[0].Requirements; len(got) != 1 || got[0].ID != "toolchain.go.v1-26-8" {
		t.Fatalf("requirements must bind actual runner version only: %v", got)
	}
}
