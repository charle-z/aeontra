package frontdoorcoordinator

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"
)

func TestPrivateCoolifyDialErrorUsesClosedFailureClasses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		want error
	}{
		{err: context.DeadlineExceeded, want: ErrCoolifyPrivateTimeout},
		{err: syscall.ECONNREFUSED, want: ErrCoolifyPrivateRefused},
		{err: syscall.ENETUNREACH, want: ErrCoolifyPrivateRoute},
		{err: syscall.EHOSTUNREACH, want: ErrCoolifyPrivateRoute},
		{err: errors.New("raw private gateway secret"), want: ErrCoolifyPrivateConnect},
	}
	for _, testCase := range tests {
		got := classifyPrivateCoolifyDialError(testCase.err)
		if got != testCase.want {
			t.Fatalf("error=%T classification=%v want=%v", testCase.err, got, testCase.want)
		}
		if strings.Contains(got.Error(), "secret") {
			t.Fatalf("classification leaked raw error: %q", got)
		}
	}
}
