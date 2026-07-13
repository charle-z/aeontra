package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/mcpserver"
	"github.com/charle-z/mcp-devbox/internal/oauth"
)

// buildOAuthProvider constructs the OAuth provider from env, or returns (nil, nil) when
// OAuth is not configured. It errors if only one of the two required vars is set, so a
// half-configured OAuth setup fails loudly rather than silently falling back.
func buildOAuthProvider() (*oauth.Provider, error) {
	publicURL := strings.TrimSpace(os.Getenv(publicURLEnv))
	passphrase := os.Getenv(oauthPassphraseEnv)
	if publicURL == "" && strings.TrimSpace(passphrase) == "" {
		return nil, nil // OAuth disabled
	}
	if publicURL == "" || strings.TrimSpace(passphrase) == "" {
		return nil, fmt.Errorf("OAuth requires BOTH %s and %s to be set", publicURLEnv, oauthPassphraseEnv)
	}
	issuer := strings.TrimRight(publicURL, "/")
	p, err := oauth.NewProvider(oauth.Config{
		Issuer:           issuer,
		Resource:         issuer + mcpserver.DefaultMCPPath,
		Passphrase:       passphrase,
		ClientStorePath:  os.Getenv(oauthClientStorePathEnv),
		RefreshStorePath: os.Getenv(oauthRefreshStorePathEnv),
	})
	if err != nil {
		return nil, fmt.Errorf("configuring OAuth: %w", err)
	}
	return p, nil
}
