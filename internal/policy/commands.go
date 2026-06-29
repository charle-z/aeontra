package policy

import (
	"errors"
	"path/filepath"
	"strings"
)

var (
	// ErrCommandNotAllowed: the program is not on the per-project allowlist.
	ErrCommandNotAllowed = errors.New("policy: command not on allowlist")
	// ErrCommandDestructive: the command (or its arguments) is inherently dangerous.
	ErrCommandDestructive = errors.New("policy: destructive command blocked")
	// ErrCommandInjection: an argument contains shell metacharacters / injection.
	ErrCommandInjection = errors.New("policy: shell metacharacters not permitted in command")
)

// alwaysBlockedPrograms are never runnable, even if added to an allowlist by
// mistake. Shells and interpreters would defeat the allowlist (they can run
// anything); privilege escalators and network-pipe tools enable exfil/escape.
var alwaysBlockedPrograms = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "ksh": true, "dash": true,
	"csh": true, "tcsh": true,
	"cmd": true, "command": true, "powershell": true, "pwsh": true,
	"sudo": true, "doas": true, "su": true, "runas": true,
	"curl": true, "wget": true,
	"nc": true, "ncat": true, "netcat": true, "telnet": true,
	"format": true, "mkfs": true, "dd": true,
	"iex": true,
}

// shellMetacharacters smuggle additional commands when content reaches a shell.
// We never invoke a shell (execution uses an explicit argv), but we reject these
// defensively so chained/substituted/redirected payloads ("a; rm -rf /",
// "x | bash", "$(...)", "`...`", ">out") cannot pass through tool arguments. We
// deliberately do NOT reject glob/brace/paren chars (* ( ) { } !) — harmless in an
// argv and present in legitimate arguments.
const shellMetacharacters = ";|&`$<>\n\r\t"

// normProgram returns the lowercased program basename without an .exe suffix.
func normProgram(prog string) string {
	base := strings.ToLower(filepath.Base(filepath.ToSlash(prog)))
	base = strings.TrimSuffix(base, ".exe")
	return base
}

// CheckCommand decides whether a program may run with the given arguments under
// the allowlist. It is the single command gate; tools never exec without it.
// Execution itself must use an explicit argv (never `sh -c`) inside the jail.
func CheckCommand(allowed []string, prog string, args []string) error {
	if strings.TrimSpace(prog) == "" {
		return ErrCommandNotAllowed
	}
	// 1. Injection: reject metacharacters / NUL in the program and every argument.
	for _, tok := range append([]string{prog}, args...) {
		if strings.ContainsAny(tok, shellMetacharacters) || strings.ContainsRune(tok, 0) {
			return ErrCommandInjection
		}
	}
	base := normProgram(prog)
	// 2. Always-blocked programs (shells, sudo, network pipes, fs-format tools).
	if alwaysBlockedPrograms[base] {
		return ErrCommandDestructive
	}
	// 3. Allowlist membership (compared on normalized basename).
	if !programAllowed(allowed, base) {
		return ErrCommandNotAllowed
	}
	// 4. Destructive argument patterns on otherwise-allowed programs.
	if isDestructiveInvocation(base, args) {
		return ErrCommandDestructive
	}
	return nil
}

func programAllowed(allowed []string, base string) bool {
	for _, a := range allowed {
		if normProgram(a) == base {
			return true
		}
	}
	return false
}

// isDestructiveInvocation flags dangerous argument combinations on allowed programs.
func isDestructiveInvocation(base string, args []string) bool {
	lower := make([]string, len(args))
	for i, a := range args {
		lower[i] = strings.ToLower(a)
	}
	joined := strings.Join(lower, " ")

	switch base {
	case "rm":
		// rm with both recursive and force, in any spelling (-rf, -r -f, --force…).
		if (hasFlag(lower, "r", "recursive") || strings.Contains(joined, "-rf") || strings.Contains(joined, "-fr")) &&
			(hasFlag(lower, "f", "force") || strings.Contains(joined, "-rf") || strings.Contains(joined, "-fr")) {
			return true
		}
	case "rmdir", "rd":
		if containsAny(lower, "/s", "/q") {
			return true
		}
	case "del", "erase":
		if containsAny(lower, "/s", "/q", "/f") {
			return true
		}
	case "chmod":
		if (hasFlag(lower, "r", "recursive") || strings.Contains(joined, "-r")) && strings.Contains(joined, "777") {
			return true
		}
	case "chown":
		if hasFlag(lower, "r", "recursive") {
			return true
		}
	case "git":
		// Network-mutating / history-destroying git is not allowed via command exec
		// (the controlled git_* tools handle read + apply). Block the worst.
		if len(args) > 0 {
			switch lower[0] {
			case "push", "clean":
				return true
			case "reset":
				if containsAny(lower, "--hard") {
					return true
				}
			}
		}
	}
	return false
}

// hasFlag reports whether args contain a short flag (e.g. "-r", possibly bundled
// like "-rf") or a long flag ("--recursive").
func hasFlag(args []string, short, long string) bool {
	for _, a := range args {
		if a == "--"+long {
			return true
		}
		if len(a) >= 2 && a[0] == '-' && a[1] != '-' {
			if strings.ContainsRune(a[1:], rune(short[0])) {
				return true
			}
		}
	}
	return false
}

func containsAny(args []string, subs ...string) bool {
	for _, a := range args {
		for _, s := range subs {
			if a == s {
				return true
			}
		}
	}
	return false
}
