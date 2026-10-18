//go:build windows

package edgeclient

import (
	"context"
	"errors"
)

func StartDevGitBroker(context.Context, DevGitBrokerConfig) (<-chan error, error) {
	return nil, errors.New("development Git broker requires Linux")
}
