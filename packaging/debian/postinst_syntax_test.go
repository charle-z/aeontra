package debian

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestP16PostinstParsesAsPOSIXShell(t *testing.T) {
	path := filepath.Join(t.TempDir(), "postinst")
	if err := os.WriteFile(path, []byte(repoFile(t, "packaging/debian/postinst.in")), 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("sh", "-n", path).CombinedOutput()
	if err != nil {
		t.Fatalf("postinst shell syntax failed: %v: %s", err, output)
	}
}
