//go:build !windows

package edgeclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectProcessWorkerAcceptsKnownExitBeforeSandboxIdentity(t *testing.T) {
	for _, test := range []struct {
		name     string
		command  string
		metadata bool
		code     int
	}{
		{"failure", "/bin/false", true, 1},
		{"success", "/bin/true", true, 0},
		{"missing_metadata", "/bin/true", false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := os.MkdirTemp("", "mcp-fast-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(root) })
			workerRoot := filepath.Join(root, projectProcessWorkerDirectory)
			for _, dir := range []string{workerRoot, filepath.Join(root, projectProcessLogDirectory), filepath.Join(root, projectProcessStdinDirectory)} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			helper := filepath.Join(root, "sandbox-helper")
			// Like a very short-lived sandbox, its workload is gone before the worker
			// can read /proc. The launcher still publishes well-formed sandbox metadata.
			metadata := ""
			if test.metadata {
				metadata = "printf '{\"child-pid\":%s}\\n' \"$child\" >&3\n"
			}
			script := "#!/bin/sh\nsetsid " + test.command + " &\nchild=$!\nwait $child\nrc=$?\n" + metadata + "exit $rc\n"
			if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			attestation, err := captureProjectProcessWorkspaceAttestation(root, root)
			if err != nil {
				t.Fatal(err)
			}
			id := "pr_abababababababababababababababab"
			req := projectProcessWorkerRequest{Executable: helper, Args: []string{"--new-session", "--", "/bin/false"}, Dir: root, Env: []string{"PATH=/usr/bin:/bin"}, MaxLogBytes: 1024, WorkspacePath: root, WorkspaceCWDPath: root, WorkspaceAttestation: attestation}
			attachTestProjectProcessRuntimeAttestation(t, root, &req)
			body, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			if err := writePrivateProjectProcessWorkerFile(projectProcessWorkerPath(workerRoot, id, "request"), body); err != nil {
				t.Fatal(err)
			}
			output, workerErr := projectProcessWorkerTestCommand(root, id).CombinedOutput()
			if !test.metadata {
				exit, err := readProjectProcessWorkerExit(workerRoot, id)
				if workerErr == nil || err != nil || exit.ExitKnown || exit.Reason != "process_identity_invalid" {
					t.Fatalf("unattested exit=%+v err=%v worker=%v %s", exit, err, workerErr, output)
				}
				if _, err := os.Stat(projectProcessWorkerPath(workerRoot, id, "ready")); !os.IsNotExist(err) {
					t.Fatal("unattested worker acknowledged admission")
				}
				return
			}
			if workerErr != nil {
				t.Fatalf("fast worker failed: %v %s", workerErr, output)
			}
			exit, err := readProjectProcessWorkerExit(workerRoot, id)
			if err != nil || !exit.ExitKnown || exit.ExitCode != test.code || exit.Reason != "" {
				t.Fatalf("exit=%+v err=%v", exit, err)
			}
			if _, err := os.Stat(projectProcessWorkerPath(workerRoot, id, "ready")); err != nil {
				t.Fatal("completed worker did not acknowledge admission", err)
			}
			if _, err := os.Stat(projectProcessWorkerPath(workerRoot, id, "child")); !os.IsNotExist(err) {
				t.Fatalf("terminal worker fabricated child identity: %v", err)
			}
		})
	}
}
