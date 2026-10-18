package frontdoorcoordinator

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	testRequestID  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testRequestID2 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type fakePlatform struct {
	topology        Topology
	calls           []string
	status          []Status
	publishCalls    int
	publishFailures int
	publishError    error
	failCall        string
	failedCall      bool
	failCounts      map[string]int
}

func (f *fakePlatform) Topology(context.Context) (Topology, error) {
	if err := f.failOnce("topology"); err != nil {
		return Topology{}, err
	}
	return f.topology, nil
}

func (f *fakePlatform) failOnce(call string) error {
	if remaining := f.failCounts[call]; remaining > 0 {
		f.failCounts[call] = remaining - 1
		return errors.New("injected platform failure")
	}
	if f.failCall == call && !f.failedCall {
		f.failedCall = true
		return errors.New("injected platform failure")
	}
	return nil
}

func (f *fakePlatform) SetBackendDomains(_ context.Context, domains string) (string, error) {
	call := "backend:" + domains
	f.calls = append(f.calls, call)
	if err := f.failOnce(call); err != nil {
		return "", err
	}
	f.topology.BackendDomains = domains
	return "dep-backend", nil
}

func (f *fakePlatform) ConfigureFront(_ context.Context, domain, backend string) (string, error) {
	call := "front:" + domain + "->" + backend
	f.calls = append(f.calls, call)
	if err := f.failOnce(call); err != nil {
		return "", err
	}
	f.topology.FrontDomain = domain
	f.topology.FrontBackendURL = backend
	return "dep-front", nil
}

func (f *fakePlatform) ProbeBackend(_ context.Context, origin string) error {
	call := "probe-backend:" + origin
	f.calls = append(f.calls, call)
	return f.failOnce(call)
}

func (f *fakePlatform) ProbeFront(_ context.Context, origin string) error {
	call := "probe-front:" + origin
	f.calls = append(f.calls, call)
	return f.failOnce(call)
}

func (f *fakePlatform) PublishStatus(_ context.Context, status Status) error {
	f.publishCalls++
	if f.publishFailures > 0 {
		f.publishFailures--
		if f.publishError != nil {
			return f.publishError
		}
		return errors.New("temporary publish failure")
	}
	f.status = append(f.status, status)
	return nil
}

func TestRunnerCompletesCutover(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{topology: Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin}}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID}
	status, err := runner.Run(context.Background(), TargetCutover)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateSucceeded || status.Phase != PhaseComplete || status.Topology.FrontDomain != FrontPublicOrigin || status.Topology.BackendDomains != BackendOrigin {
		t.Fatalf("status=%+v", status)
	}
	want := []string{
		"backend:" + FrontPublicOrigin + "," + BackendOrigin,
		"probe-backend:" + BackendOrigin,
		"front:" + FrontTemporaryOrigin + "->" + BackendOrigin,
		"probe-front:" + FrontTemporaryOrigin,
		"backend:" + BackendOrigin,
		"probe-backend:" + BackendOrigin,
		"front:" + FrontPublicOrigin + "->" + BackendOrigin,
		"probe-front:" + FrontPublicOrigin,
	}
	if !reflect.DeepEqual(platform.calls, want) {
		t.Fatalf("calls=%q\nwant=%q", platform.calls, want)
	}
}

func TestRunnerResumesCutoverAfterFrontSwitch(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = journal.Advance(Status{RequestID: testRequestID, Target: TargetCutover, State: StateRunning, Phase: PhaseReleasePublicBackend})
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{topology: Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: BackendOrigin, BackendDomains: FrontPublicOrigin + "," + BackendOrigin}}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID}
	status, err := runner.Run(context.Background(), TargetCutover)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateSucceeded {
		t.Fatalf("status=%+v", status)
	}
	for _, call := range platform.calls {
		if call == "front:"+FrontTemporaryOrigin+"->"+BackendOrigin {
			t.Fatalf("front backend switch was repeated after durable phase: %q", platform.calls)
		}
	}
}

