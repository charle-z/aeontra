package catalogrollout

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func identity(commit, hash string) Identity {
	return Identity{Commit: commit, ProtocolVersion: "2024-11-05", ToolCount: 137, CatalogHash: hash}
}

func request() Request {
	return Request{
		RequestID: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Previous: identity(strings.Repeat("a", 40), "sha256:"+strings.Repeat("1", 64)),
		Candidate: identity(strings.Repeat("b", 40), "sha256:"+strings.Repeat("2", 64)),
	}
}

type fakePlatform struct {
	observation Observation
	calls       []string
	fail        map[string]error
	interrupt   string
}

func (f *fakePlatform) Observe(context.Context) (Observation, error) {
	f.calls = append(f.calls, "observe")
	if err := f.fail["observe"]; err != nil {
		return Observation{}, err
	}
	return f.observation, nil
}

func (f *fakePlatform) PrepareFront(_ context.Context, previous, candidate Identity) (string, error) {
	f.calls = append(f.calls, "prepare-front")
	if err := f.fail["prepare-front"]; err != nil {
		return "", err
	}
	f.observation.Front = FrontContract{Primary: candidate.CatalogHash, Transition: previous.CatalogHash}
	return "front-prepare", nil
}

func (f *fakePlatform) DeployBackend(_ context.Context, candidate Identity) (string, error) {
	f.calls = append(f.calls, "deploy-backend")
	f.observation.Backend = candidate
	if f.interrupt == "deploy-backend" {
		f.interrupt = ""
		return "backend-deploy", context.Canceled
	}
	if err := f.fail["deploy-backend"]; err != nil {
		return "backend-deploy", err
	}
	return "backend-deploy", nil
}

func (f *fakePlatform) VerifyBackend(context.Context, Identity) error {
	f.calls = append(f.calls, "verify-backend")
	return f.fail["verify-backend"]
}

func (f *fakePlatform) FinalizeFront(_ context.Context, candidate Identity) (string, error) {
	f.calls = append(f.calls, "finalize-front")
	if err := f.fail["finalize-front"]; err != nil {
		return "", err
	}
	f.observation.Front = FrontContract{Primary: candidate.CatalogHash}
	return "front-finalize", nil
}

func (f *fakePlatform) RollbackBackend(_ context.Context, previous Identity) (string, error) {
	f.calls = append(f.calls, "rollback-backend")
	if err := f.fail["rollback-backend"]; err != nil {
		return "", err
	}
	f.observation.Backend = previous
	return "backend-rollback", nil
}

func (f *fakePlatform) RollbackFront(_ context.Context, previous Identity) (string, error) {
	f.calls = append(f.calls, "rollback-front")
	if err := f.fail["rollback-front"]; err != nil {
		return "", err
	}
	f.observation.Front = FrontContract{Primary: previous.CatalogHash}
	return "front-rollback", nil
}

func (f *fakePlatform) PublishStatus(context.Context, Status) error {
	return f.fail["publish"]
}

func newRunner(t *testing.T, platform Platform) Runner {
	t.Helper()
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Runner{Platform: platform, Journal: journal}
}

