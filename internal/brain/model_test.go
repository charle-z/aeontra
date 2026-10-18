package brain

import (
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 7, 13, 23, 30, 0, 0, time.UTC)

func validSource(slug string, trust TrustLevel, author string) string {
	review := ""
	if strings.HasPrefix(author, "agent:") {
		review = "review_by: 2026-08-13\n"
	}
	return "---\n" +
		"slug: " + slug + "\n" +
		"title: Deployment rollback rule\n" +
		"type: fact\n" +
		"author: " + author + "\n" +
		"created: 2026-07-13T22:00:00Z\n" +
		"updated: 2026-07-13T22:30:00Z\n" +
		"provenance: PR 3 and production smoke\n" +
		review +
		"---\n\n" +
		"Use the verified rollback procedure. See [[release-gates]].\n"
}

func TestValidateSlugAcceptsStrictKebabCase(t *testing.T) {
	for _, slug := range []string{"a", "brain", "release-gates", strings.Repeat("a", 64)} {
		if err := ValidateSlug(slug); err != nil {
			t.Errorf("ValidateSlug(%q): %v", slug, err)
		}
	}
}

func TestValidateSlugRejectsTraversalAndNonKebabInput(t *testing.T) {
	for _, slug := range []string{
		"", ".", "..", "../curated", "working/note", `working\\note`, "/absolute",
		"two--dashes", "-leading", "trailing-", "Uppercase", "under_score",
		"with space", "café", strings.Repeat("a", 65), "a.md", "a%2fb",
	} {
		if err := ValidateSlug(slug); err == nil {
			t.Errorf("ValidateSlug(%q) unexpectedly succeeded", slug)
		}
	}
}

func TestParseNoteAcceptsStrictCuratedAndWorkingSources(t *testing.T) {
	curated, err := ParseNote([]byte(validSource("release-gates", TrustCurated, AuthorOwner)), "release-gates", TrustCurated, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if curated.Metadata.Slug != "release-gates" || curated.Trust != TrustCurated || curated.Expired {
		t.Fatalf("curated=%+v", curated)
	}

	working, err := ParseNote([]byte(validSource("rollback-hypothesis", TrustWorking, "agent:chatgpt")), "rollback-hypothesis", TrustWorking, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(working.Links) != 1 || working.Links[0] != "release-gates" {
		t.Fatalf("links=%v", working.Links)
	}
	if working.Expired {
		t.Fatal("working note unexpectedly expired")
	}
}

func TestParseNoteRejectsMalformedOrAmbiguousFrontmatter(t *testing.T) {
	base := validSource("release-gates", TrustCurated, AuthorOwner)
	cases := map[string]string{
		"missing opening delimiter": strings.TrimPrefix(base, "---\n"),
		"missing closing delimiter": strings.Replace(base, "---\n\nUse", "Use", 1),
		"unknown key":               strings.Replace(base, "provenance:", "private_path: /secret\nprovenance:", 1),
		"duplicate key":             strings.Replace(base, "title:", "slug: duplicate\ntitle:", 1),
		"slug mismatch":             strings.Replace(base, "slug: release-gates", "slug: other", 1),
		"invalid type":              strings.Replace(base, "type: fact", "type: decision", 1),
		"invalid author":            strings.Replace(base, "author: owner", "author: admin", 1),
		"curated agent":             strings.Replace(base, "author: owner", "author: agent:chatgpt\nreview_by: 2026-08-13", 1),
		"invalid timestamp":         strings.Replace(base, "created: 2026-07-13T22:00:00Z", "created: yesterday", 1),
		"non UTC timestamp":         strings.Replace(base, "created: 2026-07-13T22:00:00Z", "created: 2026-07-13T17:00:00-05:00", 1),
		"created after updated":     strings.Replace(base, "created: 2026-07-13T22:00:00Z", "created: 2026-07-13T23:00:00Z", 1),
		"future timestamp":          strings.Replace(base, "updated: 2026-07-13T22:30:00Z", "updated: 2026-07-14T22:30:00Z", 1),
		"empty body":                strings.Replace(base, "Use the verified rollback procedure. See [[release-gates]].\n", "", 1),
		"invalid link":              strings.Replace(base, "[[release-gates]]", "[[../secret]]", 1),
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseNote([]byte(source), "release-gates", TrustCurated, fixedNow); err == nil {
				t.Fatal("source unexpectedly parsed")
			}
		})
	}
}

