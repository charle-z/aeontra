package tools

import (
	"fmt"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

// BenchmarkWorkspaceStateReconstruction compares the prior two-call state rebuild
// (context pack plus repeated repository status) with the compact checkpoint. It
// reports measured response bytes and repeated status bytes without asserting a
// reduction in advance.
func BenchmarkWorkspaceStateReconstruction(b *testing.B) {
	svc, root := initRepo(b, config.ModeReadOnly)
	configIdentity(b, root)
	write(b, root, "README.md", "# benchmark repository\n")
	write(b, root, ".agent-memory/current-task.md", "# Current task\n\nMeasure checkpoint overhead.\n")
	for i := 0; i < 80; i++ {
		write(b, root, fmt.Sprintf("src/file-%03d.go", i), "package src\n")
	}
	gitCmd(b, root, "add", ".")
	gitCmd(b, root, "commit", "-qm", "benchmark fixture")

	b.Run("previous_reconstruction", func(b *testing.B) {
		var responseBytes, repeatedBytes int64
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pack, err := svc.BuildContextPackIn("")
			if err != nil {
				b.Fatal(err)
			}
			status, err := svc.RepoStatus("")
			if err != nil {
				b.Fatal(err)
			}
			responseBytes += int64(len(pack) + len(status))
			repeatedBytes += int64(len(status))
		}
		b.ReportMetric(2, "mcp_calls/op")
		b.ReportMetric(float64(responseBytes)/float64(b.N), "response_bytes/op")
		b.ReportMetric(float64(repeatedBytes)/float64(b.N), "repeated_bytes/op")
	})

	b.Run("workspace_checkpoint", func(b *testing.B) {
		var responseBytes int64
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			checkpoint, err := svc.WorkspaceCheckpointIn("")
			if err != nil {
				b.Fatal(err)
			}
			responseBytes += int64(len(checkpoint))
		}
		b.ReportMetric(1, "mcp_calls/op")
		b.ReportMetric(float64(responseBytes)/float64(b.N), "response_bytes/op")
		b.ReportMetric(0, "repeated_bytes/op")
	})
}
