package modelturn

import (
	"context"
	"errors"
)

var (
	ErrSamplingNotAnnounced = errors.New("MCP sampling was not explicitly announced by the current client session")
	ErrSamplingReserved     = errors.New("MCP sampling transport is reserved but not activated")
	ErrNilRendezvous        = errors.New("model-turn rendezvous is required")
)

// ModelTurnTransport is the only inference boundary the agent runtime may use.
// Implementations must not silently fall back to another model or provider.
type ModelTurnTransport interface {
	CreateTurn(context.Context, ModelRequest) (Turn, error)
	WaitResponse(context.Context, TurnID) (ModelResponse, error)
	Cancel(context.Context, TurnID) error
}

// TurnRendezvous is implemented by the durable store added in Step 3.
type TurnRendezvous interface {
	CreateTurn(context.Context, ModelRequest) (Turn, error)
	WaitResponse(context.Context, TurnID) (ModelResponse, error)
	Cancel(context.Context, TurnID) error
}

// PullRendezvousTransport delegates model turns to a pull-based rendezvous. It has
// no provider fallback and performs no model-network traffic itself.
type PullRendezvousTransport struct {
	rendezvous TurnRendezvous
}

func NewPullRendezvousTransport(rendezvous TurnRendezvous) (*PullRendezvousTransport, error) {
	if rendezvous == nil {
		return nil, ErrNilRendezvous
	}
	return &PullRendezvousTransport{rendezvous: rendezvous}, nil
}

func (t *PullRendezvousTransport) CreateTurn(ctx context.Context, request ModelRequest) (Turn, error) {
	return t.rendezvous.CreateTurn(ctx, request)
}

func (t *PullRendezvousTransport) WaitResponse(ctx context.Context, turnID TurnID) (ModelResponse, error) {
	return t.rendezvous.WaitResponse(ctx, turnID)
}

func (t *PullRendezvousTransport) Cancel(ctx context.Context, turnID TurnID) error {
	return t.rendezvous.Cancel(ctx, turnID)
}

// MCPSamplingTransport is intentionally reserved. Construction is denied unless
// the current session explicitly announced capabilities.sampling; even then its
// methods remain inactive until a bounded sampling driver is implemented.
type MCPSamplingTransport struct{}

func NewMCPSamplingTransport(samplingExplicitlyAnnounced bool) (*MCPSamplingTransport, error) {
	if !samplingExplicitlyAnnounced {
		return nil, ErrSamplingNotAnnounced
	}
	return &MCPSamplingTransport{}, nil
}

func (*MCPSamplingTransport) CreateTurn(context.Context, ModelRequest) (Turn, error) {
	return Turn{}, ErrSamplingReserved
}

func (*MCPSamplingTransport) WaitResponse(context.Context, TurnID) (ModelResponse, error) {
	return ModelResponse{}, ErrSamplingReserved
}

func (*MCPSamplingTransport) Cancel(context.Context, TurnID) error {
	return ErrSamplingReserved
}
