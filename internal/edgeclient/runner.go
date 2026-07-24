package edgeclient

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

var (
	ErrKillSwitch           = errors.New("edge kill switch is active")
	ErrOfflineGraceExceeded = errors.New("edge offline grace exceeded")
)

const (
	DefaultOfflineGrace       = 10 * time.Minute
	DefaultReconnectInterval  = 5 * time.Second
	DefaultDeliveredRetention = 7 * 24 * time.Hour
	DefaultLeaseSafetyMargin  = 30 * time.Second
)

type EdgeTransport interface {
	Lease(context.Context, string, time.Duration) (*edge.Lease, error)
	Heartbeat(context.Context, string, string, time.Duration) (edge.HeartbeatStatus, error)
	Complete(context.Context, string, string, edge.TaskResult) (edge.Task, error)
}

type Executor interface {
	Execute(context.Context, edge.Task) edge.TaskResult
}

type Runner struct {
	Transport          EdgeTransport
	Journal            *Journal
	Executor           Executor
	Holder             string
	LeaseTTL           time.Duration
	HeartbeatInterval  time.Duration
	OfflineGrace       time.Duration
	ReconnectInterval  time.Duration
	DeliveredRetention time.Duration
	StopPath           string
}

func (r *Runner) RunOnce(ctx context.Context) (bool, error) {
	if r.killSwitchActive() {
		return false, ErrKillSwitch
	}
	if r.Transport == nil || r.Journal == nil || r.Executor == nil {
		return false, errors.New("edge runner is incomplete")
	}
	if pending, err := r.retryPending(ctx); pending || err != nil {
		return pending, err
	}
	if _, err := r.Journal.CleanupDelivered(r.deliveredRetention()); err != nil {
		return false, err
	}
	lease, err := r.Transport.Lease(ctx, r.Holder, r.LeaseTTL)
	if err != nil || lease == nil {
		return false, err
	}
	if !lease.LeaseExpiresAt.After(time.Now().UTC().Add(r.leaseSafetyMargin())) {
		return true, errors.New("edge lease authority is insufficient")
	}
	entry, err := r.Journal.BeginLease(lease.Task.IdempotencyKey, lease.Task.ID, lease.Attempt, lease.LeaseID)
	if err != nil {
		return true, err
	}
	if entry.State == JournalCompleted {
		return true, r.deliver(ctx, *lease, entry)
	}
	if !entry.New {
		result := edge.TaskResult{Outcome: edge.OutcomeFailed, Summary: "previous execution was interrupted; manual reconciliation required"}
		completed, err := r.Journal.FinishEntry(lease.Task.IdempotencyKey, lease.Task.ID, result)
		if err != nil {
			return true, err
		}
		return true, r.deliver(ctx, *lease, completed)
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
	} else if errors.Is(heartbeatErr, ErrOfflineGraceExceeded) {
		result = edge.TaskResult{Outcome: edge.OutcomeFailed, Summary: "offline grace exceeded; manual reconciliation required"}
	} else if heartbeatErr != nil {
		result = edge.TaskResult{Outcome: edge.OutcomeFailed, Summary: "heartbeat failed; execution stopped"}
	}
	completed, err := r.Journal.FinishEntry(lease.Task.IdempotencyKey, lease.Task.ID, result)
	if err != nil {
		return true, err
	}
	return true, r.deliver(ctx, *lease, completed)
}

func (r *Runner) heartbeatLoop(ctx context.Context, cancel context.CancelFunc, lease edge.Lease) error {
	heartbeatInterval := r.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = 10 * time.Second
	}
	offlineGrace := r.offlineGrace()
	reconnectInterval := r.ReconnectInterval
	if reconnectInterval <= 0 {
		reconnectInterval = DefaultReconnectInterval
	}
	if reconnectInterval > offlineGrace {
		reconnectInterval = offlineGrace
	}
	timer := time.NewTimer(heartbeatInterval)
	defer timer.Stop()
	var offlineSince time.Time
	authorityDeadline := lease.LeaseExpiresAt.UTC().Add(-r.leaseSafetyMargin())
	if !authorityDeadline.After(time.Now().UTC()) {
		cancel()
		return ErrOfflineGraceExceeded
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			if r.killSwitchActive() {
				cancel()
				return ErrKillSwitch
			}
			status, err := r.Transport.Heartbeat(ctx, lease.Task.ID, lease.LeaseID, r.LeaseTTL)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				now := time.Now().UTC()
				if offlineSince.IsZero() {
					offlineSince = now
				}
				deadline := offlineSince.Add(offlineGrace)
				if authorityDeadline.Before(deadline) {
					deadline = authorityDeadline
				}
				if !deadline.After(now) {
					cancel()
					return ErrOfflineGraceExceeded
				}
				delay := reconnectInterval
				if remaining := time.Until(deadline); remaining < delay {
					delay = remaining
				}
				if delay <= 0 {
					cancel()
					return ErrOfflineGraceExceeded
				}
				timer.Reset(delay)
				continue
			}
			offlineSince = time.Time{}
			authorityDeadline = status.LeaseExpiresAt.UTC().Add(-r.leaseSafetyMargin())
			if !authorityDeadline.After(time.Now().UTC()) {
				cancel()
				return ErrOfflineGraceExceeded
			}
			if status.CancelRequested {
				cancel()
				return errRemoteCancelled
			}
			timer.Reset(heartbeatInterval)
		}
	}
}

func (r *Runner) retryPending(ctx context.Context) (bool, error) {
	entries, err := r.Journal.PendingDeliveries(1)
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return false, nil
	}
	entry := entries[0]
	if _, err := r.Transport.Complete(ctx, entry.TaskID, entry.LeaseID, entry.Result); err != nil {
		return true, err
	}
	if err := r.Journal.MarkDelivered(entry.Key, entry.TaskID, entry.ResultID); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Runner) deliver(ctx context.Context, lease edge.Lease, entry JournalEntry) error {
	if entry.State != JournalCompleted || entry.ResultID == "" {
		return errors.New("journal result is not deliverable")
	}
	if _, err := r.Transport.Complete(ctx, lease.Task.ID, lease.LeaseID, entry.Result); err != nil {
		return err
	}
	return r.Journal.MarkDelivered(lease.Task.IdempotencyKey, lease.Task.ID, entry.ResultID)
}

func (r *Runner) offlineGrace() time.Duration {
	if r.OfflineGrace > 0 {
		return r.OfflineGrace
	}
	return DefaultOfflineGrace
}

func (r *Runner) deliveredRetention() time.Duration {
	if r.DeliveredRetention > 0 {
		return r.DeliveredRetention
	}
	return DefaultDeliveredRetention
}

func (r *Runner) leaseSafetyMargin() time.Duration {
	margin := DefaultLeaseSafetyMargin
	if r.LeaseTTL > 0 && r.LeaseTTL/10 < margin {
		margin = r.LeaseTTL / 10
	}
	if margin < time.Millisecond {
		return time.Millisecond
	}
	return margin
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
