//go:build linux

package main

import (
	"errors"
	"os"
)

func ensureWorkcellUser() error {
	if os.Geteuid() == 0 {
		return errors.New("development workcell refuses to run as root")
	}
	return nil
}
