package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/mcpserver"
	"github.com/charle-z/mcp-devbox/internal/oauth"
)

// buildOAuthProvider constructs the OAuth provider from env, or returns (nil, nil) when
// OAuth is not configured. It errors if only one of the two required vars is set, so a
// half-configured OAuth setup fails loudly rather than silently falling back.
func buildOAuthProvider(stateRoot string) (*oauth.Provider, error) {
	publicURL := strings.TrimSpace(os.Getenv(publicURLEnv))
	passphrase := os.Getenv(oauthPassphraseEnv)
	if publicURL == "" && strings.TrimSpace(passphrase) == "" {
		return nil, nil // OAuth disabled
	}
	if publicURL == "" || strings.TrimSpace(passphrase) == "" {
		return nil, fmt.Errorf("OAuth requires BOTH %s and %s to be set", publicURLEnv, oauthPassphraseEnv)
	}
	issuer := strings.TrimRight(publicURL, "/")
	clientStorePath, accessStorePath, refreshStorePath := resolveOAuthStorePaths(stateRoot)
	p, err := oauth.NewProvider(oauth.Config{
		Issuer:           issuer,
		Resource:         issuer + mcpserver.DefaultMCPPath,
		Passphrase:       passphrase,
		ClientStorePath:  clientStorePath,
		AccessStorePath:  accessStorePath,
		RefreshStorePath: refreshStorePath,
	})
	if err != nil {
		return nil, fmt.Errorf("configuring OAuth: %w", err)
	}
	return p, nil
}

// resolveOAuthStorePaths keeps explicit per-store overrides, but when an administrator
// configured a durable state root it defaults any missing OAuth store there. The Docker
// deployment sets MCP_DEVBOX_STATE_ROOT=/state, so client registrations, access-token
// digests and rotating refresh tokens no longer fall back silently to process memory.
//
// An empty state root preserves the local-development memory-only behavior. We do not
// derive persistence from the repository fallback state path because OAuth state must
// remain outside agent-writable repository roots.
func resolveOAuthStorePaths(stateRoot string) (clientStorePath, accessStorePath, refreshStorePath string) {
	clientStorePath = strings.TrimSpace(os.Getenv(oauthClientStorePathEnv))
	accessStorePath = strings.TrimSpace(os.Getenv(oauthAccessStorePathEnv))
	refreshStorePath = strings.TrimSpace(os.Getenv(oauthRefreshStorePathEnv))
	stateRoot = strings.TrimSpace(stateRoot)
	if stateRoot == "" {
		return clientStorePath, accessStorePath, refreshStorePath
	}
	stateRoot = filepath.Clean(stateRoot)
	if clientStorePath == "" {
		clientStorePath = filepath.Join(stateRoot, "oauth-clients.json")
	}
	if accessStorePath == "" {
		accessStorePath = filepath.Join(stateRoot, "oauth-access.json")
	}
	if refreshStorePath == "" {
		refreshStorePath = filepath.Join(stateRoot, "oauth-refresh.json")
	}
	return clientStorePath, accessStorePath, refreshStorePath
}
