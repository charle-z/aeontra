package policy

import (
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
)

// TestJail_FuzzNeverEscapes throws thousands of hostile, randomly-assembled path
// inputs at the jail. The invariant is absolute: Resolve MUST either deny the input
// or return a path contained within the root — it may never resolve to somewhere
// outside. This is the crown-jewel security property (it also covers command
// execution, which is jailed through the same Resolve).
func TestJail_FuzzNeverEscapes(t *testing.T) {
	root := t.TempDir()
	j, err := NewJail([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	rootResolved := root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		rootResolved = r
	}

	// A nasty alphabet: traversal, separators, drive/UNC bits, encodings, junk.
	segments := []string{
		"..", "...", ".", "a", "sub", "etc", "passwd", "id_rsa", "",
		" ", "\t", "\n", "..\\", "../", "../..", "%2e%2e", "%2f",
		"C:", "/", "\\", "\\\\", "~", "$HOME", ".ssh", ".git",
		"....//", "..;", "/etc", "/root", "con", "nul",
	}
	rng := rand.New(rand.NewSource(20260702))
	for i := 0; i < 5000; i++ {
		n := rng.Intn(10) + 1
		parts := make([]string, n)
		for k := range parts {
			parts[k] = segments[rng.Intn(len(segments))]
		}
		// Mix separators to stress path parsing.
		sep := "/"
		if rng.Intn(2) == 0 {
			sep = string(filepath.Separator)
		}
		input := strings.Join(parts, sep)

		got, err := j.Resolve(input)
		if err != nil {
			continue // denial is always acceptable
		}
		if !withinRoot(got, rootResolved) {
			t.Fatalf("JAIL ESCAPE: input=%q resolved=%q is outside root=%q", input, got, rootResolved)
		}
	}
}
