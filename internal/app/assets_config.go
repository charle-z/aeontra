package app

import (
	"errors"
	"os"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/assets"
)

func buildAssetLibrary(repositoryRoots []string) (*assets.Library, error) {
	path := strings.TrimSpace(os.Getenv(assetLibraryEnv))
	if path == "" {
		return nil, nil
	}
	library, err := assets.LoadLibrary(path, repositoryRoots)
	if err != nil {
		return nil, errors.New("invalid " + assetLibraryEnv + ": requires a valid reviewed owner-private Linux manifest outside repository roots")
	}
	return library, nil
}
