//go:build !linux

package tools

import (
	"errors"
	"os"
)

func assetDirectoryIdentity(os.FileInfo) (string, error) {
	return "", errors.New("asset materialization requires the Linux backend")
}
func createAssetBeneath(string, string, string, string, []byte) error {
	return errors.New("asset materialization requires the Linux backend")
}
