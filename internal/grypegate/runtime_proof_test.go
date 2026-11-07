package grypegate

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRuntimeProofIsBoundedToCorrectedImage(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	definition := []byte("reviewed recipe")
	policy := RuntimePolicy{SchemaVersion: 1, Owner: "charle-z", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), GlibcSource: "34106fee698c83eb0af71d4d0381e66b399be746", GlibcPatch: "03067f3c5e39aec5ec250a44bc1b395a1afd45e8", PatchSHA: "a9d503207f2cabfe2e0b336728202824d466170759950bcc51a8cba152e28e92"}
	pb, _ := json.Marshal(policy)
	proof := RuntimeProof{SchemaVersion: 1, ImageID: "sha256:" + strings.Repeat("a", 64), DefinitionSHA: digestBytes(definition), PolicySHA: digestBytes(pb), ObservedAt: now}
	proof.Glibc.Source = policy.GlibcSource
	proof.Glibc.PatchSHA = policy.PatchSHA
	proof.Glibc.ManifestSHA = strings.Repeat("b", 64)
	proof.Glibc.FilesVerified = 9
	proof.Glibc.ABI = true
	proof.Glibc.Regression = true
	proof.Glibc.Normal = true
	proof.SQLite.Version = "3.54.1-r0"
	proof.SQLite.InventorySHA = strings.Repeat("c", 64)
	proof.SQLite.FilesScanned = 9
	proof.SQLite.CLIAbsent = true
	proof.SQLite.RenamedControl = true
	proof.SQLite.CanarySHA = "c42cdc1da4e6c59fbb90c3281c2a10a51a6fb4ce12105a7db6e0a5e68da1b2a9"
	report := []byte(`{"source":{"type":"image","target":{"imageID":"` + proof.ImageID + `"}},"matches":[{"vulnerability":{"id":"CVE-2026-84243","severity":"High","fix":{"versions":[]}},"artifact":{"name":"glibc-2.44","version":"2.44-r8","type":"apk","locations":[{"path":"/usr/lib/apk/db/installed"}]}},{"vulnerability":{"id":"CVE-2026-119586","severity":"High","fix":{"versions":[]}},"artifact":{"name":"sqlite-libs","version":"3.54.1-r0","type":"apk","locations":[{"path":"/usr/lib/apk/db/installed"}]}}]}`)
	check := func(t *testing.T, report []byte, p RuntimeProof, pol []byte, recipe []byte, at time.Time, want bool) {
		t.Helper()
		b, _ := json.Marshal(p)
		_, qualified, err := EvaluateRuntimeProof(report, pol, b, recipe, "Dockerfile", proof.ImageID, at)
		if (err == nil) != want || (want && len(qualified) != 2) {
			t.Fatalf("qualified=%d err=%v want=%v", len(qualified), err, want)
		}
	}
	check(t, report, proof, pb, definition, now, true)
	for name, mutate := range map[string]func(*RuntimeProof){
		"wrong image":              func(p *RuntimeProof) { p.ImageID = "sha256:" + strings.Repeat("b", 64) },
		"old proof":                func(p *RuntimeProof) { p.ObservedAt = now.Add(-25 * time.Hour) },
		"future proof":             func(p *RuntimeProof) { p.ObservedAt = now.Add(time.Hour) },
		"missing regression":       func(p *RuntimeProof) { p.Glibc.Regression = false },
		"missing abi":              func(p *RuntimeProof) { p.Glibc.ABI = false },
		"wrong patch":              func(p *RuntimeProof) { p.Glibc.PatchSHA = strings.Repeat("a", 64) },
		"cli present":              func(p *RuntimeProof) { p.SQLite.CLIAbsent = false },
		"renamed cli not rejected": func(p *RuntimeProof) { p.SQLite.RenamedControl = false },
		"missing inventory":        func(p *RuntimeProof) { p.SQLite.InventorySHA = "" },
		"substituted control":      func(p *RuntimeProof) { p.SQLite.CanarySHA = strings.Repeat("a", 64) },
	} {
		t.Run(name, func(t *testing.T) { p := proof; mutate(&p); check(t, report, p, pb, definition, now, false) })
	}
	check(t, report, proof, pb, []byte("changed recipe"), now, false)
	check(t, report, proof, pb, definition, now.Add(2*time.Hour), false)
	check(t, []byte(strings.Replace(string(report), `"versions":[]`, `"state":"fixed","versions":[]`, 1)), proof, pb, definition, now, false)
	for _, pair := range [][2]string{{"CVE-2026-84243", "CVE-OTHER"}, {"glibc-2.44", "glibc-2.43"}, {"2.44-r8", "2.44-r9"}, {"High", "Critical"}, {"High", "Unknown"}, {"\"versions\":[]", "\"versions\":[\"2.44-r14\"]"}, {"/usr/lib/apk/db/installed", "/elsewhere/db"}, {"sqlite-libs", "sqlite"}} {
		r := []byte(strings.Replace(string(report), pair[0], pair[1], 1))
		check(t, r, proof, pb, definition, now, false)
	}
}