func TestRunnerCompletesRollback(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{topology: Topology{FrontDomain: FrontPublicOrigin, FrontBackendURL: BackendOrigin, BackendDomains: BackendOrigin}}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID}
	status, err := runner.Run(context.Background(), TargetRollback)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateSucceeded || status.Topology.FrontDomain != FrontTemporaryOrigin || status.Topology.BackendDomains != FrontPublicOrigin {
		t.Fatalf("status=%+v", status)
	}
}

func TestRunnerRetriesStatusPublication(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{
		topology:        Topology{FrontDomain: FrontPublicOrigin, FrontBackendURL: BackendOrigin, BackendDomains: BackendOrigin},
		publishFailures: 2,
	}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID, PublishAttempts: 3, PublishBackoff: time.Millisecond}
	status, err := runner.Run(context.Background(), TargetCutover)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateSucceeded || platform.publishCalls != 4 {
		t.Fatalf("status=%+v publish_calls=%d", status, platform.publishCalls)
	}
}

func TestRunnerReturnsRestartableErrorWhenStatusPublicationIsExhausted(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{
		topology:        Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin},
		publishFailures: 3,
		publishError:    ErrCoolifyResponseHTTP,
	}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID, PublishAttempts: 2, PublishBackoff: time.Millisecond}
	_, err = runner.Run(context.Background(), TargetCutover)
	if !errors.Is(err, ErrStatusPublish) || !errors.Is(err, ErrCoolifyResponseHTTP) || platform.publishCalls != 2 {
		t.Fatalf("err=%v publish_calls=%d", err, platform.publishCalls)
	}
}

func TestRunnerDoesNotRetrySameFailedRequestAfterRestart(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = journal.Advance(Status{
		RequestID: testRequestID,
		Target:    TargetCutover,
		State:     StateFailed,
		Phase:     PhaseSwitchFrontBackend,
		Topology:  Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin + "," + BackendOrigin},
		Reason:    "switch-front-backend_failed",
	})
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{topology: Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin + "," + BackendOrigin}}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID, PublishBackoff: time.Millisecond}
	status, err := runner.Run(context.Background(), TargetCutover)
	if err == nil || !strings.Contains(err.Error(), "already failed") {
		t.Fatalf("same failed request resumed: status=%+v err=%v", status, err)
	}
	if len(platform.calls) != 0 || platform.publishCalls != 1 || status.RequestID != testRequestID || status.State != StateFailed {
		t.Fatalf("status=%+v calls=%v publish_calls=%d", status, platform.calls, platform.publishCalls)
	}
}

func TestRunnerAllowsNewReviewedRequestAfterFailure(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = journal.Advance(Status{
		RequestID: testRequestID,
		Target:    TargetCutover,
		State:     StateFailed,
		Phase:     PhaseAddBackendOrigin,
		Topology:  Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin},
		Reason:    "add-backend-origin_failed",
	})
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{topology: Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin}}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID2}
	status, err := runner.Run(context.Background(), TargetCutover)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateSucceeded || status.RequestID != testRequestID2 || len(platform.calls) == 0 {
		t.Fatalf("status=%+v calls=%v", status, platform.calls)
	}
}

func TestRunnerRepublishesSameSucceededRequestWithoutEffects(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = journal.Advance(Status{
		RequestID: testRequestID,
		Target:    TargetCutover,
		State:     StateSucceeded,
		Phase:     PhaseComplete,
		Topology:  Topology{FrontDomain: FrontPublicOrigin, FrontBackendURL: BackendOrigin, BackendDomains: BackendOrigin},
	})
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{topology: Topology{FrontDomain: FrontPublicOrigin, FrontBackendURL: BackendOrigin, BackendDomains: BackendOrigin}}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID}
	status, err := runner.Run(context.Background(), TargetCutover)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateSucceeded || len(platform.calls) != 0 || platform.publishCalls != 1 {
		t.Fatalf("status=%+v calls=%v publish_calls=%d", status, platform.calls, platform.publishCalls)
	}
}

