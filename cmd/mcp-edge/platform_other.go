//go:build !linux

package main

import "errors"

func ensureWorkcellUser() error {
	return errors.New("development workcell requires Linux or WSL2")
}
