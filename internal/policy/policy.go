package policy

import (
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

// ErrReadOnly is returned when a write or command is attempted while the server is
// in read-only mode (the secure default).
var ErrReadOnly = errors.New("policy: blocked: server is read-only (writes/commands disabled)")

// Policy is the single security decision surface. Every MCP tool consults it; no
// tool re-implements path/secret/command checks. Its fields are unexported and set
// only by NewPolicy — there is no setter and no MCP-reachable mutator, so the agent
// cannot relax policy at runtime (constitution Article I.8).
type Policy struct {
	jail    *Jail
	allowed []string
	mode    config.Mode
	grants  *AccessGrants
}

// NewPolicy builds an immutable Policy from a validated Config.
func NewPolicy(cfg config.Config) (*Policy, error) {
	jail, err := NewJail(cfg.Roots)
	if err != nil {
		return nil, err
	}
	allowed := make([]string, len(cfg.AllowedCommands))
	copy(allowed, cfg.AllowedCommands)
	mode, err := config.NormalizeMode(cfg.Mode)
	if err != nil {
		return nil, err
	}
	return &Policy{jail: jail, allowed: allowed, mode: mode, grants: NewAccessGrants()}, nil
}

// Mode returns the effective access posture (read-only diagnostic accessor).
func (p *Policy) Mode() config.Mode { return p.mode }

// Roots returns the jail roots (copy).
func (p *Policy) Roots() []string { return p.jail.Roots() }

// AccessGrants returns the in-memory grant manager used by the local admin
// channel. It does not expose a policy/config mutator to MCP tools.
func (p *Policy) AccessGrants() *AccessGrants { return p.grants }

// CheckRead authorizes reading a path. It denies secret paths (by name) regardless
// of the jail, then enforces jail containment. Returns the resolved absolute path.
func (p *Policy) CheckRead(path string) (string, error) {
	if IsSecretPath(path) {
		return "", ErrSecretDenied
	}
	resolved, err := p.jail.Resolve(path)
	if err != nil {
		return "", err
	}
	// Re-check after resolution (e.g. a symlink that lands on a secret name).
	if IsSecretPath(resolved) {
		return "", ErrSecretDenied
	}
	return resolved, nil
}

// CheckReadWithAccess is the tool-facing read gate. It preserves jail checks,
// denies secret paths by default, and only allows a secret path when a local human
// has approved an exact-path, unexpired, single-use grant.
func (p *Policy) CheckReadWithAccess(path, requestID string, raw bool) (resolved string, rawAllowed bool, err error) {
	resolved, err = p.jail.Resolve(path)
	if err != nil {
		return "", false, err
	}
	if !IsSecretPath(path) && !IsSecretPath(resolved) {
		return resolved, false, nil
	}
	if requestID == "" {
		req, err := p.RequestReadAccess(resolved, raw)
		if err != nil {
			return "", false, err
		}
		return "", false, &AccessRequiredError{
			Type:         "access-required",
			RequestID:    req.ID,
			Path:         req.Path,
			Reason:       req.Reason,
			RawRequested: req.RawRequested,
		}
	}
	rawAllowed, err = p.ConsumeReadGrant(requestID, resolved, raw)
	if err != nil {
		return "", false, err
	}
	return resolved, rawAllowed, nil
}

// RequestReadAccess creates a pending request for an exact, already-resolved
// secret path. It still validates the path through the jail first.
func (p *Policy) RequestReadAccess(path string, raw bool) (AccessRequest, error) {
	resolved, err := p.jail.Resolve(path)
	if err != nil {
		return AccessRequest{}, err
	}
	if !IsSecretPath(path) && !IsSecretPath(resolved) {
		return AccessRequest{}, nil
	}
	return p.grants.Request(resolved, raw)
}

func (p *Policy) ApproveReadAccess(requestID string, raw bool, ttl time.Duration) (AccessDecision, error) {
	return p.grants.Approve(requestID, raw, ttl)
}

func (p *Policy) ConsumeReadGrant(requestID, path string, raw bool) (bool, error) {
	resolved, err := p.jail.Resolve(path)
	if err != nil {
		return false, err
	}
	return p.grants.Consume(requestID, resolved, raw)
}

// CheckWrite authorizes writing a path. It performs the same jail+secret checks as
// CheckRead, then applies the write posture: read-only denies; ask requires
// approval (needsApproval=true, the tool must NOT execute yet); allow permits.
func (p *Policy) CheckWrite(path string) (resolved string, needsApproval bool, err error) {
	resolved, err = p.CheckRead(path)
	if err != nil {
		return "", false, err
	}
	switch p.mode {
	case config.ModeReadOnly:
		return "", false, ErrReadOnly
	case config.ModeAsk:
		return resolved, true, nil
	case config.ModeAllow:
		return resolved, false, nil
	default:
		return "", false, config.ErrUnknownMode
	}
}

// CheckCommand authorizes running a program with args: the allowlist + destructive
// + injection gate first, then the write/command posture (read-only denies, ask
// requires approval, allow permits).
func (p *Policy) CheckCommand(prog string, args []string) (needsApproval bool, err error) {
	if err := CheckCommand(p.allowed, prog, args); err != nil {
		return false, err
	}
	switch p.mode {
	case config.ModeReadOnly:
		return false, ErrReadOnly
	case config.ModeAsk:
		return true, nil
	case config.ModeAllow:
		return false, nil
	default:
		return false, config.ErrUnknownMode
	}
}

// CheckSandboxExec applies ONLY the write/command mode posture (read-only denies;
// ask requires approval; allow permits) with NO allowlist — because a real L3
// sandbox, not an allowlist, is what contains a broad command. Callers MUST verify a
// sandbox backend is available before using this.
func (p *Policy) CheckSandboxExec() (needsApproval bool, err error) {
	return p.CheckAction()
}

// CheckAction applies ONLY the write/command mode posture (read-only denies; ask
// requires approval; allow permits). Use it for non-filesystem side-effecting
// actions (e.g. triggering an external deploy) that are gated by mode but have no
// path/allowlist of their own.
func (p *Policy) CheckAction() (needsApproval bool, err error) {
	switch p.mode {
	case config.ModeReadOnly:
		return false, ErrReadOnly
	case config.ModeAsk:
		return true, nil
	case config.ModeAllow:
		return false, nil
	default:
		return false, config.ErrUnknownMode
	}
}

// CheckCommandAllowed runs only the allowlist + destructive + injection gate,
// WITHOUT the write/command mode posture. It is for inherently read-only command
// tools (git_status, git_diff) that must work even in read-only mode.
func (p *Policy) CheckCommandAllowed(prog string, args []string) error {
	return CheckCommand(p.allowed, prog, args)
}

// Redact applies content-level secret scanning to any content before it is
// returned to the agent. Delegates to the package-level Redact.
func (p *Policy) Redact(content string) (string, bool) { return Redact(content) }
