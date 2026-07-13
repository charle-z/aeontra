package mcpserver

import (
	"encoding/json"

	"github.com/charle-z/mcp-devbox/internal/mcpserver/catalog"
)

// object builds a JSON-Schema object node.
func object(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func strArrProp(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

// add registers a tool definition and handler.
func (s *Server) add(name, desc string, schema map[string]any, h func(json.RawMessage) (string, error)) {
	s.table[name] = toolEntry{
		def: toolDef{
			Name:        name,
			Description: desc,
			InputSchema: schema,
			Version:     defaultToolContractVersion,
		},
		handler: h,
	}
	s.order = append(s.order, name)
}

// addCatalogTool adapts one declarative domain registration into the server-owned
// registry. The server remains responsible for annotations, ordering, dispatch,
// and the shared policy-backed service handlers.
func (s *Server) addCatalogTool(tool catalog.Tool) {
	s.table[tool.Name] = toolEntry{
		def: toolDef{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			Version:     tool.Version,
		},
		handler: tool.Handler,
	}
	s.order = append(s.order, tool.Name)
}

// addAlias exposes a stable recommended name while preserving the exact handler,
// input schema, and policy path of an existing compatibility name.
func (s *Server) addAlias(name, target, desc string) {
	original := s.table[target]
	original.def.Name = name
	original.def.Description = desc
	s.table[name] = original
	s.order = append(s.order, name)
}

func (s *Server) addCatalogAlias(alias catalog.Alias) {
	s.addAlias(alias.Name, alias.Target, alias.Description)
}

// annotate attaches the same behavior hints to each named tool (no-op for names that
// were not registered, e.g. a tool gated off by configuration).
func (s *Server) annotate(hints map[string]any, names ...string) {
	for _, n := range names {
		if e, ok := s.table[n]; ok {
			e.def.Annotations = hints
			s.table[n] = e
		}
	}
}

// register wires every L1 tool. Descriptions are written for the orchestrating
// agent; all enforcement happens in the tool/policy layer regardless of how a
// client calls them.
func (s *Server) register() {
	catalog.RegisterRuntime(s.addCatalogTool, func() (any, error) {
		return s.RuntimeInfo()
	})

	catalog.RegisterRepositoryReads(s.addCatalogTool, s.svc)

	catalog.RegisterRepositoryWrites(s.addCatalogTool, s.svc)

	catalog.RegisterExecution(s.addCatalogTool, s.svc)

	catalog.RegisterPrivileged(s.addCatalogTool, s.svc)

	catalog.RegisterPlatformCore(s.addCatalogTool, s.svc)

	catalog.RegisterValidationRunnerPlatform(s.addCatalogTool, s.svc)

	catalog.RegisterPlatformAppPreview(s.addCatalogTool, platformAppPreviewAdapter{service: s.svc})

	catalog.RegisterPlatformDeployment(s.addCatalogTool, s.svc)

	catalog.RegisterPlatformEnvironment(s.addCatalogTool, s.svc)

	catalog.RegisterGitReads(s.addCatalogTool, s.svc)

	catalog.RegisterGitAcquisition(s.addCatalogTool, s.svc)

	catalog.RegisterGitFastForward(s.addCatalogTool, s.svc)

	catalog.RegisterGitPublication(s.addCatalogTool, s.svc)

	catalog.RegisterSourceRepoCreation(s.addCatalogTool, s.svc)

	catalog.RegisterSourceRepoInfo(s.addCatalogTool, s.svc)

	catalog.RegisterGitRemoteManagement(s.addCatalogTool, s.svc)

	catalog.RegisterValidation(s.addCatalogTool, s.svc)

	catalog.RegisterGitCommit(s.addCatalogTool, s.svc)

	catalog.RegisterMemory(s.addCatalogTool, s.svc)

	catalog.RegisterNotes(s.addCatalogTool, s.svc)

	catalog.RegisterHandoff(s.addCatalogTool, s.svc)

	catalog.RegisterAliases(s.addCatalogAlias)

	catalog.RegisterAnnotations(s.annotate)
}
