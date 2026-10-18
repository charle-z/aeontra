package mcpserver

import (
	"fmt"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
)

// RuntimeInfo is the public, non-sensitive identity of the live MCP process and its
// registered tool catalog. It intentionally excludes configuration, roots, clients,
// tokens, providers, and deployment-platform details.
type RuntimeInfo struct {
	Status          string `json:"status"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocol_version"`
	Commit          string `json:"commit"`
	BuiltAt         string `json:"built_at"`
	ToolCount       int    `json:"tool_count"`
	CatalogHash     string `json:"catalog_hash"`
}

// RuntimeInfo returns a value-only snapshot suitable for safe diagnostics.
func (s *Server) RuntimeInfo() (RuntimeInfo, error) {
	build := buildinfo.Current()
	catalog, err := s.CatalogInfo()
	if err != nil {
		return RuntimeInfo{}, err
	}
	return RuntimeInfo{
		Status:          "ok",
		Version:         build.Version,
		ProtocolVersion: build.ProtocolVersion,
		Commit:          build.Commit,
		BuiltAt:         build.BuiltAt,
		ToolCount:       catalog.ToolCount,
		CatalogHash:     catalog.Hash,
	}, nil
}

func (s *Server) mustRuntimeInfo() RuntimeInfo {
	info, err := s.RuntimeInfo()
	if err != nil {
		panic(fmt.Sprintf("invalid MCP catalog: %v", err))
	}
	return info
}
