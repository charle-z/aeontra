package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

type execTestSandbox struct{}

func (execTestSandbox) Status(context.Context) SandboxStatusInfo {
	return SandboxStatusInfo{Available: true, Backend: "test", DefaultEgress: "deny"}
}

func (execTestSandbox) Run(ctx context.Context, request SandboxRunRequest) (SandboxRunResult, error) {
	command := exec.CommandContext(ctx, request.Argv[0], request.Argv[1:]...)
	command.Dir = request.Dir
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	output, err := command.CombinedOutput()
	result := SandboxRunResult{Stdout: string(output), SandboxBackend: "test", EgressProfile: "none"}
	if exit, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exit.ExitCode()
	}
	if err != nil && strings.TrimSpace(string(output)) != "" {
		return result, err
	}
	return result, err
}
