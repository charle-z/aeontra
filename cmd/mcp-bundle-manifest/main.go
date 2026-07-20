package main

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/bundle"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("mcp-bundle-manifest", flag.ContinueOnError)
	root := fs.String("root", "", "absolute staged release root")
	release := fs.String("release", "", "p15.x.y release")
	commit := fs.String("commit", "", "exact 40-character commit")
	protocol := fs.String("protocol", "", "bundle protocol version")
	catalog := fs.String("catalog", "", "sha256 catalog identity")
	architecture := fs.String("architecture", "", "amd64 or arm64")
	keyPath := fs.String("private-key", "", "absolute raw Ed25519 private-key file")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	if !filepath.IsAbs(filepath.Clean(*root)) || !filepath.IsAbs(filepath.Clean(*keyPath)) {
		fmt.Fprintln(os.Stderr, "bundle root and private key must be absolute")
		return 1
	}
	key, err := readPrivateKey(*keyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bundle private key is invalid")
		return 1
	}
	manifest, err := bundle.Build(*root, bundle.Metadata{
		Release: strings.TrimSpace(*release), Commit: strings.TrimSpace(*commit),
		ProtocolVersion: strings.TrimSpace(*protocol), CatalogHash: strings.TrimSpace(*catalog),
		Architecture: strings.TrimSpace(*architecture),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	signature, err := bundle.Sign(manifest, key)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bundle signing failed")
		return 1
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "bundle manifest encoding failed")
		return 1
	}
	if err := writeNew(filepath.Join(*root, bundle.ManifestFile), append(encoded, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "bundle manifest already exists or is unsafe")
		return 1
	}
	if err := writeNew(filepath.Join(*root, bundle.SignatureFile), signature, 0o644); err != nil {
		_ = os.Remove(filepath.Join(*root, bundle.ManifestFile))
		fmt.Fprintln(os.Stderr, "bundle signature already exists or is unsafe")
		return 1
	}
	fmt.Println("signed bundle manifest created")
	return 0
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != ed25519.PrivateKeySize {
		return nil, errors.New("invalid private key")
	}
	content, err := os.ReadFile(path)
	if err != nil || len(content) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid private key")
	}
	return ed25519.PrivateKey(content), nil
}

func writeNew(path string, content []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
