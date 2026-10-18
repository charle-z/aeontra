package docs_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type p112RestartMatrix struct {
	SchemaVersion                    int      `json:"schema_version"`
	Phase                            string   `json:"phase"`
	AuthoritativeStoreSharedWithEdge bool     `json:"authoritative_store_shared_with_edge"`
	RequiredProcesses                []string `json:"required_processes"`
	Cases                            []struct {
		Case     string `json:"case"`
		Evidence string `json:"evidence"`
		Expected string `json:"expected"`
	} `json:"cases"`
}

func TestP112RestartResumeMatrixIsCompleteAndBounded(t *testing.T) {
	body, err := os.ReadFile("evidence/p11-2-restart-resume-matrix.json")
	if err != nil {
		t.Fatal(err)
	}
	var matrix p112RestartMatrix
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&matrix); err != nil {
		t.Fatal(err)
	}
	if matrix.SchemaVersion != 1 || matrix.Phase != "P11.2 Step 7" || matrix.AuthoritativeStoreSharedWithEdge {
		t.Fatalf("matrix header=%+v", matrix)
	}
	wantProcesses := []string{"mcp-devbox-server", "mcp-edge", "model-turn-driver", "opencode-1.18.1"}
	if !reflect.DeepEqual(matrix.RequiredProcesses, wantProcesses) {
		t.Fatalf("processes=%v want=%v", matrix.RequiredProcesses, wantProcesses)
	}
	if len(matrix.Cases) < 31 {
		t.Fatalf("matrix cases=%d want at least 31", len(matrix.Cases))
	}
	seen := make(map[string]struct{}, len(matrix.Cases))
	for _, item := range matrix.Cases {
		if item.Case == "" || item.Evidence == "" || item.Expected == "" {
			t.Fatalf("incomplete matrix row=%+v", item)
		}
		if _, duplicate := seen[item.Case]; duplicate {
			t.Fatalf("duplicate matrix case=%q", item.Case)
		}
		seen[item.Case] = struct{}{}
	}
	for _, required := range []string{
		"server handler restart",
		"authoritative SQLite restart",
		"Edge journal restart",
		"driver and socket restart",
		"revocation during wait",
		"late response",
		"wrong device",
		"wrong runtime",
		"wrong workspace",
		"nonce replay",
		"incorrect signature",
		"incorrect request digest",
		"request reference changed",
		"result reference changed",
		"lease response lost",
		"create response lost",
		"wait response lost",
		"complete response lost",
		"temporary server outage",
		"OpenCode exits unexpectedly",
		"driver disappears",
		"timeout",
		"local kill switch",
		"two lease identities claim one runtime",
		"two waits simultaneously",
		"terminal lifecycle repeated",
		"distributed four-process path",
	} {
		if _, ok := seen[required]; !ok {
			t.Errorf("missing restart/resume case %q", required)
		}
	}
}
