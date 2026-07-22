package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func main() {
	root := flag.String("root", "", "absolute output root")
	flag.Parse()
	if flag.NArg() != 0 || !filepath.IsAbs(strings.TrimSpace(*root)) || filepath.Clean(*root) == string(os.PathSeparator) {
		fatal("fixture root must be one absolute non-root path")
	}
	clean := filepath.Clean(*root)
	if err := os.MkdirAll(clean, 0o700); err != nil {
		fatal("create fixture root")
	}
	if err := os.Chmod(clean, 0o700); err != nil {
		fatal("protect fixture root")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fatal("generate fixture device key")
	}
	controlPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fatal("generate fixture control key")
	}
	identity := edgeclient.Identity{
		SchemaVersion:    2,
		ServerURL:        "https://mcp.example.invalid",
		DeviceID:         "ed_0123456789abcdef0123456789abcdef",
		Name:             "parrot-ci",
		ControlPublicKey: edge.EncodePublicKey(controlPublic),
	}
	identityBytes, err := json.Marshal(identity)
	if err != nil {
		fatal("encode fixture identity")
	}
	writePrivate(filepath.Join(clean, "identity.json"), append(identityBytes, '\n'))
	writePrivate(filepath.Join(clean, "device.key"), []byte(base64.RawURLEncoding.EncodeToString(privateKey)+"\n"))
	writePrivate(filepath.Join(clean, "workspaces.db"), []byte("ws_593c26b24ba6dc583c9aa1da5e9e0152\n"))
	writePrivate(filepath.Join(clean, "checkpoint.md"), []byte("preserved-checkpoint\n"))
	if _, _, err := edgeclient.LoadIdentity(clean); err != nil {
		fatal("validate fixture identity")
	}
}

func writePrivate(path string, content []byte) {
	if err := os.WriteFile(path, content, 0o600); err != nil {
		fatal("write fixture file")
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
