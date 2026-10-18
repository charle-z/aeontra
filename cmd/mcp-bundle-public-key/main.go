package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 2 || !filepath.IsAbs(os.Args[1]) {
		fmt.Fprintln(os.Stderr, "usage: mcp-bundle-public-key <ABS_RAW_PRIVATE_KEY>")
		os.Exit(2)
	}
	key, err := readKey(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "bundle private key is invalid")
		os.Exit(1)
	}
	public := key.Public().(ed25519.PublicKey)
	fmt.Println(hex.EncodeToString(public))
}

func readKey(path string) (ed25519.PrivateKey, error) {
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
