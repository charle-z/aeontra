package catalog

import "encoding/json"

// PlatformAppCreatePreviewRequest is the catalog-layer request for planning one
// application without exposing deployment-client implementation details.
type PlatformAppCreatePreviewRequest struct {
	Name                string
	GitHubRepo          string
	Branch              string
	Domain              string
	Port                string
	BuildPack           string
	HealthcheckPath     string
	HealthcheckInterval int
	HealthcheckTimeout  int
	RequiredEnv         []string
}

// PlatformAppPreviewService is the narrow contract required by application
// creation preview.
type PlatformAppPreviewService interface {
	PlatformAppCreatePreview(request PlatformAppCreatePreviewRequest) (string, error)
}

// RegisterPlatformAppPreview registers application creation preview at its
// original catalog position.
func RegisterPlatformAppPreview(register Register, service PlatformAppPreviewService) {
	register(Tool{
		Name:        "platform_app_create_preview",
		Description: "Validate a Coolify application definition against configured server/project/environment, GitHub owner and domain allowlist, then create a read-only expiring single-use plan. Required environment variable names are shown; no secret values are accepted or returned.",
		InputSchema: object(map[string]any{
			"name":                 strProp("new application name"),
			"github_repo":          strProp("owner/repo or allowed credential-free GitHub URL"),
			"branch":               strProp("branch, defaults to main"),
			"domain":               strProp("optional domain restricted by COOLIFY_ALLOWED_DOMAINS"),
			"port":                 strProp("optional exposed port from 1 to 65535"),
			"build_pack":           strProp("nixpacks, dockerfile, static, or dockercompose"),
			"healthcheck_path":     strProp("optional absolute HTTP healthcheck path"),
			"healthcheck_interval": map[string]any{"type": "integer", "description": "optional healthcheck interval in seconds"},
			"healthcheck_timeout":  map[string]any{"type": "integer", "description": "optional healthcheck timeout in seconds"},
			"required_env": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "names of required environment variables; never values",
			},
		}, "name", "github_repo"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
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
			if err := json.Unmarshal(arguments, &params); err != nil {
				return "", err
			}
			return service.PlatformAppCreatePreview(PlatformAppCreatePreviewRequest{
				Name: params.Name, GitHubRepo: params.GitHubRepo, Branch: params.Branch, Domain: params.Domain,
				Port: params.Port, BuildPack: params.BuildPack, HealthcheckPath: params.HealthcheckPath,
				HealthcheckInterval: params.HealthcheckInterval, HealthcheckTimeout: params.HealthcheckTimeout,
				RequiredEnv: params.RequiredEnv,
			})
		},
	})
}
