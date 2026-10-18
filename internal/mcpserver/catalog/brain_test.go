package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
)

type fakeBrainService struct {
	searchQuery string
	searchTopK  int
	readSlug    string
	writeDraft  brainpkg.AgentDraft
	indexAction string
	contextMax  int
	err         error
}

func (f *fakeBrainService) BrainSearch(_ context.Context, stringValue string, intValue int) ([]brainpkg.SearchResult, error) {
	f.searchQuery, f.searchTopK = stringValue, intValue
	if f.err != nil {
		return nil, f.err
	}
	return []brainpkg.SearchResult{{Slug: "search-result", Trust: brainpkg.TrustCurated, Title: "Search result"}}, nil
}

func (f *fakeBrainService) BrainRead(_ context.Context, slug string) (brainpkg.ReadResult, error) {
	f.readSlug = slug
	if f.err != nil {
		return brainpkg.ReadResult{}, f.err
	}
	return brainpkg.ReadResult{Note: brainpkg.Note{Metadata: brainpkg.Metadata{Slug: slug}, Links: []string{}}, Backlinks: []string{}}, nil
}

func (f *fakeBrainService) BrainWrite(_ context.Context, draft brainpkg.AgentDraft) (brainpkg.Note, error) {
	f.writeDraft = draft
	if f.err != nil {
		return brainpkg.Note{}, f.err
	}
	return brainpkg.Note{Metadata: brainpkg.Metadata{Slug: draft.Slug, Title: draft.Title}, Body: draft.Body, Trust: brainpkg.TrustWorking, Links: []string{}}, nil
}

func (f *fakeBrainService) BrainIndex(_ context.Context, action string) (brainpkg.IndexStatus, error) {
	f.indexAction = action
	if f.err != nil {
		return brainpkg.IndexStatus{}, f.err
	}
	return brainpkg.IndexStatus{Ready: true, SchemaVersion: brainpkg.IndexSchemaVersion, NoteCount: 2}, nil
}

func (f *fakeBrainService) BrainContext(_ context.Context, limit int) (string, error) {
	f.contextMax = limit
	if f.err != nil {
		return "", f.err
	}
	return "one-line digest", nil
}

func registeredBrainTools(t *testing.T, service BrainService) map[string]Tool {
	t.Helper()
	tools := map[string]Tool{}
	RegisterBrain(func(tool Tool) {
		if _, exists := tools[tool.Name]; exists {
			t.Fatalf("duplicate tool %s", tool.Name)
		}
		tools[tool.Name] = tool
	}, service)
	if len(tools) != 5 {
		t.Fatalf("registered tools=%d", len(tools))
	}
	return tools
}

func TestRegisterBrainHandlersDecodeAndEncodeExactContracts(t *testing.T) {
	service := &fakeBrainService{}
	tools := registeredBrainTools(t, service)

	search, err := tools["brain_search"].Handler(json.RawMessage(`{"query":"release gates","top_k":7}`))
	if err != nil || service.searchQuery != "release gates" || service.searchTopK != 7 || !strings.Contains(search, `"slug":"search-result"`) {
		t.Fatalf("search=%q query=%q topK=%d err=%v", search, service.searchQuery, service.searchTopK, err)
	}

	read, err := tools["brain_read"].Handler(json.RawMessage(`{"slug":"safe-note"}`))
	if err != nil || service.readSlug != "safe-note" || !strings.Contains(read, `"backlinks":[]`) {
		t.Fatalf("read=%q slug=%q err=%v", read, service.readSlug, err)
	}

	writeInput := brainpkg.AgentDraft{
		Slug: "working-note", Title: "Working note", Type: brainpkg.TypeHypothesis,
		Author: "agent:test", Provenance: "owner message", ReviewBy: "2026-08-13", Body: "bounded body",
	}
	writeJSON, err := json.Marshal(map[string]any{
		"slug": writeInput.Slug, "title": writeInput.Title, "type": writeInput.Type,
		"author": writeInput.Author, "provenance": writeInput.Provenance,
		"review_by": writeInput.ReviewBy, "body": writeInput.Body,
	})
	if err != nil {
		t.Fatal(err)
	}
	written, err := tools["brain_write"].Handler(writeJSON)
	if err != nil || !reflect.DeepEqual(service.writeDraft, writeInput) || !strings.Contains(written, `"slug":"working-note"`) {
		t.Fatalf("written=%q draft=%+v err=%v", written, service.writeDraft, err)
	}

	indexed, err := tools["brain_index"].Handler(json.RawMessage(`{"action":"reindex"}`))
	if err != nil || service.indexAction != "reindex" || !strings.Contains(indexed, `"note_count":2`) {
		t.Fatalf("indexed=%q action=%q err=%v", indexed, service.indexAction, err)
	}

	digest, err := tools["brain_context"].Handler(json.RawMessage(`{"limit":9}`))
	if err != nil || service.contextMax != 9 || digest != "one-line digest" {
		t.Fatalf("digest=%q limit=%d err=%v", digest, service.contextMax, err)
	}
}

