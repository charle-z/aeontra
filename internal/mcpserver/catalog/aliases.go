package catalog

// Alias describes one stable public compatibility name that must reuse the exact
// target schema, handler, and policy path.
type Alias struct {
	Name        string
	Target      string
	Description string
}

// RegisterAliases emits compatibility aliases in their historical catalog order.
func RegisterAliases(register func(Alias)) {
	aliases := []Alias{
		{Name: "repo_list", Target: "list_dir", Description: "List one jailed repository directory without reading file contents; equivalent to list_dir."},
		{Name: "repo_status", Target: "git_status", Description: "Show read-only status for one jailed repository; equivalent to git_status."},
		{Name: "repo_diff", Target: "git_diff", Description: "Show a read-only diff for one jailed repository; equivalent to git_diff."},
		{Name: "source_repo_info", Target: "github_repo_info", Description: "Read metadata for a repository under the configured source-host owner; equivalent to github_repo_info and performs an external read."},
		{Name: "source_repo_create", Target: "github_create_repo", Description: "Create a repository under the configured source-host owner; equivalent to github_create_repo and performs an external write requiring approval in ask mode."},
		{Name: "repo_publish", Target: "git_push", Description: "Publish one local branch to one named remote; equivalent to git_push and performs an external write requiring approval in ask mode."},
		{Name: "platform_apps_list", Target: "coolify_list_apps", Description: "List applications from the configured deployment platform; equivalent to coolify_list_apps and performs an external read."},
		{Name: "platform_app_status", Target: "coolify_app_status", Description: "Read one application from the configured deployment platform; equivalent to coolify_app_status and performs an external read."},
		{Name: "platform_app_logs", Target: "coolify_app_logs", Description: "Read bounded application logs from the configured deployment platform; equivalent to coolify_app_logs and performs an external read."},
		{Name: "platform_deployment_status", Target: "coolify_deployment_status", Description: "Read one deployment from the configured deployment platform; equivalent to coolify_deployment_status and performs an external read."},
		{Name: "platform_app_create", Target: "coolify_create_app", Description: "Create an application on the configured deployment platform; equivalent to coolify_create_app and performs an external write requiring approval in ask mode."},
		{Name: "platform_deploy", Target: "coolify_deploy", Description: "Trigger a deployment on the configured platform; equivalent to coolify_deploy and performs an external write requiring approval in ask mode."},
	}
	for _, alias := range aliases {
		register(alias)
	}
}