func TestParseWorkingAgentRequiresReviewDateButAllowsExpiredSource(t *testing.T) {
	source := validSource("working-note", TrustWorking, "agent:claude")
	withoutReview := strings.Replace(source, "review_by: 2026-08-13\n", "", 1)
	if _, err := ParseNote([]byte(withoutReview), "working-note", TrustWorking, fixedNow); err == nil {
		t.Fatal("agent note without review_by parsed")
	}

	expiredSource := strings.Replace(source, "review_by: 2026-08-13", "review_by: 2026-07-12", 1)
	note, err := ParseNote([]byte(expiredSource), "working-note", TrustWorking, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if !note.Expired {
		t.Fatal("expired source was not marked expired")
	}
}

func TestBuildAgentNoteEnforcesTrustProvenanceReviewAndAuthor(t *testing.T) {
	draft := AgentDraft{
		Slug:       "rollback-hypothesis",
		Title:      "Rollback hypothesis",
		Type:       TypeHypothesis,
		Author:     "agent:chatgpt",
		Provenance: "Owner discussion on 2026-07-13",
		ReviewBy:   "2026-08-13",
		Body:       "Verify against [[release-gates]] before curation.",
	}
	note, err := BuildAgentNote(draft, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if note.Trust != TrustWorking || note.Metadata.Created != fixedNow.Format(time.RFC3339) || note.Metadata.Updated != note.Metadata.Created {
		t.Fatalf("note=%+v", note)
	}
	if strings.Contains(string(RenderNote(note)), "***REDACTED") {
		t.Fatal("valid note was redacted")
	}

	for name, mutate := range map[string]func(*AgentDraft){
		"owner author":       func(d *AgentDraft) { d.Author = AuthorOwner },
		"missing provenance": func(d *AgentDraft) { d.Provenance = "" },
		"missing review":     func(d *AgentDraft) { d.ReviewBy = "" },
		"past review":        func(d *AgentDraft) { d.ReviewBy = "2026-07-12" },
		"distant review":     func(d *AgentDraft) { d.ReviewBy = "2028-01-01" },
		"invalid type":       func(d *AgentDraft) { d.Type = NoteType("decision") },
		"empty body":         func(d *AgentDraft) { d.Body = "" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := draft
			mutate(&candidate)
			if _, err := BuildAgentNote(candidate, nil, fixedNow); err == nil {
				t.Fatal("invalid draft unexpectedly succeeded")
			}
		})
	}
}

func TestBuildAgentNoteRejectsSecretCanaryBeforeRendering(t *testing.T) {
	draft := AgentDraft{
		Slug:       "credential-hypothesis",
		Title:      "Credential hypothesis",
		Type:       TypeHypothesis,
		Author:     "agent:chatgpt",
		Provenance: "manual observation",
		ReviewBy:   "2026-08-13",
		Body:       "Observed token github_pat_0123456789abcdefghijklmnopQRSTUV.",
	}
	if _, err := BuildAgentNote(draft, nil, fixedNow); err == nil || !strings.Contains(strings.ToLower(err.Error()), "secret") {
		t.Fatalf("secret draft error=%v", err)
	}
}

func TestBuildAgentNotePreservesCreatedAndRejectsCrossAuthorUpdate(t *testing.T) {
	existing, err := ParseNote([]byte(validSource("working-note", TrustWorking, "agent:chatgpt")), "working-note", TrustWorking, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	draft := AgentDraft{
		Slug:       "working-note",
		Title:      "Updated note",
		Type:       TypeNote,
		Author:     "agent:chatgpt",
		Provenance: "new owner message",
		ReviewBy:   "2026-08-20",
		Body:       "Updated bounded body.",
	}
	updatedAt := fixedNow.Add(time.Hour)
	updated, err := BuildAgentNote(draft, &existing, updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Metadata.Created != existing.Metadata.Created || updated.Metadata.Updated != updatedAt.Format(time.RFC3339) {
		t.Fatalf("timestamps existing=%+v updated=%+v", existing.Metadata, updated.Metadata)
	}

	draft.Author = "agent:claude"
	if _, err := BuildAgentNote(draft, &existing, updatedAt); err == nil {
		t.Fatal("cross-author update unexpectedly succeeded")
	}
}

func TestRenderParseRoundTripIsDeterministic(t *testing.T) {
	draft := AgentDraft{
		Slug:       "release-hypothesis",
		Title:      "Release hypothesis",
		Type:       TypeHypothesis,
		Author:     "agent:chatgpt",
		Provenance: "PR 4 review",
		ReviewBy:   "2026-08-13",
		Body:       "Compare [[release-gates]] and [[deployment-rollback]].",
	}
	note, err := BuildAgentNote(draft, nil, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	encoded := RenderNote(note)
	parsed, err := ParseNote(encoded, draft.Slug, TrustWorking, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if string(RenderNote(parsed)) != string(encoded) {
		t.Fatalf("round trip differs\nfirst:\n%s\nsecond:\n%s", encoded, RenderNote(parsed))
	}
}

func TestParseNoteEnforcesBodyAndFileBounds(t *testing.T) {
	source := validSource("bounded-note", TrustCurated, AuthorOwner)
	bodyMarker := "Use the verified rollback procedure. See [[release-gates]].\n"
	tooLargeBody := strings.Replace(source, bodyMarker, strings.Repeat("x", MaxBodyBytes+1), 1)
	if _, err := ParseNote([]byte(tooLargeBody), "bounded-note", TrustCurated, fixedNow); err == nil {
		t.Fatal("oversized body parsed")
	}
	if _, err := ParseNote([]byte(strings.Repeat("x", MaxFileBytes+1)), "bounded-note", TrustCurated, fixedNow); err == nil {
		t.Fatal("oversized file parsed")
	}
}