func TestBrainHandlersUseDefaultsAndStrictSingleObjectDecoding(t *testing.T) {
	service := &fakeBrainService{}
	tools := registeredBrainTools(t, service)

	if _, err := tools["brain_search"].Handler(json.RawMessage(`{"query":"test"}`)); err != nil || service.searchTopK != 0 {
		t.Fatalf("default top_k=%d err=%v", service.searchTopK, err)
	}
	if output, err := tools["brain_context"].Handler(nil); err != nil || service.contextMax != 0 || output != "one-line digest" {
		t.Fatalf("empty context output=%q limit=%d err=%v", output, service.contextMax, err)
	}

	for name, input := range map[string]string{
		"unknown field":   `{"query":"test","path":"/private"}`,
		"multiple values": `{"query":"test"}{"query":"other"}`,
		"wrong type":      `{"query":7}`,
		"malformed":       `{"query":`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tools["brain_search"].Handler(json.RawMessage(input)); err == nil {
				t.Fatal("invalid input unexpectedly succeeded")
			}
		})
	}
}

func TestBrainHandlersPropagateCapabilityErrorsWithoutOutput(t *testing.T) {
	service := &fakeBrainService{err: errors.New("brain unavailable")}
	tools := registeredBrainTools(t, service)
	calls := map[string]json.RawMessage{
		"brain_search":  json.RawMessage(`{"query":"test"}`),
		"brain_read":    json.RawMessage(`{"slug":"safe-note"}`),
		"brain_write":   json.RawMessage(`{"slug":"safe-note","title":"Safe","type":"note","author":"agent:test","provenance":"source","review_by":"2026-08-13","body":"body"}`),
		"brain_index":   json.RawMessage(`{"action":"status"}`),
		"brain_context": json.RawMessage(`{}`),
	}
	for name, input := range calls {
		output, err := tools[name].Handler(input)
		if err == nil || output != "" || !strings.Contains(err.Error(), "brain unavailable") {
			t.Fatalf("%s output=%q err=%v", name, output, err)
		}
	}
}

func TestBrainSchemasAreClosedAndBoundedAtRegistration(t *testing.T) {
	tools := registeredBrainTools(t, &fakeBrainService{})
	for name, tool := range tools {
		if tool.Version != "1" || tool.InputSchema["type"] != "object" || tool.InputSchema["additionalProperties"] != false {
			t.Fatalf("%s contract=%#v", name, tool)
		}
	}
	writeProps := tools["brain_write"].InputSchema["properties"].(map[string]any)
	if writeProps["slug"].(map[string]any)["maxLength"] != brainpkg.MaxSlugBytes || writeProps["body"].(map[string]any)["maxLength"] != brainpkg.MaxBodyBytes {
		t.Fatalf("write bounds=%#v", writeProps)
	}
	searchProps := tools["brain_search"].InputSchema["properties"].(map[string]any)
	if searchProps["query"].(map[string]any)["maxLength"] != brainpkg.MaxQueryBytes || searchProps["top_k"].(map[string]any)["maximum"] != brainpkg.MaxTopK {
		t.Fatalf("search bounds=%#v", searchProps)
	}
}
