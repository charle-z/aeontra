package edgeclient

import (
	"net/url"
	"strings"
)

func projectRemoteMatches(raw, owner, repository string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || !strings.HasSuffix(strings.ToLower(parts[1]), ".git") {
		return false
	}
	remoteOwner, remoteRepository, err := NormalizeProjectRepository(parts[0], parts[1][:len(parts[1])-4])
	return err == nil && remoteOwner == owner && remoteRepository == repository
}