func TestRunnerRejectsDifferentActiveRequest(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = journal.Advance(Status{
		RequestID: testRequestID,
		Target:    TargetCutover,
		State:     StateRunning,
		Phase:     PhaseAddBackendOrigin,
		Topology:  Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin},
	})
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{topology: Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin}}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID2}
	status, err := runner.Run(context.Background(), TargetCutover)
	if err == nil || !strings.Contains(err.Error(), "different front-door coordinator request") {
		t.Fatalf("different active request was replaced: status=%+v err=%v", status, err)
	}
	if len(platform.calls) != 0 || platform.publishCalls != 0 || status.RequestID != testRequestID {
		t.Fatalf("status=%+v calls=%v publish_calls=%d", status, platform.calls, platform.publishCalls)
	}
}

func TestRunnerPreservesActiveRequestWhenInterrupted(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	before, err := journal.Advance(Status{
		RequestID: testRequestID,
		Target:    TargetCutover,
		State:     StateRunning,
		Phase:     PhaseSwitchFrontBackend,
		Topology:  Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin + "," + BackendOrigin},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	platform := &fakePlatform{topology: before.Topology}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID}
	status, err := runner.Run(ctx, TargetCutover)
	if !errors.Is(err, ErrTransitionInterrupted) {
		t.Fatalf("interruption became terminal: status=%+v err=%v", status, err)
	}
	after, readErr := journal.Read()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if after.Revision != before.Revision || after.State != StateRunning || after.RequestID != testRequestID || len(platform.calls) != 0 || platform.publishCalls != 0 {
		t.Fatalf("before=%+v after=%+v calls=%v publish_calls=%d", before, after, platform.calls, platform.publishCalls)
	}
}

func TestRunnerCompensatesFailedCutoverToDirectBackend(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	failCall := "front:" + FrontPublicOrigin + "->" + BackendOrigin
	platform := &fakePlatform{
		topology: Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin},
		failCall: failCall,
	}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID}
	status, err := runner.Run(context.Background(), TargetCutover)
	if err == nil || !strings.Contains(err.Error(), "injected platform failure") {
		t.Fatalf("cutover failure was hidden: status=%+v err=%v", status, err)
	}
	if status.State != StateFailed || status.RecoveryTarget != TargetRollback || status.Reason != "assign-public-front_failed_compensated" {
		t.Fatalf("status=%+v", status)
	}
	want := Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin}
	if status.Topology != want || platform.topology != want || !platform.failedCall {
		t.Fatalf("status_topology=%+v platform_topology=%+v failed=%t", status.Topology, platform.topology, platform.failedCall)
	}
	seenFailure := false
	seenRestore := false
	for _, call := range platform.calls {
		if call == failCall {
			seenFailure = true
		}
		if call == "backend:"+BackendOrigin+","+FrontPublicOrigin {
			seenRestore = true
		}
	}
	if !seenFailure || !seenRestore {
		t.Fatalf("calls=%v", platform.calls)
	}
}

func TestRunnerCompensatesFailedRollbackToFrontDoor(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	failCall := "backend:" + BackendOrigin + "," + FrontPublicOrigin
	platform := &fakePlatform{
		topology: Topology{FrontDomain: FrontPublicOrigin, FrontBackendURL: BackendOrigin, BackendDomains: BackendOrigin},
		failCall: failCall,
	}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID}
	status, err := runner.Run(context.Background(), TargetRollback)
	if err == nil || !strings.Contains(err.Error(), "injected platform failure") {
		t.Fatalf("rollback failure was hidden: status=%+v err=%v", status, err)
	}
	if status.State != StateFailed || status.RecoveryTarget != TargetCutover || status.Reason != "restore-public-backend_failed_compensated" {
		t.Fatalf("status=%+v", status)
	}
	want := Topology{FrontDomain: FrontPublicOrigin, FrontBackendURL: BackendOrigin, BackendDomains: BackendOrigin}
	if status.Topology != want || platform.topology != want || !platform.failedCall {
		t.Fatalf("status_topology=%+v platform_topology=%+v failed=%t", status.Topology, platform.topology, platform.failedCall)
	}
}

