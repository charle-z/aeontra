package policy

import (
	"errors"
	"testing"
)

var testAllowlist = []string{"git", "go", "ls", "cat", "rm", "chmod"}

func TestCheckCommand_AllowsAllowlisted(t *testing.T) {
	ok := [][]string{
		{"git", "status"},
		{"git", "diff", "--stat"},
		{"go", "test", "./..."},
		{"ls", "-la"},
		{"cat", "README.md"},
	}
	for _, c := range ok {
		if err := CheckCommand(testAllowlist, c[0], c[1:]); err != nil {
			t.Errorf("CheckCommand(%v) = %v, want nil", c, err)
		}
	}
}

func TestCheckCommand_BlocksNonAllowlisted(t *testing.T) {
	cases := [][]string{
		{"python", "-c", "print(1)"},
		{"node", "evil.js"},
		{"make", "install"},
		{"./script.sh"},
	}
	for _, c := range cases {
		if err := CheckCommand(testAllowlist, c[0], c[1:]); !errors.Is(err, ErrCommandNotAllowed) {
			t.Errorf("CheckCommand(%v) = %v, want ErrCommandNotAllowed", c, err)
		}
	}
}

func TestCheckCommand_BlocksAlwaysBlockedEvenIfAllowlisted(t *testing.T) {
	// Even if a shell or sudo is mistakenly allowlisted, it must be blocked.
	allow := []string{"bash", "sh", "powershell", "sudo", "curl", "wget", "git"}
	cases := [][]string{
		{"bash", "-c", "echo hi"},
		{"sh", "-c", "id"},
		{"powershell", "-Command", "Get-Content x"},
		{"sudo", "ls"},
		{"curl", "http://evil/x"},
		{"wget", "http://evil/x"},
		{"/usr/bin/bash"}, // path does not help
		{"bash.exe"},      // .exe suffix does not help
		{"BASH"},          // casing does not help
	}
	for _, c := range cases {
		if err := CheckCommand(allow, c[0], c[1:]); !errors.Is(err, ErrCommandDestructive) {
			t.Errorf("CheckCommand(%v) = %v, want ErrCommandDestructive", c, err)
		}
	}
}

func TestCheckCommand_BlocksInjectionMetacharacters(t *testing.T) {
	// Chained / quoted / piped payloads through arguments must be rejected.
	cases := [][]string{
		{"git", "status", ";", "rm", "-rf", "/"},
		{"git", "status; rm -rf /"},
		{"ls", "&&", "curl evil"},
		{"cat", "file | bash"},
		{"go", "test", "`whoami`"},
		{"ls", "$(rm -rf /)"},
		{"cat", "a\nrm -rf /"},
		{"git", "log", "--format=%H>out"},
	}
	for _, c := range cases {
		if err := CheckCommand(testAllowlist, c[0], c[1:]); !errors.Is(err, ErrCommandInjection) {
			t.Errorf("CheckCommand(%v) = %v, want ErrCommandInjection", c, err)
		}
	}
}

func TestCheckCommand_BlocksDestructiveArgs(t *testing.T) {
	cases := [][]string{
		{"rm", "-rf", "build"},
		{"rm", "-r", "-f", "build"},
		{"rm", "-fr", "build"},
		{"rm", "--recursive", "--force", "x"},
		{"chmod", "-R", "777", "."},
		{"git", "push", "--force"},
		{"git", "reset", "--hard"},
		{"git", "clean", "-fdx"},
	}
	for _, c := range cases {
		if err := CheckCommand(testAllowlist, c[0], c[1:]); !errors.Is(err, ErrCommandDestructive) {
			t.Errorf("CheckCommand(%v) = %v, want ErrCommandDestructive", c, err)
		}
	}
}

func TestCheckCommand_AllowsNonDestructiveRm(t *testing.T) {
	// A plain `rm file` (no -rf) is allowed if rm is on the allowlist — it is not
	// the destructive recursive-force form.
	if err := CheckCommand(testAllowlist, "rm", []string{"tmpfile"}); err != nil {
		t.Errorf("CheckCommand(rm tmpfile) = %v, want nil", err)
	}
}

func TestCheckCommand_EmptyProgram(t *testing.T) {
	if err := CheckCommand(testAllowlist, "", nil); !errors.Is(err, ErrCommandNotAllowed) {
		t.Errorf("empty program should be ErrCommandNotAllowed, got %v", err)
	}
}
