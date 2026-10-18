package policy

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

// The core security property of grants: a request_id is NOT a grant. The agent
// receives a request_id in the access-required response; it must never be able to
// turn that into access by itself — only a local human Approve creates a grant.

func TestAccessGrants_AgentCannotReplayRequestIDWithoutApproval(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)
	secret := filepath.Join(root, ".env")

	// First call: agent hits a secret path, gets an access-required error + id.
	_, _, err := p.CheckReadWithAccess(secret, "", false)
	var required *AccessRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("expected AccessRequiredError, got %v", err)
	}
	if required.RequestID == "" {
		t.Fatal("access-required should carry a request_id")
	}

	// Agent immediately replays that request_id WITHOUT any human approval.
	if _, _, err := p.CheckReadWithAccess(secret, required.RequestID, false); !errors.Is(err, ErrAccessGrantInvalid) {
		t.Fatalf("agent replayed request_id without approval and was not denied: %v", err)
	}
}

func TestAccessGrants_ConsumeWithoutApproveIsInvalid(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)
	secret := filepath.Join(root, ".env")

	req, err := p.RequestReadAccess(secret, false)
	if err != nil {
		t.Fatal(err)
	}
	// No ApproveReadAccess call here.
	if _, err := p.ConsumeReadGrant(req.ID, secret, false); !errors.Is(err, ErrAccessGrantInvalid) {
		t.Fatalf("a requested-but-unapproved id must not be consumable, got %v", err)
	}
}

func TestAccessGrants_ApproveUnknownIDFails(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)
	if _, err := p.ApproveReadAccess("deadbeefdeadbeefdeadbeefdeadbeef", false, time.Minute); !errors.Is(err, ErrAccessGrantInvalid) {
		t.Fatalf("approving an unknown request id should fail, got %v", err)
	}
}

func TestAccessGrants_GrantForOnePathCannotReadAnother(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)
	envFile := filepath.Join(root, ".env")
	sshKey := filepath.Join(root, ".ssh", "id_rsa")

	req, err := p.RequestReadAccess(envFile, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApproveReadAccess(req.ID, false, time.Minute); err != nil {
		t.Fatal(err)
	}
	// Use the .env grant to try to read a different secret (an SSH key).
	if _, _, err := p.CheckReadWithAccess(sshKey, req.ID, false); !errors.Is(err, ErrAccessGrantPathMismatch) {
		t.Fatalf("a grant for .env must not unlock a different secret path, got %v", err)
	}
}
