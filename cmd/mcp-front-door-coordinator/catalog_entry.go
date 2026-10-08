package main

import (
	"context"
	"errors"
	"net"
)

func runCatalogCoordinator(ctx context.Context, getenv func(string) string, dependencies coordinatorDependencies) error {
	listener, err := net.Listen("tcp", coordinatorListenAddress(getenv))
	if err != nil {
		return errors.New("catalog coordinator listener failed")
	}
	defer listener.Close()
	return serveCatalogCoordinator(ctx, listener, getenv, dependencies)
}
