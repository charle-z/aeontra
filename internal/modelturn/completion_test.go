package modelturn

import (
	"errors"
	"testing"
)

func TestValidateCompletionState(t *testing.T) {
	tests := []struct {
		name      string
		state     string
		finish    string
		text      string
		toolCalls int
		wantErr   error
	}{
		{name: "active tool call", state: TaskStateActive, finish: "tool_calls", text: "Continuing.", toolCalls: 1},
		{name: "complete", state: TaskStateComplete, finish: "stop", text: "All requested work is complete."},
		{name: "blocked", state: TaskStateBlocked, finish: "error", text: "Operator authority is required."},
		{name: "missing state", finish: "stop", wantErr: ErrCompletionStateRequired},
		{name: "active stopped", state: TaskStateActive, finish: "stop", wantErr: ErrActiveWithoutToolCalls},
		{name: "complete tool call", state: TaskStateComplete, finish: "tool_calls", toolCalls: 1, wantErr: ErrCompleteWithoutStop},
		{name: "blocked stopped", state: TaskStateBlocked, finish: "stop", wantErr: ErrBlockedWithoutFailure},
		{name: "truncated", state: TaskStateActive, finish: "length", wantErr: ErrTruncatedResponse},
		{name: "pending English", state: TaskStateComplete, finish: "stop", text: "X is ready. Now I will execute Y.", wantErr: ErrPrematureCompletion},
		{name: "pending English comma", state: TaskStateComplete, finish: "stop", text: "X is ready. Next, I'll execute Y.", wantErr: ErrPrematureCompletion},
		{name: "pending Spanish", state: TaskStateComplete, finish: "stop", text: "X está listo. A continuación probaré Y.", wantErr: ErrPrematureCompletion},
		{name: "quoted example", state: TaskStateComplete, finish: "stop", text: "The regression rejects `Now I will execute Y.` and is complete."},
		{name: "fenced example", state: TaskStateComplete, finish: "stop", text: "Verified this sample:\n```text\nAhora voy a ejecutar Y.\n```\nAll checks passed."},
		{name: "negated continuation", state: TaskStateComplete, finish: "stop", text: "I will not continue because every requested check passed."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateCompletionState(test.state, test.finish, test.text, test.toolCalls)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error=%v want=%v", err, test.wantErr)
			}
		})
	}
}

func TestCompletionStateForFinishReason(t *testing.T) {
	for finish, want := range map[string]string{
		"tool_calls": TaskStateActive,
		"stop":       TaskStateComplete,
		"error":      TaskStateBlocked,
		"cancelled":  TaskStateBlocked,
		"length":     "",
	} {
		if got := CompletionStateForFinishReason(finish); got != want {
			t.Fatalf("finish=%s state=%s want=%s", finish, got, want)
		}
	}
}
