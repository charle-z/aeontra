package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"runtime"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/bundle"
)

const installedBundleRoot = "/opt/mcp-devbox/current"

func bundleCommand(args []string, stdout io.Writer) error {
	if len(args) != 1 || args[0] != "verify" {
		return errors.New("bundle accepts only the verify operation")
	}
	verified, err := verifyInstalledEdgeBundleAt(installedBundleRoot)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "bundle valid release=%s commit=%s\n", verified.Release, verified.Commit)
	return nil
}

func verifyInstalledEdgeBundle(root string) error {
	_, err := verifyInstalledEdgeBundleAt(root)
	return err
}

func verifyInstalledEdgeBundleAt(root string) (bundle.Verified, error) {
	keyBytes, err := hex.DecodeString(buildinfo.EdgeBundlePublicKey)
	if err != nil || len(keyBytes) != ed25519.PublicKeySize {
		return bundle.Verified{}, &bundle.VerificationError{Code: bundle.ManifestInvalid}
	}
	return bundle.LoadAndVerify(root, ed25519.PublicKey(keyBytes), bundle.Compatibility{
		Release: buildinfo.EdgeBundleRelease, Commit: buildinfo.Commit,
		ProtocolVersion: buildinfo.EdgeBundleProtocolVersion,
		CatalogHash:     buildinfo.EdgeBundleCatalogHash,
		Architecture:    runtime.GOARCH,
	})
}
