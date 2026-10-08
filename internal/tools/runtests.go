package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

// RunCommand runs a single allowlisted program with args (one-off) inside the
// attested private L3 executor. It is gated by the same command policy as
// everything else: allowlist membership + destructive/injection block +
// write/command posture. Ask mode denies execution; an administrator must select
// allow mode explicitly. This is NOT a free terminal — only allowlisted programs
// run, no shell, no metacharacters. Output is redacted. For the project's fixed
// test command, prefer run_tests.
func (s *ExecutionCapability) RunCommand(prog string, args []string) (string, error) {
	return s.RunCommandIn(prog, args, "")
}

// RunCommandIn is RunCommand with an explicit, jailed working directory. This is
// how a global /repos root can safely operate inside one selected repo without a
// mutable session-level "cd".
func (s *ExecutionCapability) RunCommandIn(prog string, args []string, cwd string) (string, error) {
	sp := s.log.Start("run_command")
	summary := summarize(append([]string{prog}, args...)...)
	dir, err := s.workdir(cwd)
	if err != nil {
		sp.Finish(audit.Deny, summary, nil, err)
		return "", err
	}
	if err := s.pol.CheckCommandAllowed(prog, args); err != nil {
		sp.Finish(audit.Deny, summary, nil, err)
		return "", err
	}
	if err := s.pol.CheckContainedExecution(); err != nil {
		sp.Finish(audit.Deny, summary, nil, err)
		return "", err
	}
	if !s.sandbox.Status(context.Background()).Available {
		err := fmt.Errorf("run_command requires an attested private L3 executor; host execution is disabled")
		sp.Finish(audit.Deny, summary, nil, err)
		return "", err
	}
	argv := append([]string{prog}, args...)
	result, runErr := s.sandbox.Run(context.Background(), SandboxRunRequest{Dir: dir, Argv: argv, NetworkProfile: "none"})
	out := sandboxCombinedOutput(result)
	if runErr != nil {
		sp.Finish(audit.Error, summarize(prog), []string{dir}, runErr)
		return s.redact(out), fmt.Errorf("command sandbox failed: %w", runErr)
	}
	if result.ExitCode != 0 {
		exitErr := fmt.Errorf("command exited with status %d", result.ExitCode)
		sp.Finish(audit.Allow, summarize(prog), []string{dir}, exitErr)
		return s.redact(out), exitErr
	}
	sp.Finish(audit.Allow, summarize(prog), []string{dir}, nil)
	return s.redact(out), nil
}

// RunTests runs the configured test command in the attested private L3 executor.
// It is command execution, so it is gated by the write/command posture: read-only
// denies; ask denies; allow runs. The base command must
// be configured (WithTestCommand) and pass the allowlist gate. Output is redacted
// before return.
func (s *ExecutionCapability) RunTests(extra ...string) (string, error) {
	return s.RunTestsIn("", extra...)
}

// RunTestsIn is RunTests with an explicit, jailed working directory.
func (s *ExecutionCapability) RunTestsIn(cwd string, extra ...string) (string, error) {
	sp := s.log.Start("run_tests")
	if len(s.testCmd) == 0 {
		err := fmt.Errorf("no test command configured for this project")
		sp.Finish(audit.Error, "run_tests", nil, err)
		return "", err
	}
	prog := s.testCmd[0]
	args := append(append([]string{}, s.testCmd[1:]...), extra...)
	dir, err := s.workdir(cwd)
	if err != nil {
		sp.Finish(audit.Deny, summarize(append([]string{prog}, args...)...), nil, err)
		return "", err
	}

	summary := summarize(append([]string{prog}, args...)...)
	if err := s.pol.CheckCommandAllowed(prog, args); err != nil {
		sp.Finish(audit.Deny, summary, nil, err)
		return "", err
	}
	if err := s.pol.CheckContainedExecution(); err != nil {
		sp.Finish(audit.Deny, summary, nil, err)
		return "", err
	}
	if !s.sandbox.Status(context.Background()).Available {
		err := fmt.Errorf("run_tests requires an attested private L3 executor; host execution is disabled")
		sp.Finish(audit.Deny, summary, nil, err)
		return "", err
	}
	result, runErr := s.sandbox.Run(context.Background(), SandboxRunRequest{Dir: dir, Argv: append([]string{prog}, args...), NetworkProfile: "none"})
	out := sandboxCombinedOutput(result)
	if runErr != nil {
		sp.Finish(audit.Error, summarize(prog), []string{dir}, runErr)
		return s.redact(out), fmt.Errorf("test sandbox failed: %w", runErr)
	}
	if result.ExitCode != 0 {
		// A non-zero exit (failing tests) is a normal contained result, not a
		// policy error: return the redacted output for diagnosis.
		exitErr := fmt.Errorf("tests exited with status %d", result.ExitCode)
		sp.Finish(audit.Allow, summarize(prog), []string{dir}, exitErr)
		return s.redact(out), exitErr
	}
	sp.Finish(audit.Allow, summarize(prog), []string{dir}, nil)
	return s.redact(out), nil
}

func sandboxCombinedOutput(result SandboxRunResult) string {
	if result.Stdout == "" {
		return result.Stderr
	}
	if result.Stderr == "" {
		return result.Stdout
	}
	return strings.TrimRight(result.Stdout, "\n") + "\n" + result.Stderr
}
