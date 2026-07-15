package tools

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/audit"
	"github.com/charle-z/mcp-devbox/internal/config"
	"github.com/charle-z/mcp-devbox/internal/policy"
	"github.com/charle-z/mcp-devbox/internal/resultstore"
)

func TestResultCapabilityReadsFindsStagesAndAuditsWithoutSensitiveArguments(t *testing.T) {
	root := t.TempDir()
	pol, err := policy.NewPolicy(config.Config{Roots: []string{root}, Mode: config.ModeAllow})
	if err != nil {
		t.Fatal(err)
	}
	var auditOutput bytes.Buffer
	log := audit.New(&auditOutput)
	store, err := resultstore.Open(resultstore.Config{Root: filepath.Join(t.TempDir(), "results")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewService(pol, log, root).WithResultStore(store)
	secret := "gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz"
	meta, err := store.Put(resultstore.Input{Status: resultstore.StatusSuccess, Summary: "large result stored", Content: "needle " + secret})
	if err != nil {
		t.Fatal(err)
	}

	read, err := service.ResultRead(meta.ResultRef, 0, 1024)
	if err != nil || strings.Contains(read, secret) || !strings.Contains(read, "REDACTED") {
		t.Fatalf("read=%q err=%v", read, err)
	}
	found, err := service.ResultFind("needle", 5)
	if err != nil || !strings.Contains(found, meta.ResultRef) {
		t.Fatalf("find=%q err=%v", found, err)
	}
	stage, err := service.ResultStage(meta.ResultRef, 0, 1024)
	if err != nil || !strings.Contains(stage, "needle") {
		t.Fatalf("stage=%q err=%v", stage, err)
	}

	for _, forbidden := range []string{meta.ResultRef, "needle", secret} {
		if strings.Contains(auditOutput.String(), forbidden) {
			t.Fatalf("audit leaked %q: %s", forbidden, auditOutput.String())
		}
	}
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(auditOutput.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, entry)
	}
	if len(lines) != 3 {
		t.Fatalf("audit entries=%d want=3", len(lines))
	}
}

func TestResultCapabilityFailsClosedWithoutStore(t *testing.T) {
	root := t.TempDir()
	pol, err := policy.NewPolicy(config.Config{Roots: []string{root}, Mode: config.ModeAllow})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(pol, audit.New(&bytes.Buffer{}), root)
	if _, err := service.ResultRead("rs_0123456789abcdef0123456789abcdef", 0, 10); err == nil {
		t.Fatal("result read succeeded without a configured store")
	}
}
