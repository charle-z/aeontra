package policy

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/carbe/mcp-devbox/internal/config"
)

func TestAccessGrants_ApproveConsumeSingleUseExactPath(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)
	secret := filepath.Join(root, ".env")

	req, err := p.RequestReadAccess(secret, false)
	if err != nil {
		t.Fatal(err)
	}
	if req.ID == "" {
		t.Fatal("request id should be populated")
	}

	if _, err := p.ApproveReadAccess(req.ID, false, time.Minute); err != nil {
		t.Fatal(err)
	}
	raw, err := p.ConsumeReadGrant(req.ID, secret, false)
	if err != nil {
		t.Fatal(err)
	}
	if raw {
		t.Fatal("normal grant must not allow raw output")
	}

	if _, err := p.ConsumeReadGrant(req.ID, secret, false); !errors.Is(err, ErrAccessGrantUsed) {
		t.Fatalf("grant should be single-use, got %v", err)
	}
}

func TestAccessGrants_ExpiredAndWrongPathDenied(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)
	secret := filepath.Join(root, ".env")
	sibling := filepath.Join(root, ".env.local")

	req, err := p.RequestReadAccess(secret, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApproveReadAccess(req.ID, false, -time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ConsumeReadGrant(req.ID, secret, false); !errors.Is(err, ErrAccessGrantExpired) {
		t.Fatalf("expired grant should be denied, got %v", err)
	}

	req, err = p.RequestReadAccess(secret, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApproveReadAccess(req.ID, false, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ConsumeReadGrant(req.ID, sibling, false); !errors.Is(err, ErrAccessGrantPathMismatch) {
		t.Fatalf("grant must be exact path only, got %v", err)
	}
}

func TestAccessGrants_RawRequiresRawApproval(t *testing.T) {
	root := t.TempDir()
	p := mustPolicy(t, config.ModeReadOnly, root)
	secret := filepath.Join(root, ".env")

	req, err := p.RequestReadAccess(secret, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApproveReadAccess(req.ID, false, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ConsumeReadGrant(req.ID, secret, true); !errors.Is(err, ErrRawAccessDenied) {
		t.Fatalf("normal grant must not allow raw output, got %v", err)
	}

	req, err = p.RequestReadAccess(secret, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApproveReadAccess(req.ID, true, time.Minute); err != nil {
		t.Fatal(err)
	}
	raw, err := p.ConsumeReadGrant(req.ID, secret, true)
	if err != nil {
		t.Fatal(err)
	}
	if !raw {
		t.Fatal("raw grant should allow raw output")
	}
}

func TestAccessGrants_NewPolicyDoesNotPersistGrants(t *testing.T) {
	root := t.TempDir()
	p1 := mustPolicy(t, config.ModeReadOnly, root)
	secret := filepath.Join(root, ".env")
	req, err := p1.RequestReadAccess(secret, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p1.ApproveReadAccess(req.ID, false, time.Minute); err != nil {
		t.Fatal(err)
	}

	p2 := mustPolicy(t, config.ModeReadOnly, root)
	if _, err := p2.ConsumeReadGrant(req.ID, secret, false); !errors.Is(err, ErrAccessGrantInvalid) {
		t.Fatalf("new policy should not see old in-memory grant, got %v", err)
	}
}
