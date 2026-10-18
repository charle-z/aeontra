package brain

import (
	"strings"
	"testing"
)

func TestConsoleSummaryIsExplicitBoundedMetadata(t *testing.T) {
	maximum := strings.Repeat("s", MaxConsoleSummaryBytes)
	source := strings.Replace(validSource("bounded-summary", TrustCurated, AuthorOwner), "\n---\n\n", "\nconsole_summary: "+maximum+"\n---\n\n", 1)
	note, err := ParseNote([]byte(source), "bounded-summary", TrustCurated, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if note.Metadata.ConsoleSummary != maximum {
		t.Fatalf("summary bytes=%d", len(note.Metadata.ConsoleSummary))
	}

	oversized := strings.Replace(source, maximum, maximum+"x", 1)
	if _, err := ParseNote([]byte(oversized), "bounded-summary", TrustCurated, fixedNow); err == nil {
		t.Fatal("oversized console_summary accepted")
	}
	multiline := strings.Replace(source, "console_summary: "+maximum, "console_summary: |\n  line one\n  line two", 1)
	if _, err := ParseNote([]byte(multiline), "bounded-summary", TrustCurated, fixedNow); err == nil {
		t.Fatal("multiline console_summary accepted")
	}
}

func TestAgentUpdatePreservesOwnerSuppliedConsoleSummary(t *testing.T) {
	source := strings.Replace(validSource("working-summary", TrustWorking, "agent:chatgpt"), "\n---\n\n", "\nconsole_summary: Owner-approved safe summary.\n---\n\n", 1)
	existing, err := ParseNote([]byte(source), "working-summary", TrustWorking, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	draft := agentDraft("working-summary", "agent:chatgpt")
	draft.Title = "Updated working note"
	draft.Body = "Updated private body."
	updated, err := BuildAgentNote(draft, &existing, fixedNow.Add(1))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Metadata.ConsoleSummary != existing.Metadata.ConsoleSummary {
		t.Fatalf("summary changed: existing=%q updated=%q", existing.Metadata.ConsoleSummary, updated.Metadata.ConsoleSummary)
	}
}
