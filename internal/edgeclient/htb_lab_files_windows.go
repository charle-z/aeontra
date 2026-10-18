//go:build windows

package edgeclient

import "errors"

func readHTBLabArtifact(string, string, int64) ([]byte, error) {
	return nil, errors.New("HTB lab artifact access requires Linux")
}

func writeHTBLabOutput(string, string, []byte) error {
	return errors.New("HTB lab output access requires Linux")
}