func TestRunnerCatalogUnchangedDeploysWithoutFrontDoorMutation(t *testing.T) {
	req := request()
	req.Candidate.CatalogHash = req.Previous.CatalogHash
	platform := &fakePlatform{observation: Observation{Backend: req.Previous, Front: FrontContract{Primary: req.Previous.CatalogHash}}, fail: map[string]error{}}
	status, err := newRunner(t, platform).Run(context.Background(), req)
	if err != nil || status.State != StateSucceeded {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if got := strings.Join(platform.calls, ","); strings.Contains(got, "prepare-front") || strings.Contains(got, "finalize-front") || got != "observe,deploy-backend,observe,verify-backend,observe" {
		t.Fatalf("calls=%s", got)
	}
}

func TestRunnerChangedCatalogOrdersTransitionBeforeBackendAndRetiresOld(t *testing.T) {
	req := request()
	platform := &fakePlatform{observation: Observation{Backend: req.Previous, Front: FrontContract{Primary: req.Previous.CatalogHash}}, fail: map[string]error{}}
	status, err := newRunner(t, platform).Run(context.Background(), req)
	if err != nil || status.State != StateSucceeded || status.Phase != PhaseComplete {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	want := "observe,prepare-front,observe,deploy-backend,observe,verify-backend,observe,finalize-front,observe"
	if got := strings.Join(platform.calls, ","); got != want {
		t.Fatalf("calls=%s want=%s", got, want)
	}
	if platform.observation.Front.Primary != req.Candidate.CatalogHash || platform.observation.Front.Transition != "" {
		t.Fatalf("front=%+v", platform.observation.Front)
	}
}

func TestRunnerRejectsThirdCatalogAndInvalidIdentity(t *testing.T) {
	req := request()
	platform := &fakePlatform{observation: Observation{
		Backend: req.Previous,
		Front: FrontContract{Primary: req.Previous.CatalogHash, Transition: "sha256:" + strings.Repeat("3", 64)},
	}, fail: map[string]error{}}
	if _, err := newRunner(t, platform).Run(context.Background(), req); err == nil || !strings.Contains(err.Error(), "third catalog") {
		t.Fatalf("expected third catalog rejection, got %v", err)
	}
	req.Candidate.ProtocolVersion = "wrong"
	if _, err := newRunner(t, platform).Run(context.Background(), req); err == nil {
		t.Fatal("invalid protocol accepted")
	}
}

func TestRunnerResumesAfterInterruptionWithoutDuplicateDeployment(t *testing.T) {
	req := request()
	platform := &fakePlatform{observation: Observation{Backend: req.Previous, Front: FrontContract{Primary: req.Previous.CatalogHash}}, fail: map[string]error{}, interrupt: "deploy-backend"}
	runner := newRunner(t, platform)
	if _, err := runner.Run(context.Background(), req); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("expected interruption, got %v", err)
	}
	status, err := runner.Run(context.Background(), req)
	if err != nil || status.State != StateSucceeded {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	count := 0
	for _, call := range platform.calls {
		if call == "deploy-backend" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("backend deployments=%d calls=%v", count, platform.calls)
	}
}

func TestRunnerFailureBeforeBackendChangeRestoresPreviousFront(t *testing.T) {
	req := request()
	platform := &fakePlatform{observation: Observation{Backend: req.Previous, Front: FrontContract{Primary: req.Previous.CatalogHash}}, fail: map[string]error{"deploy-backend": errors.New("build failed")}}
	status, err := newRunner(t, platform).Run(context.Background(), req)
	if err == nil || status.State != StateFailed || platform.observation.Backend != req.Previous || platform.observation.Front.Primary != req.Previous.CatalogHash || platform.observation.Front.Transition != "" {
		t.Fatalf("status=%+v observation=%+v err=%v", status, platform.observation, err)
	}
	if got := strings.Join(platform.calls, ","); !strings.Contains(got, "rollback-front") || strings.Contains(got, "rollback-backend") {
		t.Fatalf("calls=%s", got)
	}
}

func TestRunnerFailureAfterBackendChangeRollsBackCompatibleBackendAndFront(t *testing.T) {
	req := request()
	platform := &fakePlatform{observation: Observation{Backend: req.Previous, Front: FrontContract{Primary: req.Previous.CatalogHash}}, fail: map[string]error{"verify-backend": errors.New("MCP smoke failed")}}
	status, err := newRunner(t, platform).Run(context.Background(), req)
	if err == nil || status.State != StateFailed || platform.observation.Backend != req.Previous || platform.observation.Front.Primary != req.Previous.CatalogHash || platform.observation.Front.Transition != "" {
		t.Fatalf("status=%+v observation=%+v err=%v", status, platform.observation, err)
	}
	got := strings.Join(platform.calls, ",")
	for _, part := range []string{"verify-backend", "rollback-backend", "rollback-front"} {
		if !strings.Contains(got, part) {
			t.Fatalf("calls=%s missing=%s", got, part)
		}
	}
}

func TestRunnerRejectsDifferentConcurrentRequest(t *testing.T) {
	req := request()
	platform := &fakePlatform{observation: Observation{Backend: req.Previous, Front: FrontContract{Primary: req.Previous.CatalogHash}}, fail: map[string]error{}, interrupt: "deploy-backend"}
	runner := newRunner(t, platform)
	if _, err := runner.Run(context.Background(), req); !errors.Is(err, ErrInterrupted) {
		t.Fatal(err)
	}
	other := req
	other.RequestID = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if _, err := runner.Run(context.Background(), other); err == nil || !strings.Contains(err.Error(), "different rollout") {
		t.Fatalf("different active request accepted: %v", err)
	}
}
