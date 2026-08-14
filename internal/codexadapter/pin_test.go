package codexadapter_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type codexPin struct {
	Version                string   `json:"version"`
	Tag                    string   `json:"tag"`
	LinuxAMD64Asset        string   `json:"linux_amd64_asset"`
	LinuxAMD64SHA256       string   `json:"linux_amd64_sha256"`
	LinuxAMD64BinarySHA256 string   `json:"linux_amd64_binary_sha256"`
	WireAPI                string   `json:"wire_api"`
	RequiresOpenAIAuth     bool     `json:"requires_openai_auth"`
	AppServerExperimental  bool     `json:"app_server_experimental"`
	AppServerTransports    []string `json:"app_server_transports"`
}

func TestCodexCompatibilityDecisionDocumentsBoundaries(t *testing.T) {
	body, err := os.ReadFile("../../docs/analysis/codex-harness-compatibility.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		"without forking Codex",
		"private loopback OpenAI-compatible Responses provider",
		"without giving it an OpenAI API key",
		"experimental and unsupported for production workloads",
		"Retain OpenCode as a signed rollback harness",
		"Implement managed worktrees and one-writer ownership",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("Codex compatibility decision does not contain %q", required)
		}
	}
}

func TestOfficialCodexPin(t *testing.T) {
	body, err := os.ReadFile("../../integrations/codex/pin.json")
	if err != nil {
		t.Fatal(err)
	}
	var pin codexPin
	if err := json.Unmarshal(body, &pin); err != nil {
		t.Fatal(err)
	}
	if pin.Version != "0.147.0" || pin.Tag != "rust-v0.147.0" {
		t.Fatalf("unexpected Codex pin: version=%q tag=%q", pin.Version, pin.Tag)
	}
	if pin.LinuxAMD64Asset != "codex-x86_64-unknown-linux-musl.tar.gz" {
		t.Fatalf("unexpected Linux asset: %q", pin.LinuxAMD64Asset)
	}
	if pin.LinuxAMD64SHA256 != "0246e2e773834e07f0fb5249ed6ebad12e4591e608f8c7bb97dd6a9690544c36" {
		t.Fatalf("unexpected Linux digest: %q", pin.LinuxAMD64SHA256)
	}
	if pin.LinuxAMD64BinarySHA256 != "cb0a15567e9a60a5820d54b0f6ae86d504dc3805c1eab21a47f70e3eb7b73a40" {
		t.Fatalf("unexpected Linux binary digest: %q", pin.LinuxAMD64BinarySHA256)
	}
	if pin.WireAPI != "responses" || pin.RequiresOpenAIAuth {
		t.Fatalf("unsafe provider contract: wire=%q requires_openai_auth=%v", pin.WireAPI, pin.RequiresOpenAIAuth)
	}
	if !pin.AppServerExperimental {
		t.Fatal("App Server must remain explicitly experimental in the compatibility pin")
	}
	want := []string{"stdio", "unix", "websocket"}
	if len(pin.AppServerTransports) != len(want) {
		t.Fatalf("unexpected app-server transports: %v", pin.AppServerTransports)
	}
	for index := range want {
		if pin.AppServerTransports[index] != want[index] {
			t.Fatalf("unexpected app-server transports: %v", pin.AppServerTransports)
		}
	}
}
