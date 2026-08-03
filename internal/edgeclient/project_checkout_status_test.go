package edgeclient

import "testing"

func TestProjectCheckoutStatusCleanIgnoresOnlyUntrackedManagedRuntime(t *testing.T) {
	tests := []struct {
		name   string
		status string
		clean  bool
	}{
		{name: "empty", clean: true},
		{name: "managed runtime", status: "?? .mcp-devbox/runtime/home/.config/go/telemetry/local/weekends\n", clean: true},
		{name: "managed runtime crlf", status: "?? .mcp-devbox/cache/build\r\n?? .mcp-devbox/tools/bin/tool\r\n", clean: true},
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
