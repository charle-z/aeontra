//go:build windows

package modelturn

import (
	"context"
	"errors"
	"io"
)

const DefaultDriverSocketName = "model-turn-driver.sock"

func ServeDriver(context.Context, string, *Store, io.Writer) error {
	return errors.New("the OpenCode model-turn driver requires a Unix socket")
}
