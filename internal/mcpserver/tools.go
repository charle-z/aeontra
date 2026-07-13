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

// annotateTools labels every tool with MCP behavior hints so clients can distinguish
// safe reads from consequential actions. Labeling is HONEST: side-effecting tools are
// never marked read-only. This mainly stops clients (e.g. ChatGPT) from over-blocking
// harmless read-only tools like git_status/list_dir.
func (s *Server) annotateTools() {
	localRead := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	externalRead := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}
	localWrite := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false}
	externalWrite := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": true}
	externalIdempotentWrite := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}
	localDestructive := map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": false, "openWorldHint": false}
	externalDestructive := map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": false, "openWorldHint": true}
	// Local, side-effect-free reads.
	s.annotate(localRead,
		"system_runtime_info", "build_context_pack", "list_dir", "repo_list", "read_file", "read_many_files",
		"search_code", "git_status", "repo_status", "git_diff", "repo_diff", "repo_fast_forward_preview", "repo_remote_preview", "privileged_task_preview", "project_validation_preview", "memory_read", "notes_list", "notes_read", "notes_write_preview", "sandbox_status")
	// Read-only, but reaching external services (GitHub / Coolify APIs).
	s.annotate(externalRead,
		"github_repo_info", "source_repo_info", "source_repo_create_preview", "repo_publish_preview", "coolify_list_apps", "platform_apps_list", "coolify_app_status", "platform_app_status", "coolify_app_logs", "platform_app_logs", "coolify_deployment_status", "platform_deployment_status", "platform_app_create_preview", "platform_validation_runner_create_preview", "platform_deploy_preview", "platform_deploy_without_cache_preview")
	// Additive/local writes: not read-only, but not destructive (no data loss).
	s.annotate(localWrite,
		"create_file", "git_commit")
	// External writes are consequential and open-world, but not inherently destructive.
	s.annotate(externalWrite,
		"git_clone", "git_push", "repo_publish", "github_create_repo", "source_repo_create",
		"coolify_create_app", "platform_app_create", "platform_validation_runner_create")
	s.annotate(externalIdempotentWrite, "repo_fetch")
	s.annotate(localWrite, "repo_fast_forward")
	s.annotate(localWrite, "notes_write")
	// These tools can replace/delete content or perform effects the server cannot
	// characterize as additive, so clients must see truthful destructive hints.
	s.annotate(localDestructive, "apply_patch", "memory_write", "memory_update_handoff", "repo_remote_set", "sandbox_exec")
	s.annotate(externalDestructive, "run_command", "run_tests", "coolify_deploy", "platform_deploy", "platform_deploy_without_cache", "coolify_set_env", "privileged_task_execute", "project_validation_execute")
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

	s.add("repo_remote_preview",
		"Create a read-only, exact, expiring and single-use plan to add or update one named Git remote in a jailed repository. The destination must be credential-free and stay under configured GITHUB_OWNER.",
		object(map[string]any{
			"repo":       strProp("repository directory, absolute or relative to the workspace root"),
			"remote":     strProp("remote name, defaults to origin"),
			"repository": strProp("repository name under configured owner, or an allowed credential-free HTTPS/SSH GitHub URL"),
		}, "repo", "repository"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Repo       string `json:"repo"`
				Remote     string `json:"remote"`
				Repository string `json:"repository"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.RepoRemotePreview(p.Repo, p.Remote, p.Repository)
		})

	s.add("repo_remote_set",
		"Execute one reviewed repo_remote_preview plan. It revalidates the current remote state and runs exactly git remote add or git remote set-url; requires approval in ask mode.",
		object(map[string]any{
			"plan_id": strProp("plan id returned by repo_remote_preview"),
			"approve": boolProp("execute the remote plan when approval is required"),
		}, "plan_id"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.RepoRemoteSet(p.PlanID, p.Approve)
		})

	catalog.RegisterValidation(s.addCatalogTool, s.svc)

	s.add("git_commit",
		"Stage all changes and commit them in the root or optional selected repo. Write action: denied in read-only; in ask mode set approve=true. Does not push.",
		object(map[string]any{
			"message": strProp("commit message"),
			"approve": boolProp("commit even when approval is required"),
			"repo":    strProp("optional repo directory, absolute or relative to the workspace root"),
		}, "message"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Message string `json:"message"`
				Approve bool   `json:"approve"`
				Repo    string `json:"repo"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.GitCommitIn(p.Repo, p.Message, p.Approve)
		})

	catalog.RegisterMemory(s.addCatalogTool, s.svc)

	catalog.RegisterNotes(s.addCatalogTool, s.svc)

	catalog.RegisterHandoff(s.addCatalogTool, s.svc)

	// Compatibility names remain available. Recommended names share the exact same
	// handler and schema, so aliases cannot bypass or duplicate policy enforcement.
	s.addAlias("repo_list", "list_dir", "List one jailed repository directory without reading file contents; equivalent to list_dir.")
	s.addAlias("repo_status", "git_status", "Show read-only status for one jailed repository; equivalent to git_status.")
	s.addAlias("repo_diff", "git_diff", "Show a read-only diff for one jailed repository; equivalent to git_diff.")
	s.addAlias("source_repo_info", "github_repo_info", "Read metadata for a repository under the configured source-host owner; equivalent to github_repo_info and performs an external read.")
	s.addAlias("source_repo_create", "github_create_repo", "Create a repository under the configured source-host owner; equivalent to github_create_repo and performs an external write requiring approval in ask mode.")
	s.addAlias("repo_publish", "git_push", "Publish one local branch to one named remote; equivalent to git_push and performs an external write requiring approval in ask mode.")
	s.addAlias("platform_apps_list", "coolify_list_apps", "List applications from the configured deployment platform; equivalent to coolify_list_apps and performs an external read.")
	s.addAlias("platform_app_status", "coolify_app_status", "Read one application from the configured deployment platform; equivalent to coolify_app_status and performs an external read.")
	s.addAlias("platform_app_logs", "coolify_app_logs", "Read bounded application logs from the configured deployment platform; equivalent to coolify_app_logs and performs an external read.")
	s.addAlias("platform_deployment_status", "coolify_deployment_status", "Read one deployment from the configured deployment platform; equivalent to coolify_deployment_status and performs an external read.")
	s.addAlias("platform_app_create", "coolify_create_app", "Create an application on the configured deployment platform; equivalent to coolify_create_app and performs an external write requiring approval in ask mode.")
	s.addAlias("platform_deploy", "coolify_deploy", "Trigger a deployment on the configured platform; equivalent to coolify_deploy and performs an external write requiring approval in ask mode.")

	s.annotateTools()
}
