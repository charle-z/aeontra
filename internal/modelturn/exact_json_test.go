package modelturn

import (
	"encoding/json"
	"testing"
)

func TestCanonicalJSONMatchesJavaScriptEscapingForHTMLCharacters(t *testing.T) {
	raw := json.RawMessage(`{"amp":"&","greater":">","less":"<","nested":{"z":1,"a":2}}`)
	canonical, err := canonicalJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"amp":"&","greater":">","less":"<","nested":{"a":2,"z":1}}`
	if string(canonical) != want {
		t.Fatalf("canonical=%s want=%s", canonical, want)
	}
	exact, err := exactJSON(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if string(exact) != want {
		t.Fatalf("exact=%s want=%s", exact, want)
	}
}

func TestExactJSONRejectsValidButNonCanonicalPayload(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(` {"a":1}`),
		json.RawMessage(`{"z":1,"a":2}`),
		json.RawMessage(`{"less":"\u003c"}`),
	} {
		if _, err := exactJSON(raw); err == nil {
			t.Fatalf("non-canonical payload accepted: %s", raw)
		}
	}
}
