package modelturn

import (
	"context"
	"errors"
	"testing"
)

type fakeRendezvous struct {
	created   ModelRequest
	waited    TurnID
	cancelled TurnID
}

func (f *fakeRendezvous) CreateTurn(_ context.Context, request ModelRequest) (Turn, error) {
	f.created = request
	return Turn{RuntimeID: request.RuntimeID, ID: "turn-1", Sequence: request.Sequence}, nil
}

func (f *fakeRendezvous) WaitResponse(_ context.Context, turnID TurnID) (ModelResponse, error) {
	f.waited = turnID
	return ModelResponse{TurnID: turnID}, nil
}

func (f *fakeRendezvous) Cancel(_ context.Context, turnID TurnID) error {
	f.cancelled = turnID
	return nil
}

func TestPullRendezvousTransportDelegatesWithoutFallback(t *testing.T) {
	backend := &fakeRendezvous{}
	transport, err := NewPullRendezvousTransport(backend)
	if err != nil {
		t.Fatal(err)
	}
	request := ModelRequest{RuntimeID: "runtime-1", Sequence: 3, Payload: []byte(`{"messages":[]}`)}
	turn, err := transport.CreateTurn(context.Background(), request)
	if err != nil || turn.ID != "turn-1" || backend.created.RuntimeID != request.RuntimeID {
		t.Fatalf("turn=%+v backend=%+v err=%v", turn, backend.created, err)
	}
	if _, err := transport.WaitResponse(context.Background(), turn.ID); err != nil || backend.waited != turn.ID {
		t.Fatalf("waited=%q err=%v", backend.waited, err)
	}
	if err := transport.Cancel(context.Background(), turn.ID); err != nil || backend.cancelled != turn.ID {
		t.Fatalf("cancelled=%q err=%v", backend.cancelled, err)
	}
}

func TestPullRendezvousTransportRejectsNilBackend(t *testing.T) {
	if _, err := NewPullRendezvousTransport(nil); !errors.Is(err, ErrNilRendezvous) {
		t.Fatalf("error=%v", err)
	}
}

func TestMCPSamplingTransportRequiresExplicitAnnouncementAndRemainsReserved(t *testing.T) {
	if _, err := NewMCPSamplingTransport(false); !errors.Is(err, ErrSamplingNotAnnounced) {
		t.Fatalf("missing announcement error=%v", err)
	}
	transport, err := NewMCPSamplingTransport(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.CreateTurn(context.Background(), ModelRequest{}); !errors.Is(err, ErrSamplingReserved) {
		t.Fatalf("create error=%v", err)
	}
	if _, err := transport.WaitResponse(context.Background(), "turn"); !errors.Is(err, ErrSamplingReserved) {
		t.Fatalf("wait error=%v", err)
	}
	if err := transport.Cancel(context.Background(), "turn"); !errors.Is(err, ErrSamplingReserved) {
		t.Fatalf("cancel error=%v", err)
	}
}

var _ ModelTurnTransport = (*PullRendezvousTransport)(nil)
var _ ModelTurnTransport = (*MCPSamplingTransport)(nil)
