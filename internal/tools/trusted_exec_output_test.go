package tools

import (
	"bytes"
	"strings"
	"testing"
)

func TestBoundedCombinedOutputCapsMemoryAndReportsTruncation(t *testing.T) {
	output := &boundedCombinedOutput{limit: 8}
	payload := bytes.Repeat([]byte("x"), 32)
	if written, err := output.Write(payload); err != nil || written != len(payload) {
		t.Fatalf("write=%d err=%v", written, err)
	}
	got := output.String()
	if !strings.HasPrefix(got, "xxxxxxxx") || !strings.Contains(got, "output truncated") || len(got) > 128 {
		t.Fatalf("unexpected bounded output %q", got)
	}
}
