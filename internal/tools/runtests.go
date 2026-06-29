package tools

import (
	"context"
	"fmt"

	"github.com/carbe/mcp-devbox/internal/audit"
)

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
