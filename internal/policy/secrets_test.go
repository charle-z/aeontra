package policy

import (
	"strings"
	"testing"
)

func TestIsSecretPath_Denied(t *testing.T) {
	denied := []string{
		".env",
		"config/.env",
		".env.local",
		".env.production",
		"deploy/.env.staging",
		".ssh/id_rsa",
		"home/.ssh/known_hosts",
		"keys/server.pem",
		"certs/private.key",
		"app.keystore",
		".npmrc",
		".netrc",
		".git-credentials",
		"project/.aws/credentials",
		".gnupg/secring.gpg",
		// Casing must not bypass.
		".ENV",
		"Config/.Env.Local",
		"KEYS/Server.PEM",
		// Traversal to a secret still resolves to a secret name.
		"../../.ssh/id_ed25519",
	}
	for _, p := range denied {
		if !IsSecretPath(p) {
			t.Errorf("IsSecretPath(%q) = false, want true (must be denied)", p)
		}
	}
}

func TestIsSecretPath_Allowed(t *testing.T) {
	ok := []string{
		"main.go",
		"src/server.go",
		"README.md",
		"environment.md",  // not ".env"
		"keyboard.go",     // not a ".key" file
		"docs/.envrc.md",  // not ".env" / ".env."
		"public/cert.crt", // public cert, not a key
		"internal/env.go",
	}
	for _, p := range ok {
		if IsSecretPath(p) {
			t.Errorf("IsSecretPath(%q) = true, want false (legitimate file)", p)
		}
	}
}

func TestRedact_PrivateKeyBlock(t *testing.T) {
	in := "before\n-----BEGIN RSA PRIVATE KEY-----\nMIIE...lines...\n-----END RSA PRIVATE KEY-----\nafter"
	out, red := Redact(in)
	if !red {
		t.Fatal("expected private key block to be redacted")
	}
	if contains(out, "BEGIN RSA PRIVATE KEY") {
		t.Errorf("private key header leaked: %q", out)
	}
	if !contains(out, "before") || !contains(out, "after") {
		t.Errorf("surrounding content should be preserved: %q", out)
	}
}

func TestRedact_Tokens(t *testing.T) {
	cases := map[string]string{
		"aws":    "id = AKIA" + "IOSFODNN7EXAMPLE here",
		"github": "token gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz here",
		"google": "key AIza" + strings.Repeat("a", 35) + " end",
		"stripe": "sk" + "_live_0123456789abcdefghij here",
		"jwt":    "tok eyJ" + strings.Repeat("a", 10) + ".eyJ" + strings.Repeat("b", 10) + "." + strings.Repeat("c", 10) + " here",
		"slack":  "xo" + "xb-1234567890-abcdefghijklmno here",
	}
	for name, in := range cases {
		out, red := Redact(in)
		if !red {
			t.Errorf("%s: expected redaction in %q", name, in)
		}
		if out == in {
			t.Errorf("%s: content unchanged, secret leaked: %q", name, out)
		}
	}
}

func TestRedact_GenericAssignments(t *testing.T) {
	in := `api_key = "supersecretvalue123"
password: hunter2hunter2
DB_TOKEN=abcdef0123456789`
	out, red := Redact(in)
	if !red {
		t.Fatal("expected generic credential assignments to be redacted")
	}
	for _, leaked := range []string{"supersecretvalue123", "hunter2hunter2", "abcdef0123456789"} {
		if contains(out, leaked) {
			t.Errorf("secret value leaked (%q) in %q", leaked, out)
		}
	}
	// The key names should remain so the agent still sees structure.
	if !contains(out, "api_key") {
		t.Errorf("key name should be preserved: %q", out)
	}
}

func TestRedact_GenericAssignmentsRealSecretsStillRedact(t *testing.T) {
	cases := map[string]string{
		"mcp-token":       "MCP_DEVBOX_TOKEN=supersecretvalue123",
		"quoted-secret":   `CLIENT_SECRET="abcdef0123456789"`,
		"password-urlish": "PASSWORD=postgres://user:pass@example.invalid/db",
	}
	for name, in := range cases {
		out, red := Redact(in)
		if !red {
			t.Errorf("%s: expected redaction in %q", name, in)
		}
		if out == in {
			t.Errorf("%s: content unchanged, secret leaked: %q", name, out)
		}
	}
}

func TestRedact_GenericAssignmentsFalsePositives(t *testing.T) {
	cases := map[string]string{
		"shell-command-substitution": `MCP_DEVBOX_TOKEN="$(openssl rand -base64 32)"`,
		"bare-env-ref":               "MCP_DEVBOX_TOKEN=$MCP_DEVBOX_TOKEN",
		"braced-env-ref":             "MCP_DEVBOX_TOKEN=${MCP_DEVBOX_TOKEN}",
		"powershell-env-ref":         "MCP_DEVBOX_TOKEN=$env:MCP_DEVBOX_TOKEN",
		"angle-placeholder":          "MCP_DEVBOX_TOKEN=<paste-the-token>",
		"replace-placeholder":        "CLIENT_SECRET=REPLACE_ME_WITH_SECRET",
		"your-placeholder":           "API_KEY=your-token-here",
	}
	for name, in := range cases {
		out, red := Redact(in)
		if red || out != in {
			t.Errorf("%s: false positive redaction; redacted=%v out=%q", name, red, out)
		}
	}
}

func TestRedact_CleanContentUnchanged(t *testing.T) {
	in := "package main\n\nfunc main() { println(\"hello world\") }\n"
	out, red := Redact(in)
	if red || out != in {
		t.Errorf("clean source should not be altered; got redacted=%v out=%q", red, out)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
