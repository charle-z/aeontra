package modelturn

import (
	"context"
	"time"
)

// WaitLeaseNextRuntime blocks without holding a SQLite transaction. Store
// notifications wake it promptly; the one-second fallback handles processes that
// use separate Store instances and therefore cannot share an in-memory signal.
func (s *Store) WaitLeaseNextRuntime(ctx context.Context, deviceID string) (Runtime, bool, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Runtime{}, false, err
		}
		wake := s.waitChannel()
		runtime, found, err := s.LeaseNextRuntime(ctx, deviceID)
		if err != nil || found {
			return runtime, found, err
		}
		select {
		case <-ctx.Done():
			return Runtime{}, false, ctx.Err()
		case <-wake:
		case <-time.After(time.Second):
		}
	}
}
