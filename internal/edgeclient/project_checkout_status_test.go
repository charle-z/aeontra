package edgeclient

import "testing"

func TestProjectCheckoutStatusArgsUseBoundedUntrackedScan(t *testing.T) {
	args := ProjectCheckoutStatusArgs()
	want := []string{"status", "--porcelain=v1", "--untracked-files=normal"}
	if len(args) != len(want) {
		t.Fatalf("status args length=%d want %d", len(args), len(want))
	}
	for index := range want {
		if args[index] != want[index] {
			t.Fatalf("status arg %d=%q want %q", index, args[index], want[index])
		}
	}

	args[2] = "--untracked-files=all"
	if ProjectCheckoutStatusArgs()[2] != "--untracked-files=normal" {
		t.Fatal("caller mutation changed the fixed status invocation")
	}
}

func TestProjectCheckoutStatusCleanIgnoresOnlyUntrackedManagedRuntime(t *testing.T) {
	tests := []struct {
		name   string
		status string
		clean  bool
	}{
		{name: "empty", clean: true},
		{name: "managed runtime", status: "?? .mcp-devbox/runtime/home/.config/go/telemetry/local/weekends\n", clean: true},
		{name: "managed runtime crlf", status: "?? .mcp-devbox/cache/build\r\n?? .mcp-devbox/tools/bin/tool\r\n", clean: true},
		{name: "managed browser harness", status: "?? .mcp-devbox/browser-harness/runs/run-1/trace.zip\n", clean: true},
		{name: "managed control file", status: "?? .mcp-devbox/authorization-revision\n?? .mcp-devbox/current-state.md\n?? .mcp-devbox/instructions.md\n?? .mcp-devbox/lab-contract.json\n?? .mcp-devbox/tool-inventory.json\n", clean: true},
		{name: "arbitrary control directory", status: "?? .mcp-devbox/notes/decision.md\n", clean: false},
		{name: "runtime lookalike", status: "?? .mcp-devbox/runtime-backup/file\n", clean: false},
		{name: "managed quoted path", status: "?? \".mcp-devbox/cache/a file\"\n", clean: true},
		{name: "ordinary untracked", status: "?? source.go\n", clean: false},
		{name: "lookalike", status: "?? .mcp-devbox-other/file\n", clean: false},
		{name: "tracked runtime modification", status: " M .mcp-devbox/current-state.md\n", clean: false},
		{name: "staged runtime addition", status: "A  .mcp-devbox/runtime/file\n", clean: false},
		{name: "mixed", status: "?? .mcp-devbox/cache/build\n M main.go\n", clean: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ProjectCheckoutStatusClean(test.status); got != test.clean {
				t.Fatalf("ProjectCheckoutStatusClean(%q)=%t want %t", test.status, got, test.clean)
			}
		})
	}
}
