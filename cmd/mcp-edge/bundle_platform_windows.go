//go:build windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
)

var installedBundleRoot = windowsInstalledBundleRoot()

func windowsInstalledBundleRoot() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(filepath.Clean(executable)))
}

func installedBundleArchitecture() string { return runtime.GOARCH }

func installedBundlePlatform() string { return "windows" }
