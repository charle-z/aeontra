//go:build !windows

package main

import "runtime"

const installedBundleRoot = "/opt/mcp-devbox/current"

func installedBundleArchitecture() string { return runtime.GOARCH }

func installedBundlePlatform() string { return "" }
