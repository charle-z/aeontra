//go:build !windows

package buildspike

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCommandIsClosedBoundedAndShellFree(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspaces")
	contextPath := filepath.Join(workspaceRoot, "project")
	outputRoot := filepath.Join(root, "results")
	if err := makePrivateDirs(contextPath, outputRoot); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig(1001)
	config.MaxArtifactBytes = 64 << 20
	request := BuildRequest{
		WorkspaceRoot: workspaceRoot,
		ContextPath:   contextPath,
		Dockerfile:    "Dockerfile",
		OutputRoot:    outputRoot,
		Commit:        "0123456789abcdef0123456789abcdef01234567",
		NoCache:       true,
	}
	command, artifact, err := BuildCommand(config, request)
	if err != nil {
		t.Fatal(err)
	}
	if command.Executable != config.BuildctlPath {
		t.Fatalf("executable=%q", command.Executable)
	}
	joined := strings.Join(command.Args, "\n")
	for _, required := range []string{
		"--addr", "unix://" + filepath.Join(config.RuntimeRoot, "buildkit", "buildkitd.sock"),
		"--frontend", "dockerfile.v0", "--local", "context=" + contextPath,
		"dockerfile=" + contextPath, "--opt", "filename=Dockerfile", "--no-cache",
		"--output", "type=oci,dest=" + artifact.Path,
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("command missing %q: %v", required, command.Args)
		}
	}
	for _, argument := range command.Args {
		for _, forbidden := range []string{"sh", "bash", "-c", "sudo"} {
			if argument == forbidden {
				t.Fatalf("command contains forbidden argument %q: %v", forbidden, command.Args)
			}
		}
	}
	for _, forbidden := range []string{";", "&&", "/var/run/docker.sock", "--allow=", "security.insecure", "network.host"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("command contains forbidden content %q: %v", forbidden, command.Args)
		}
	}
	if artifact.Commit != request.Commit || !strings.HasSuffix(artifact.Path, request.Commit+".oci.tar") {
		t.Fatalf("artifact=%+v", artifact)
	}
}

func TestBuildCommandRejectsEscapeAndUnboundedInput(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspaces")
	contextPath := filepath.Join(workspaceRoot, "project")
	outputRoot := filepath.Join(root, "results")
	if err := makePrivateDirs(contextPath, outputRoot); err != nil {
		t.Fatal(err)
	}
	base := BuildRequest{
		WorkspaceRoot: workspaceRoot,
		ContextPath:   contextPath,
		Dockerfile:    "Dockerfile",
		OutputRoot:    outputRoot,
		Commit:        "0123456789abcdef0123456789abcdef01234567",
	}
	config := DefaultConfig(1001)
	cases := []BuildRequest{
		func() BuildRequest { value := base; value.ContextPath = root; return value }(),
		func() BuildRequest { value := base; value.Dockerfile = "../Dockerfile"; return value }(),
		func() BuildRequest { value := base; value.OutputRoot = contextPath; return value }(),
		func() BuildRequest { value := base; value.Commit = "main"; return value }(),
	}
	for index, request := range cases {
		if _, _, err := BuildCommand(config, request); err == nil {
			t.Fatalf("invalid request %d accepted: %+v", index, request)
		}
	}
}

func makePrivateDirs(paths ...string) error {
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}
