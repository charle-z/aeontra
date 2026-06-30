package tools

import (
	"context"
	"fmt"

	"github.com/carbe/mcp-devbox/internal/audit"
)

// RunCommand runs a single allowlisted program with args (one-off). It is gated by
// the same command policy as everything else: allowlist membership + destructive/
// injection block + write/command posture (read-only denies, ask needs approve=true,
// allow runs). This is NOT a free terminal — only allowlisted programs run, no shell,
// no metacharacters. Output is redacted. For the project's fixed test command, prefer
// run_tests.
func (s *Service) RunCommand(prog string, args []string, approve bool) (string, error) {
	sp := s.log.Start("run_command")
	summary := summarize(append([]string{prog}, args...)...)
	needsApproval, err := s.pol.CheckCommand(prog, args)
	if err != nil {
		sp.Finish(audit.Deny, summary, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, summary, nil, nil)
		return fmt.Sprintf("APPROVAL REQUIRED: run_command would execute %q. Re-invoke with approve=true.", prog), nil
	}
	out, runErr := s.run(context.Background(), s.root, prog, args)
	if runErr != nil {
		sp.Finish(audit.Allow, summarize(prog), nil, runErr)
		return s.redact(out), fmt.Errorf("command failed: %w", runErr)
	}
	sp.Finish(audit.Allow, summarize(prog), nil, nil)
	return s.redact(out), nil
}

// RunTests runs the configured test command. It is command execution, so it is
// gated by the write/command posture: read-only denies; ask requires approve=true;
// allow runs. The base command must be configured (WithTestCommand) and pass the
// allowlist gate. Output is redacted before return.
func (s *Service) RunTests(approve bool, extra ...string) (string, error) {
	sp := s.log.Start("run_tests")
	if len(s.testCmd) == 0 {
		err := fmt.Errorf("no test command configured for this project")
		sp.Finish(audit.Error, "run_tests", nil, err)
		return "", err
	}
	prog := s.testCmd[0]
	args := append(append([]string{}, s.testCmd[1:]...), extra...)

	needsApproval, err := s.pol.CheckCommand(prog, args)
	if err != nil {
		sp.Finish(audit.Deny, summarize(append([]string{prog}, args...)...), nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, summarize(append([]string{prog}, args...)...), nil, nil)
		return fmt.Sprintf("APPROVAL REQUIRED: run_tests would execute %q. Re-invoke with approve=true.", prog), nil
	}

	out, runErr := s.run(context.Background(), s.root, prog, args)
	if runErr != nil {
		// A non-zero exit (failing tests) is a normal result, not a policy error:
		// return the (redacted) output along with the error for the agent to read.
		sp.Finish(audit.Allow, summarize(prog), nil, runErr)
		return s.redact(out), fmt.Errorf("tests failed: %w", runErr)
	}
	sp.Finish(audit.Allow, summarize(prog), nil, nil)
	return s.redact(out), nil
}
