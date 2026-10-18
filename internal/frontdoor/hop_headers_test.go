package frontdoor

import (
	"net/http"
	"testing"
)

func TestRemoveHopHeadersRemovesConnectionNamedHeaders(t *testing.T) {
	header := http.Header{
		"Connection":        {"keep-alive, X-Private-Hop"},
		"Keep-Alive":        {"timeout=5"},
		"X-Private-Hop":     {"connection-local"},
		"X-End-To-End-Test": {"preserve"},
	}
	removeHopHeaders(header)
	for _, key := range []string{"Connection", "Keep-Alive", "X-Private-Hop"} {
		if header.Get(key) != "" {
			t.Fatalf("hop header %s survived: %v", key, header)
		}
	}
	if header.Get("X-End-To-End-Test") != "preserve" {
		t.Fatalf("end-to-end header changed: %v", header)
	}
}
