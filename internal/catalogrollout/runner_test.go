package catalogrollout

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func ti(c, h string) Identity {
	return Identity{Commit: c, ProtocolVersion: "2024-11-05", ToolCount: 137, CatalogHash: h}
}

func tr() Request {
	return Request{
		RequestID: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Previous:  ti(strings.Repeat("a", 40), "sha256:"+strings.Repeat("1", 64)),
		Candidate: ti(strings.Repeat("b", 40), "sha256:"+strings.Repeat("2", 64)),
	}
}

type fp struct {
	o         Observation
	c         []string
	f         map[string]error
	interrupt bool
}

func (p *fp) Observe(context.Context) (Observation, error) {
	p.c = append(p.c, "observe")
	return p.o, p.f["observe"]
}

func (p *fp) PrepareFront(_ context.Context, a, b Identity) (string, error) {
	p.c = append(p.c, "prepare-front")
	if err := p.f["prepare-front"]; err != nil {
		return "", err
	}
	p.o.Front = FrontContract{Primary: b.CatalogHash, Transition: a.CatalogHash}
	return "front-prepare", nil
}

func (p *fp) DeployBackend(_ context.Context, b Identity) (string, error) {
	p.c = append(p.c, "deploy-backend")
	if err := p.f["deploy-backend"]; err != nil {
		return "backend-deploy", err
	}
	p.o.Backend = b
	if p.interrupt {
		p.interrupt = false
		return "backend-deploy", context.Canceled
	}
	return "backend-deploy", nil
}

func (p *fp) VerifyBackend(context.Context, Identity) error {
	p.c = append(p.c, "verify-backend")
	return p.f["verify-backend"]
}

func (p *fp) FinalizeFront(_ context.Context, b Identity) (string, error) {
	p.c = append(p.c, "finalize-front")
	if err := p.f["finalize-front"]; err != nil {
		return "", err
	}
	p.o.Front = FrontContract{Primary: b.CatalogHash}
	return "front-finalize", nil
}

func (p *fp) RollbackBackend(_ context.Context, a Identity) (string, error) {
	p.c = append(p.c, "rollback-backend")
	if err := p.f["rollback-backend"]; err != nil {
		return "", err
	}
	p.o.Backend = a
	return "backend-rollback", nil
}

func (p *fp) RollbackFront(_ context.Context, a Identity) (string, error) {
	p.c = append(p.c, "rollback-front")
	if err := p.f["rollback-front"]; err != nil {
		return "", err
	}
	p.o.Front = FrontContract{Primary: a.CatalogHash}
	return "front-rollback", nil
}

func (p *fp) PublishStatus(context.Context, Status) error { return p.f["publish"] }

func rr(t *testing.T, p Platform) Runner {
	t.Helper()
	journal, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return Runner{Platform: p, Journal: journal}
}

func TestUnchanged(t *testing.T) {
	request := tr()
	request.Candidate.CatalogHash = request.Previous.CatalogHash
	platform := &fp{o: Observation{Backend: request.Previous, Front: FrontContract{Primary: request.Previous.CatalogHash}}, f: map[string]error{}}
	status, err := rr(t, platform).Run(context.Background(), request)
	if err != nil || status.State != StateSucceeded {
		t.Fatalf("%+v %v", status, err)
	}
	if got := strings.Join(platform.c, ","); got != "observe,deploy-backend,observe,verify-backend,observe" {
		t.Fatal(got)
	}
}

func TestChangedOrder(t *testing.T) {
	request := tr()
	platform := &fp{o: Observation{Backend: request.Previous, Front: FrontContract{Primary: request.Previous.CatalogHash}}, f: map[string]error{}}
	status, err := rr(t, platform).Run(context.Background(), request)
	if err != nil || status.State != StateSucceeded || status.Phase != PhaseComplete {
		t.Fatalf("%+v %v", status, err)
	}
	want := "observe,prepare-front,observe,deploy-backend,observe,verify-backend,observe,finalize-front,observe"
	if got := strings.Join(platform.c, ","); got != want {
		t.Fatalf("%s", got)
	}
	if platform.o.Front != (FrontContract{Primary: request.Candidate.CatalogHash}) {
		t.Fatalf("%+v", platform.o.Front)
	}
}

func TestRejectThirdAndProtocol(t *testing.T) {
	request := tr()
	platform := &fp{o: Observation{Backend: request.Previous, Front: FrontContract{Primary: request.Previous.CatalogHash, Transition: "sha256:" + strings.Repeat("3", 64)}}, f: map[string]error{}}
	if _, err := rr(t, platform).Run(context.Background(), request); err == nil || !strings.Contains(err.Error(), "third catalog") {
		t.Fatalf("%v", err)
	}
	request.Candidate.ProtocolVersion = "bad"
	if _, err := rr(t, platform).Run(context.Background(), request); err == nil {
		t.Fatal("invalid accepted")
	}
}

func TestResumeNoDuplicate(t *testing.T) {
	request := tr()
	platform := &fp{o: Observation{Backend: request.Previous, Front: FrontContract{Primary: request.Previous.CatalogHash}}, f: map[string]error{}, interrupt: true}
	runner := rr(t, platform)
	if _, err := runner.Run(context.Background(), request); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("%v", err)
	}
	status, err := runner.Run(context.Background(), request)
	if err != nil || status.State != StateSucceeded {
		t.Fatalf("%+v %v", status, err)
	}
	count := 0
	for _, call := range platform.c {
		if call == "deploy-backend" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d %v", count, platform.c)
	}
}

func TestFailureBeforeSwitch(t *testing.T) {
	request := tr()
	platform := &fp{o: Observation{Backend: request.Previous, Front: FrontContract{Primary: request.Previous.CatalogHash}}, f: map[string]error{"deploy-backend": errors.New("build")}}
	status, err := rr(t, platform).Run(context.Background(), request)
	if err == nil || status.State != StateFailed || platform.o.Backend != request.Previous || platform.o.Front != (FrontContract{Primary: request.Previous.CatalogHash}) {
		t.Fatalf("%+v %+v %v", status, platform.o, err)
	}
	calls := strings.Join(platform.c, ",")
	if !strings.Contains(calls, "rollback-front") || strings.Contains(calls, "rollback-backend") {
		t.Fatal(calls)
	}
}

func TestFailureAfterSwitch(t *testing.T) {
	request := tr()
	platform := &fp{o: Observation{Backend: request.Previous, Front: FrontContract{Primary: request.Previous.CatalogHash}}, f: map[string]error{"verify-backend": errors.New("smoke")}}
	status, err := rr(t, platform).Run(context.Background(), request)
	if err == nil || status.State != StateFailed || platform.o.Backend != request.Previous || platform.o.Front != (FrontContract{Primary: request.Previous.CatalogHash}) {
		t.Fatalf("%+v %+v %v", status, platform.o, err)
	}
	calls := strings.Join(platform.c, ",")
	for _, expected := range []string{"rollback-backend", "rollback-front"} {
		if !strings.Contains(calls, expected) {
			t.Fatal(calls)
		}
	}
}

func TestDifferentActive(t *testing.T) {
	request := tr()
	platform := &fp{o: Observation{Backend: request.Previous, Front: FrontContract{Primary: request.Previous.CatalogHash}}, f: map[string]error{}, interrupt: true}
	runner := rr(t, platform)
	if _, err := runner.Run(context.Background(), request); !errors.Is(err, ErrInterrupted) {
		t.Fatal(err)
	}
	request.RequestID = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if _, err := runner.Run(context.Background(), request); err == nil || !strings.Contains(err.Error(), "different rollout") {
		t.Fatalf("%v", err)
	}
}
