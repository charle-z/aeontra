package sandboxexecutor

import (
	"context"
	"net/http"
	"time"
)

func contextWithMaximum(r *http.Request, maximum time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := r.Context().Deadline(); ok && time.Until(deadline) <= maximum {
		return context.WithCancel(r.Context())
	}
	return context.WithTimeout(r.Context(), maximum)
}
