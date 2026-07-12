package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStopLegacyRunnerIsIdempotentWhenAlreadyExited(t *testing.T) {
	original := dockerCommand
	defer func() { dockerCommand = original }()
	calls := 0
	dockerCommand = func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		if strings.Join(args, " ") != "inspect --format {{.State.Status}} mcp-devbox-validation-runner" {
			t.Fatalf("unexpected command: %v", args)
		}
		return []byte("exited\n"), nil
	}
	if err := stopLegacyRunner(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("docker called %d times, want 1", calls)
	}
}

func TestStopLegacyRunnerInspectsThenStopsRunningContainer(t *testing.T) {
	original := dockerCommand
	defer func() { dockerCommand = original }()
	calls := 0
	dockerCommand = func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		switch calls {
		case 1:
			return []byte("running\n"), nil
		case 2:
			if strings.Join(args, " ") != "stop --time=30 mcp-devbox-validation-runner" {
				t.Fatalf("unexpected stop command: %v", args)
			}
			return []byte("mcp-devbox-validation-runner\n"), nil
		default:
			t.Fatalf("unexpected extra docker call")
			return nil, nil
		}
	}
	if err := stopLegacyRunner(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStopLegacyRunnerReturnsBoundedDiagnostic(t *testing.T) {
	original := dockerCommand
	defer func() { dockerCommand = original }()
	dockerCommand = func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte("permission denied\n"), errors.New("exit status 1")
	}
	err := stopLegacyRunner(context.Background())
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("unexpected error: %v", err)
	}
}
