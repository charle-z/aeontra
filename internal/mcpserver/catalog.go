package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const defaultToolContractVersion = "1"

// CatalogTool is the deterministic contract identity of one MCP tool. Descriptions
// and handlers are intentionally excluded: they can change without changing the
// machine-facing wire contract.
type CatalogTool struct {
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	InputSchema map[string]any `json:"input_schema"`
	Annotations map[string]any `json:"annotations"`
}

// CatalogSnapshot is a stable, value-only view of the registered MCP tool surface.
type CatalogSnapshot struct {
	ToolCount int           `json:"tool_count"`
	Hash      string        `json:"catalog_hash"`
	Tools     []CatalogTool `json:"tools"`
}

// CatalogInfo returns a name-sorted manifest and a SHA-256 hash of its canonical
// JSON representation. Go's JSON encoder sorts string map keys; sorting tools by
// name removes registration-order dependence.
func (s *Server) CatalogInfo() (CatalogSnapshot, error) {
	names := make([]string, 0, len(s.table))
	for name := range s.table {
		names = append(names, name)
	}
	sort.Strings(names)

	tools := make([]CatalogTool, 0, len(names))
	for _, name := range names {
		entry := s.table[name]
		version := strings.TrimSpace(entry.def.Version)
		if version == "" {
			return CatalogSnapshot{}, fmt.Errorf("tool %q has no contract version", name)
		}
		schema, err := cloneJSONMap(entry.def.InputSchema)
		if err != nil {
			return CatalogSnapshot{}, fmt.Errorf("clone schema for %q: %w", name, err)
		}
		annotations, err := cloneJSONMap(entry.def.Annotations)
		if err != nil {
			return CatalogSnapshot{}, fmt.Errorf("clone annotations for %q: %w", name, err)
		}
		tools = append(tools, CatalogTool{
			Name:        name,
			Version:     version,
			InputSchema: schema,
			Annotations: annotations,
		})
	}

	encoded, err := json.Marshal(tools)
	if err != nil {
		return CatalogSnapshot{}, fmt.Errorf("encode tool catalog: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return CatalogSnapshot{
		ToolCount: len(tools),
		Hash:      "sha256:" + hex.EncodeToString(sum[:]),
		Tools:     tools,
	}, nil
}

func cloneJSONMap(input map[string]any) (map[string]any, error) {
	if input == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var output map[string]any
	if err := json.Unmarshal(encoded, &output); err != nil {
		return nil, err
	}
	return output, nil
}
