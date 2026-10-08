package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charle-z/mcp-devbox/internal/bundle"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("mcp-release-channel", flag.ContinueOnError)
	archive := fs.String("archive", "", "absolute deterministic release archive")
	output := fs.String("output", "", "absolute channel output directory")
	release := fs.String("release", "", "p15.x.y bridge or vMAJOR.MINOR.PATCH release")
	commit := fs.String("commit", "", "exact release commit")
	protocol := fs.String("protocol", "", "bundle protocol")
	catalog := fs.String("catalog", "", "catalog sha256")
	architecture := fs.String("architecture", "", "amd64 or arm64")
	platform := fs.String("platform", "", "empty for the legacy Linux channel or windows")
	keyPath := fs.String("private-key", "", "absolute raw Ed25519 private key")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || !filepath.IsAbs(*archive) || !filepath.IsAbs(*output) || !filepath.IsAbs(*keyPath) {
		return 2
	}
	archiveBytes, err := os.ReadFile(*archive)
	if err != nil {
		fmt.Fprintln(os.Stderr, "release archive unavailable")
		return 1
	}
	sum := sha256.Sum256(archiveBytes)
	key, err := readPrivateKey(*keyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "release signing key is invalid")
		return 1
	}
	version := 1
	if *platform != "" {
		version = 2
	}
	channel := bundle.Channel{
		Version: version, Release: *release, Commit: *commit, ProtocolVersion: *protocol,
		CatalogHash: *catalog, Architecture: *architecture,
		Platform:    *platform,
		ArchiveHash: "sha256:" + hex.EncodeToString(sum[:]),
	}
	canonical, signature, err := bundle.SignChannel(channel, key)
	if err != nil {
		fmt.Fprintln(os.Stderr, "release channel is invalid")
		return 1
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		return 1
	}
	name := "channel-" + *architecture + ".json"
	if *platform != "" {
		name = "channel-" + *platform + "-" + *architecture + ".json"
	}
	base := filepath.Join(*output, name)
	if err := writeNew(base, canonical, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "release channel already exists")
		return 1
	}
	if err := writeNew(base+".sig", signature, 0o644); err != nil {
		_ = os.Remove(base)
		fmt.Fprintln(os.Stderr, "release channel signature already exists")
		return 1
	}
	return 0
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != ed25519.PrivateKeySize {
		return nil, errors.New("invalid key")
	}
	content, err := os.ReadFile(path)
	if err != nil || len(content) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid key")
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
