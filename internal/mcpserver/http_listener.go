package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// serveHTTPListener owns the bounded replacement sequence: readiness drops first,
// listeners stop accepting traffic, active requests receive a finite completion
// window, and process-local session metadata is invalidated only after that window.
func serveHTTPListener(
	ctx context.Context,
	srv *http.Server,
	listener net.Listener,
	lifecycle *httpServerLifecycle,
	shutdownTimeout time.Duration,
	invalidate func(),
) error {
	if shutdownTimeout <= 0 {
		_ = listener.Close()
		return errors.New("http shutdown timeout must be positive")
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(listener) }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		lifecycle.BeginDrain()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		shutdownErr := srv.Shutdown(shutdownCtx)
		cancel()
		if shutdownErr != nil {
			closeErr := srv.Close()
			if invalidate != nil {
				invalidate()
			}
			if closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
				return errors.Join(fmt.Errorf("http server drain: %w", shutdownErr), closeErr)
			}
			return fmt.Errorf("http server drain: %w", shutdownErr)
		}

		serveErr := <-errCh
		if invalidate != nil {
			invalidate()
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", serveErr)
		}
		return nil
	}
}
