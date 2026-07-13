package policy

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func FuzzJailResolveNeverReturnsOutsideRoot(f *testing.F) {
	root := f.TempDir()
	jail, err := NewJail([]string{root})
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{"file.go", "../escape", "a/../../escape", root, root + "-evil/file", "", "."} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		resolved, err := jail.Resolve(input)
		if err == nil && !withinRoot(resolved, root) {
			t.Fatalf("Resolve(%q) returned outside root: %q", input, resolved)
		}
	})
}

func FuzzCheckCommandAllowedResultSatisfiesAllGates(f *testing.F) {
	allowed := []string{"git", "go", "ls", "cat"}
	seeds := [][2]string{
		{"git", "status"}, {"./git", "status"}, {"git", "push"}, {"go", "test ./..."},
		{"bash", "-c whoami"}, {"git", "status; rm -rf /"}, {"git.exe", "status"},
	}
	for _, seed := range seeds {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, program, argument string) {
		args := []string{argument}
		if err := CheckCommand(allowed, program, args); err == nil {
			base := normProgram(program)
			if !isBareProgramName(program) || !programAllowed(allowed, base) || alwaysBlockedPrograms[base] {
				t.Fatalf("allowed result bypassed program gates: %q", program)
			}
			for _, token := range append([]string{program}, args...) {
				if strings.ContainsAny(token, shellMetacharacters) || strings.ContainsRune(token, 0) {
					t.Fatalf("allowed result contained injection token %q", token)
				}
			}
			if isDestructiveInvocation(base, args) {
				t.Fatalf("allowed result was destructive: %q %#v", program, args)
			}
		}
	})
}

func FuzzRedactIsIdempotent(f *testing.F) {
	for _, seed := range []string{
		"plain text", "API_KEY=supersecretvalue123", "Bearer abcdefghijklmnopqrstuvwxyz",
		"ghp_0123456789abcdefghijklmnopqrstuvwxyz", "TOKEN=${TOKEN}",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		first, _ := Redact(input)
		second, _ := Redact(first)
		if second != first {
			t.Fatalf("redaction output is not idempotent: first=%q second=%q", first, second)
		}
	})
}

func FuzzAccessGrantTTLBoundary(f *testing.F) {
	for _, seed := range []int64{-1, 0, int64(time.Second), int64(5 * time.Minute), int64(time.Hour), int64(time.Hour + 1)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, nanos int64) {
		grants := NewAccessGrants()
		grants.newID = func() (string, error) { return "request", nil }
		request, err := grants.Request("/repo/.env", false)
		if err != nil {
			t.Fatal(err)
		}
		ttl := time.Duration(nanos)
		_, err = grants.Approve(request.ID, false, ttl)
		valid := ttl == 0 || (ttl >= minGrantTTL && ttl <= maxGrantTTL)
		if valid && err != nil {
			t.Fatalf("valid ttl %s rejected: %v", ttl, err)
		}
		if !valid && !errors.Is(err, ErrAccessGrantTTL) {
			t.Fatalf("invalid ttl %s returned %v, want ErrAccessGrantTTL", ttl, err)
		}
	})
}
