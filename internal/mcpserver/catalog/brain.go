package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
)

// BrainService is the isolated capability contract required by the five Brain tools.
type BrainService interface {
	BrainSearch(context.Context, string, int) ([]brainpkg.SearchResult, error)
	BrainRead(context.Context, string) (brainpkg.ReadResult, error)
	BrainWrite(context.Context, brainpkg.AgentDraft) (brainpkg.Note, error)
	BrainIndex(context.Context, string) (brainpkg.IndexStatus, error)
	BrainContext(context.Context, int) (string, error)
}

// RegisterBrain appends exactly five bounded Brain tools. They remain visible while
// disabled so the public catalog is deterministic; the capability fails closed.
func RegisterBrain(register Register, service BrainService) {
	register(Tool{
		Name:        "brain_search",
		Description: "Search the server-anchored Brain on demand using bounded plain-text FTS5 retrieval. Returns at most 20 short redacted matches; it never injects or dumps the complete Brain.",
		InputSchema: closedObject(map[string]any{
			"query": boundedStringProp("plain-text query; FTS operators are treated as text", 1, brainpkg.MaxQueryBytes),
			"top_k": integerProp("maximum results; defaults to 5", 1, brainpkg.MaxTopK),
		}, "query"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Query string `json:"query"`
				TopK  int    `json:"top_k"`
			}
			if err := decodeStrict(arguments, &params); err != nil {
				return "", err
			}
			result, err := service.BrainSearch(context.Background(), params.Query, params.TopK)
			return encodeResult(result, err)
		},
	})

	register(Tool{
		Name:        "brain_read",
		Description: "Read one validated redacted Brain note by strict slug with bounded backlinks. Curated and working trust metadata remain explicit.",
		InputSchema: closedObject(map[string]any{
			"slug": slugProp("strict kebab-case Brain note slug"),
		}, "slug"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Slug string `json:"slug"`
			}
			if err := decodeStrict(arguments, &params); err != nil {
				return "", err
			}
			result, err := service.BrainRead(context.Background(), params.Slug)
			return encodeResult(result, err)
		},
	})

	register(Tool{
		Name:        "brain_write",
		Description: "Create or update only working/<slug>.md with strict agent author, provenance and review date. Curated memory, paths, timestamps and trust level cannot be supplied; secret-like content is rejected.",
		InputSchema: closedObject(map[string]any{
			"slug":       slugProp("strict kebab-case working note slug"),
			"title":      boundedStringProp("short note title", 1, brainpkg.MaxTitleBytes),
			"type":       enumStringProp("explicit note posture", "fact", "note", "feedback", "reference", "hypothesis"),
			"author":     patternedStringProp("declared agent author; never owner", `^agent:[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$`, 7, 70),
			"provenance": boundedStringProp("evidence or source from which this note follows", 1, brainpkg.MaxProvenanceBytes),
			"review_by":  patternedStringProp("mandatory review/expiry date in YYYY-MM-DD", `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`, 10, 10),
			"body":       boundedStringProp("Markdown body with optional [[slug]] links", 1, brainpkg.MaxBodyBytes),
		}, "slug", "title", "type", "author", "provenance", "review_by", "body"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Slug       string            `json:"slug"`
				Title      string            `json:"title"`
				Type       brainpkg.NoteType `json:"type"`
				Author     string            `json:"author"`
				Provenance string            `json:"provenance"`
				ReviewBy   string            `json:"review_by"`
				Body       string            `json:"body"`
			}
			if err := decodeStrict(arguments, &params); err != nil {
				return "", err
			}
			result, err := service.BrainWrite(context.Background(), brainpkg.AgentDraft{
				Slug:       params.Slug,
				Title:      params.Title,
				Type:       params.Type,
				Author:     params.Author,
				Provenance: params.Provenance,
				ReviewBy:   params.ReviewBy,
				Body:       params.Body,
			})
			return encodeResult(result, err)
		},
	})

	register(Tool{
		Name:        "brain_index",
		Description: "Return disposable Brain index status or rebuild it transactionally from Markdown truth. Reindex changes only the derived local cache and is bounded and idempotent.",
		InputSchema: closedObject(map[string]any{
			"action": enumStringProp("status or reindex", "status", "reindex"),
		}, "action"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Action string `json:"action"`
			}
			if err := decodeStrict(arguments, &params); err != nil {
				return "", err
			}
			result, err := service.BrainIndex(context.Background(), params.Action)
			return encodeResult(result, err)
		},
	})

	register(Tool{
		Name:        "brain_context",
		Description: "Return an on-demand startup digest of at most 16 notes and 4 KiB, curated first and without full note bodies. It never injects the complete Brain automatically.",
		InputSchema: closedObject(map[string]any{
			"limit": integerProp("maximum one-line note summaries; defaults to 8", 1, brainpkg.MaxContextNotes),
		}),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Limit int `json:"limit"`
			}
			if err := decodeStrict(arguments, &params); err != nil {
				return "", err
			}
			return service.BrainContext(context.Background(), params.Limit)
		},
	})
}

func decodeStrict(arguments json.RawMessage, target any) error {
	if len(bytes.TrimSpace(arguments)) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("invalid arguments: expected one JSON object")
	}
	return nil
}

func encodeResult(value any, operationErr error) (string, error) {
	if operationErr != nil {
		return "", operationErr
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", errors.New("brain result encoding failed")
	}
	return string(encoded), nil
}
