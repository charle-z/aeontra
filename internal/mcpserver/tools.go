package mcpserver

import (
	"encoding/json"
	"fmt"

	"github.com/charle-z/mcp-devbox/internal/mcpserver/catalog"
	"github.com/charle-z/mcp-devbox/internal/tools"
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

	s.add("apply_patch",
		"Apply a unified diff (patch-first). Optional repo makes patch paths relative to that jailed repo. Validated with 'git apply --check' first; targets jailed and secret-protected. In ask mode, set approve=true to apply after review.",
		object(map[string]any{
			"patch":   strProp("unified diff text"),
			"approve": boolProp("apply even when approval is required"),
			"repo":    strProp("optional repo directory, absolute or relative to the workspace root"),
		}, "patch"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Patch   string `json:"patch"`
				Approve bool   `json:"approve"`
				Repo    string `json:"repo"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.ApplyPatchIn(p.Repo, p.Patch, p.Approve)
		})

	s.add("create_file",
		"Create a NEW file (patch-first: built as a diff and validated; refuses to overwrite — use apply_patch to modify). Jailed and secret-protected. In ask mode set approve=true.",
		object(map[string]any{
			"path":    strProp("new file path relative to the project root"),
			"content": strProp("file content"),
			"approve": boolProp("create even when approval is required"),
			"repo":    strProp("optional repo directory, absolute or relative to the workspace root"),
		}, "path", "content"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Path    string `json:"path"`
				Content string `json:"content"`
				Approve bool   `json:"approve"`
				Repo    string `json:"repo"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.CreateFileIn(p.Repo, p.Path, p.Content, p.Approve)
		})

	s.add("run_command",
		"Run a single allowlisted program with args (e.g. [\"go\",\"vet\",\"./...\"]). NOT a shell: only allowlisted programs, no metacharacters. Optional cwd is jailed under the workspace. Mode-gated (read-only denies; ask needs approve=true). Output redacted.",
		object(map[string]any{
			"command": strArrProp("program and arguments; command[0] is the program"),
			"approve": boolProp("run even when approval is required"),
			"cwd":     strProp("optional working directory, absolute or relative to the workspace root"),
		}, "command"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Command []string `json:"command"`
				Approve bool     `json:"approve"`
				CWD     string   `json:"cwd"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			if len(p.Command) == 0 {
				return "", fmt.Errorf("command must have at least the program name")
			}
			return s.svc.RunCommandIn(p.Command[0], p.Command[1:], p.Approve, p.CWD)
		})

	s.add("sandbox_status",
		"Report L3 sandbox availability. Diagnostic only: unavailable by default, no free terminal, no Docker socket in the public MCP container.",
		object(map[string]any{}),
		func(json.RawMessage) (string, error) { return s.svc.SandboxStatus(), nil })

	s.add("sandbox_exec",
		"Run an ARBITRARY command INSIDE the L3 sandbox (contained: no network, read-only rootfs, workspace-only, resource-limited). NOT allowlist-limited — the sandbox contains it. Requires a configured backend (MCP_DEVBOX_SANDBOX=docker on a host with Docker); denied in read-only; set approve=true in ask mode.",
		object(map[string]any{
			"command": strArrProp("program and arguments; command[0] is the program"),
			"approve": boolProp("run even when approval is required"),
		}, "command"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Command []string `json:"command"`
				Approve bool     `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			if len(p.Command) == 0 {
				return "", fmt.Errorf("command must have at least the program name")
			}
			return s.svc.SandboxExec(p.Command, p.Approve)
		})

	s.add("privileged_task_preview",
		"Preview one administrator-enabled, server-defined privileged profile. The client supplies only a profile name and narrow validated parameters, never an executable, argv, or shell string. Returns the exact command, jailed working directory, network/filesystem posture, effect, risk, short-lived plan id and expiry. Disabled by default.",
		object(map[string]any{
			"repo":    strProp("jailed repository directory when the selected profile applies to a repository"),
			"profile": strProp("one approved server-defined profile name"),
			"params": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
				"description":          "narrow profile parameters such as remote, branch, or allowlisted service name",
			},
		}, "profile"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Repo    string            `json:"repo"`
				Profile string            `json:"profile"`
				Params  map[string]string `json:"params"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PrivilegedTaskPreview(p.Repo, p.Profile, p.Params)
		})

	s.add("privileged_task_execute",
		"Execute one unexpired unused privileged_task_preview plan after policy approval. The exact server-generated command, jailed cwd, timeout and profile remain fixed. Docker profiles fail securely when safe containment is unavailable; no free host terminal is exposed.",
		object(map[string]any{
			"plan_id": strProp("plan id returned by privileged_task_preview"),
			"approve": boolProp("execute the privileged profile when approval is required"),
		}, "plan_id"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PrivilegedTaskExecute(p.PlanID, p.Approve)
		})

	s.add("coolify_deploy",
		"Execute one previously reviewed platform_deploy_preview plan after revalidating the application repository, branch and expected commit. The plan is expiring and single-use; requires approval in ask mode; token is never exposed.",
		object(map[string]any{
			"plan_id": strProp("plan id returned by platform_deploy_preview"),
			"approve": boolProp("execute the deployment plan when approval is required"),
		}, "plan_id"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PlatformDeploy(p.PlanID, p.Approve)
		})

	s.add("coolify_list_apps",
		"List applications on the configured Coolify instance. Disabled unless COOLIFY_URL + COOLIFY_API_TOKEN are set. Token is never exposed.",
		object(map[string]any{}),
		func(json.RawMessage) (string, error) { return s.svc.PlatformAppsList() })

	s.add("coolify_app_status",
		"Read one Coolify application by uuid. Disabled unless COOLIFY_URL + COOLIFY_API_TOKEN are set; COOLIFY_ALLOWED_APPS is enforced when configured.",
		object(map[string]any{"app": strProp("Coolify application uuid")}, "app"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				App string `json:"app"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PlatformAppStatus(p.App)
		})

	s.add("coolify_deployment_status",
		"Read one Coolify deployment by UUID and return a safe summary containing status, commit, timestamps, and application name. Token is never exposed.",
		object(map[string]any{"deployment": strProp("Coolify deployment UUID")}, "deployment"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Deployment string `json:"deployment"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PlatformDeploymentStatus(p.Deployment)
		})

	s.add("coolify_app_logs",
		"Read the latest bounded application logs from Coolify. Disabled unless COOLIFY_URL + COOLIFY_API_TOKEN are set; COOLIFY_ALLOWED_APPS is enforced and secrets are redacted.",
		object(map[string]any{
			"app":   strProp("Coolify application uuid"),
			"lines": intProp("number of log lines from the end, from 1 to 1000; defaults to 100"),
		}, "app"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				App   string `json:"app"`
				Lines int    `json:"lines"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PlatformAppLogs(p.App, p.Lines)
		})

	s.add("coolify_create_app",
		"Execute one previously reviewed platform_app_create_preview plan using the configured server/project/environment. Repository owner, domain, build, port and healthcheck were validated; plan is expiring and single-use; requires approval in ask mode.",
		object(map[string]any{
			"plan_id": strProp("plan id returned by platform_app_create_preview"),
			"approve": boolProp("execute the application creation plan when approval is required"),
		}, "plan_id"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PlatformAppCreate(p.PlanID, p.Approve)
		})

	s.add("platform_validation_runner_create_preview",
		"Plan exactly one private Coolify validation-runner application using the administrator-configured destination and exact mount allowlist. It never deploys or accepts secret values.",
		object(map[string]any{"branch": strProp("source branch; defaults to cubethon-q3")}),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Branch string `json:"branch"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PlatformValidationRunnerCreatePreview(p.Branch)
		})

	s.add("platform_validation_runner_create",
		"Execute one reviewed validation-runner creation plan. It creates one private, non-deployed Coolify application and configures only non-secret runtime variables; explicit approval is required.",
		object(map[string]any{
			"plan_id": strProp("plan id returned by platform_validation_runner_create_preview"),
			"approve": boolProp("execute the reviewed application creation plan"),
		}, "plan_id"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PlatformValidationRunnerCreate(p.PlanID, p.Approve)
		})

	s.add("platform_app_create_preview",
		"Validate a Coolify application definition against configured server/project/environment, GitHub owner and domain allowlist, then create a read-only expiring single-use plan. Required environment variable names are shown; no secret values are accepted or returned.",
		object(map[string]any{
			"name":                 strProp("new application name"),
			"github_repo":          strProp("owner/repo or allowed credential-free GitHub URL"),
			"branch":               strProp("branch, defaults to main"),
			"domain":               strProp("optional domain restricted by COOLIFY_ALLOWED_DOMAINS"),
			"port":                 strProp("optional exposed port from 1 to 65535"),
			"build_pack":           strProp("nixpacks, dockerfile, static, or dockercompose"),
			"healthcheck_path":     strProp("optional absolute HTTP healthcheck path"),
			"healthcheck_interval": intProp("optional healthcheck interval in seconds"),
			"healthcheck_timeout":  intProp("optional healthcheck timeout in seconds"),
			"required_env":         strArrProp("names of required environment variables; never values"),
		}, "name", "github_repo"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Name                string   `json:"name"`
				GitHubRepo          string   `json:"github_repo"`
				Branch              string   `json:"branch"`
				Domain              string   `json:"domain"`
				Port                string   `json:"port"`
				BuildPack           string   `json:"build_pack"`
				HealthcheckPath     string   `json:"healthcheck_path"`
				HealthcheckInterval int      `json:"healthcheck_interval"`
				HealthcheckTimeout  int      `json:"healthcheck_timeout"`
				RequiredEnv         []string `json:"required_env"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PlatformAppCreatePreview(tools.PlatformAppCreateRequest{
				Name: p.Name, GitHubRepo: p.GitHubRepo, Branch: p.Branch, Domain: p.Domain,
				Port: p.Port, BuildPack: p.BuildPack, HealthcheckPath: p.HealthcheckPath,
				HealthcheckInterval: p.HealthcheckInterval, HealthcheckTimeout: p.HealthcheckTimeout,
				RequiredEnv: p.RequiredEnv,
			})
		})

	s.add("platform_deploy_preview",
		"Read one allowed Coolify application and create an expiring single-use deployment plan bound to its repository, branch and expected commit. It does not deploy.",
		object(map[string]any{"app": strProp("Coolify application UUID")}, "app"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				App string `json:"app"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PlatformDeployPreview(p.App)
		})

	s.add("platform_deploy_without_cache_preview",
		"Read one allowed Coolify application and create an expiring single-use force=true deployment plan bound to its repository, branch, and expected commit. It does not deploy.",
		object(map[string]any{"app": strProp("Coolify application UUID")}, "app"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				App string `json:"app"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PlatformDeployWithoutCachePreview(p.App)
		})

	s.add("platform_deploy_without_cache",
		"Execute one reviewed platform_deploy_without_cache_preview plan after revalidating the application repository, branch, and expected commit. It requests Coolify force=true and requires explicit approval in ask mode.",
		object(map[string]any{
			"plan_id": strProp("plan id returned by platform_deploy_without_cache_preview"),
			"approve": boolProp("execute the force=true deployment plan when approval is required"),
		}, "plan_id"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.PlatformDeployWithoutCache(p.PlanID, p.Approve)
		})

	s.add("coolify_set_env",
		"Set environment variables on one Coolify application. Values are sent to Coolify but redacted from output/audit. Denied in read-only; in ask mode set approve=true.",
		object(map[string]any{
			"app":     strProp("Coolify application uuid"),
			"vars":    map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "environment variables to set"},
			"approve": boolProp("set env vars even when approval is required"),
		}, "app", "vars"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				App     string            `json:"app"`
				Vars    map[string]string `json:"vars"`
				Approve bool              `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.CoolifySetEnv(p.App, p.Vars, p.Approve)
		})

	s.add("git_status", "Show git working-tree status (read-only). Optional repo is a jailed directory, useful when the workspace root is /repos.",
		object(map[string]any{"repo": strProp("optional repo directory, absolute or relative to the workspace root")}),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Repo string `json:"repo"`
			}
			_ = json.Unmarshal(a, &p)
			return s.svc.GitStatus(p.Repo)
		})

	s.add("git_diff", "Show a git diff (read-only). Optional repo is a jailed directory, useful when the workspace root is /repos. Optional extra args (e.g. --staged or a pathspec).",
		object(map[string]any{
			"repo": strProp("optional repo directory, absolute or relative to the workspace root"),
			"args": strArrProp("extra git diff arguments"),
		}),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Args []string `json:"args"`
				Repo string   `json:"repo"`
			}
			_ = json.Unmarshal(a, &p)
			return s.svc.GitDiffIn(p.Repo, p.Args...)
		})

	s.add("git_clone",
		"Clone a Git repository into a new simple directory under the workspace root. No embedded credentials in URLs; target cannot escape the jail. Denied in read-only; in ask mode set approve=true.",
		object(map[string]any{
			"url":     strProp("remote Git URL, without embedded credentials"),
			"dir":     strProp("optional simple target directory name under the workspace root; inferred from URL when omitted"),
			"approve": boolProp("clone even when approval is required"),
		}, "url"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				URL     string `json:"url"`
				Dir     string `json:"dir"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.GitClone(p.URL, p.Dir, p.Approve)
		})

	s.add("repo_fetch",
		"Fetch one named remote into one jailed Git repository by running exactly 'git fetch <remote>'. No refspecs or extra arguments are accepted. This external action updates local remote-tracking refs and requires approval in ask mode.",
		object(map[string]any{
			"repo":    strProp("repository directory, absolute or relative to the workspace root"),
			"remote":  strProp("remote name, defaults to origin; option-like names are rejected"),
			"approve": boolProp("execute the fetch when approval is required"),
		}, "repo"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Repo    string `json:"repo"`
				Remote  string `json:"remote"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.RepoFetch(p.Repo, p.Remote, p.Approve)
		})

	s.add("repo_fast_forward_preview",
		"Create a read-only, short-lived, single-use plan for an exact clean-tree fast-forward of the current attached branch to its existing upstream tracking ref. It does not fetch or modify the repository.",
		object(map[string]any{"repo": strProp("repository directory, absolute or relative to the workspace root")}, "repo"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Repo string `json:"repo"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.RepoFastForwardPreview(p.Repo)
		})

	s.add("repo_fast_forward",
		"Execute one previously reviewed, unexpired and unused fast-forward plan using exactly 'git merge --ff-only <upstream>'. Repository, branch, HEAD, target and clean state are revalidated; requires approval in ask mode.",
		object(map[string]any{
			"plan_id": strProp("plan id returned by repo_fast_forward_preview"),
			"approve": boolProp("execute the plan when approval is required"),
		}, "plan_id"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.RepoFastForward(p.PlanID, p.Approve)
		})

	s.add("git_push",
		"Execute a previously reviewed repo_publish_preview plan for one local branch and one named owner-restricted remote. No force, mirror, tags, refspecs, URL remotes, or extra arguments are accepted; requires approval in ask mode.",
		object(map[string]any{
			"plan_id": strProp("plan id returned by repo_publish_preview"),
			"approve": boolProp("execute the publication plan when approval is required"),
		}, "plan_id"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.RepoPublish(p.PlanID, p.Approve)
		})

	s.add("repo_publish_preview",
		"Validate a clean attached current branch and one named credential-free GitHub remote, inspect the exact remote branch state, reject behind/diverged publication, and create a read-only expiring single-use push plan. It does not push.",
		object(map[string]any{
			"repo":   strProp("repository directory, absolute or relative to the workspace root"),
			"remote": strProp("remote name, defaults to origin; URLs and option-like names are rejected"),
			"branch": strProp("branch name, defaults to and must equal the current attached branch"),
		}, "repo"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Repo   string `json:"repo"`
				Remote string `json:"remote"`
				Branch string `json:"branch"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.RepoPublishPreview(p.Repo, p.Remote, p.Branch)
		})

	s.add("github_create_repo",
		"Execute a previously reviewed source_repo_create_preview plan to create one GitHub repository under the configured owner. The plan is exact, expiring and single-use; token is never exposed; requires approval in ask mode.",
		object(map[string]any{
			"plan_id": strProp("plan id returned by source_repo_create_preview"),
			"approve": boolProp("execute the create plan when approval is required"),
		}, "plan_id"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				PlanID  string `json:"plan_id"`
				Approve bool   `json:"approve"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.SourceRepoCreate(p.PlanID, p.Approve)
		})

	s.add("source_repo_create_preview",
		"Check that a repository is absent under the configured GitHub owner and create a read-only, exact, expiring and single-use creation plan. Private is the default; public must be explicit. Nothing is created.",
		object(map[string]any{
			"name":        strProp("new repository name under the configured owner"),
			"visibility":  strProp("optional private or public visibility; defaults to configured private posture"),
			"description": strProp("optional repository description; redacted before planning"),
		}, "name"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Name        string `json:"name"`
				Visibility  string `json:"visibility"`
				Description string `json:"description"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.SourceRepoCreatePreview(p.Name, p.Visibility, p.Description)
		})

	s.add("github_repo_info",
		"Read basic metadata for a repository under the configured GitHub owner. Token is never exposed and output is redacted.",
		object(map[string]any{"name": strProp("repository name")}, "name"),
		func(a json.RawMessage) (string, error) {
			var p struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(a, &p); err != nil {
				return "", err
			}
			return s.svc.SourceRepoInfo(p.Name)
		})

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