func TestRunnerResumesDurableCompensationAfterRestart(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	intermediate := Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: BackendOrigin, BackendDomains: BackendOrigin}
	_, err = journal.Advance(Status{
		RequestID: testRequestID, Target: TargetCutover, RecoveryTarget: TargetRollback,
		State: StateCompensating, Phase: PhaseRestorePublicBackend, Topology: intermediate,
		Reason: "assign-public-front_failed",
	})
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{topology: intermediate}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID}
	status, err := runner.Run(context.Background(), TargetCutover)
	if err == nil || !strings.Contains(err.Error(), "previously failed") {
		t.Fatalf("compensation result status=%+v err=%v", status, err)
	}
	want := Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin}
	if status.State != StateFailed || status.Reason != "assign-public-front_failed_compensated" || status.Topology != want || platform.topology != want {
		t.Fatalf("status=%+v platform=%+v", status, platform.topology)
	}
}

func TestRunnerRetriesTopologyReadBeforeFirstEffect(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	platform := &fakePlatform{
		topology:   Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin},
		failCounts: map[string]int{"topology": 1},
	}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID, CompensationBackoff: time.Millisecond}
	status, err := runner.Run(context.Background(), TargetCutover)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateSucceeded || status.RecoveryTarget != "" || len(platform.calls) == 0 {
		t.Fatalf("status=%+v calls=%v", status, platform.calls)
	}
}

func TestRunnerExhaustsTopologyReadsBeforeFirstEffectWithoutCompensation(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	initial := Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin}
	platform := &fakePlatform{topology: initial, failCounts: map[string]int{"topology": 12}}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID, CompensationBackoff: time.Millisecond}
	status, err := runner.Run(context.Background(), TargetCutover)
	if err == nil || !strings.Contains(err.Error(), "finite phase budget") {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if status.State != StateFailed || status.Phase != PhaseNone || status.RecoveryTarget != "" || status.Reason != "topology_read_budget_exhausted" {
		t.Fatalf("status=%+v", status)
	}
	if platform.topology != initial || len(platform.calls) != 0 {
		t.Fatalf("topology=%+v calls=%v", platform.topology, platform.calls)
	}
}

func TestRunnerRetriesTransientCompensationEffectFromObservedTopology(t *testing.T) {
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publicProbe := "probe-front:" + FrontPublicOrigin
	restorePublicBackend := "backend:" + BackendOrigin + "," + FrontPublicOrigin
	platform := &fakePlatform{
		topology:   Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin},
		failCounts: map[string]int{publicProbe: 1, restorePublicBackend: 1},
	}
	runner := Runner{Platform: platform, Journal: journal, RequestID: testRequestID, CompensationBackoff: time.Millisecond}
	status, err := runner.Run(context.Background(), TargetCutover)
	if err == nil || !strings.Contains(err.Error(), "injected platform failure") {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	want := Topology{FrontDomain: FrontTemporaryOrigin, FrontBackendURL: FrontPublicOrigin, BackendDomains: FrontPublicOrigin}
	if status.State != StateFailed || status.Reason != "assign-public-front_failed_compensated" || status.Topology != want || platform.topology != want {
		t.Fatalf("status=%+v topology=%+v", status, platform.topology)
	}
	restoreCount := 0
	for _, call := range platform.calls {
		if call == restorePublicBackend {
			restoreCount++
		}
	}
	if restoreCount != 2 {
		t.Fatalf("compensation retries=%d calls=%v", restoreCount, platform.calls)
	}
}
