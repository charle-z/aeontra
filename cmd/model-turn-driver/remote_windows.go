//go:build windows

package main

import (
	"context"
	"errors"
)

func runRemoteDriver(context.Context, string, string) error {
	return errors.New("remote model-turn driver requires a Unix-like Edge host")
}
