package main

import "github.com/charle-z/mcp-devbox/internal/edgeclient"

func projectGitRemoteURL(resolved edgeclient.ProjectResolution) string {
	return "https://github.com/" + resolved.Project.Owner + "/" + resolved.Project.Repository + ".git"
}
