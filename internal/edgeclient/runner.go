package edgeclient

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

var ErrKillSwitch = errors.New("edge kill switch is active")

type EdgeTransport interface {
	Lease(context.Context, string, time.Duration) (*edge.Lease, error)
	Heartbeat(context.Context, string, string, time.Duration) (edge.HeartbeatStatus, error)
	Complete(context.Context, string, string, edge.TaskResult) (edge.Task, error)
}

type Executor interface {
	Execute(context.Context, edge.Task) edge.TaskResult
}

type Runner struct {
	Transport         EdgeTransport
	Journal           *Journal
	Executor          Executor
	Holder            string
	LeaseTTL          time.Duration
	HeartbeatInterval time.Duration
	StopPath          string
}

func (r *Runner) RunOnce(ctx context.Context) (bool, error) {
	if r.killSwitchActive() {
		return false, ErrKillSwitch
	}
	if r.Transport == nil || r.Journal == nil || r.Executor == nil {
		return false, errors.New("edge runner is incomplete")
	}
	lease, err := r.Transport.Lease(ctx, r.Holder, r.LeaseTTL)
	if err != nil || lease == nil {
		return false, err
	}
	entry, err := r.Journal.Begin(lease.Task.IdempotencyKey, lease.Task.ID)
	if err != nil {
		return true, err
	}
	if entry.State == JournalCompleted {
		_, err := r.Transport.Complete(ctx, lease.Task.ID, lease.LeaseID, entry.Result)
		return true, err
	}
	if !entry.New {
		result := edge.TaskResult{Outcome: edge.OutcomeFailed, Summary: "previous execution was interrupted; manual reconciliation required"}
		if err := r.Journal.Finish(lease.Task.IdempotencyKey, lease.Task.ID, result); err != nil {
			return true, err
		}
		_, err := r.Transport.Complete(ctx, lease.Task.ID, lease.LeaseID, result)
		return true, err
	}

	executionContext, cancelExecution := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go func() {
		heartbeatDone <- r.heartbeatLoop(executionContext, cancelExecution, *lease)
	}()
	result := r.Executor.Execute(executionContext, lease.Task)
	cancelExecution()
	heartbeatErr := <-heartbeatDone
	if errors.Is(heartbeatErr, ErrKillSwitch) {
		result = edge.TaskResult{Outcome: edge.OutcomeCancelled, Summary: "local kill switch activated"}
	} else if errors.Is(heartbeatErr, errRemoteCancelled) {
		result = edge.TaskResult{Outcome: edge.OutcomeCancelled, Summary: "task cancelled by operator"}
	} else if heartbeatErr != nil {
		result = edge.TaskResult{Outcome: edge.OutcomeFailed, Summary: "heartbeat failed; execution stopped"}
	}
	if err := r.Journal.Finish(lease.Task.IdempotencyKey, lease.Task.ID, result); err != nil {
		return true, err
	}
	_, err = r.Transport.Complete(ctx, lease.Task.ID, lease.LeaseID, result)
	return true, err
}

func (r *Runner) heartbeatLoop(ctx context.Context, cancel context.CancelFunc, lease edge.Lease) error {
	interval := r.HeartbeatInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if r.killSwitchActive() {
				cancel()
				return ErrKillSwitch
			}
			status, err := r.Transport.Heartbeat(ctx, lease.Task.ID, lease.LeaseID, r.LeaseTTL)
			if err != nil {
				cancel()
				return err
			}
			if status.CancelRequested {
				cancel()
				return errRemoteCancelled
			}
		}
	}
}

func (r *Runner) killSwitchActive() bool {
	if r.StopPath == "" {
		return false
	}
	info, err := os.Lstat(r.StopPath)
	if err == nil {
		return !info.IsDir()
	}
	return !errors.Is(err, os.ErrNotExist)
}

var errRemoteCancelled = errors.New("edge task cancelled")
