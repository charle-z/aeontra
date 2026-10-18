// Command mcp-bundle-keygen creates an offline raw Ed25519 release key.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	flags := flag.NewFlagSet("mcp-bundle-keygen", flag.ContinueOnError)
	output := flags.String("output", "", "absolute new private-key path")
	if flags.Parse(os.Args[1:]) != nil || flags.NArg() != 0 || !filepath.IsAbs(*output) {
		fmt.Fprintln(os.Stderr, "usage: mcp-bundle-keygen --output <ABS_NEW_FILE>")
		os.Exit(2)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "release key generation failed")
		os.Exit(1)
	}
	file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "private-key output is unavailable")
		os.Exit(1)
	}
	_, writeErr := file.Write(privateKey)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(*output)
		fmt.Fprintln(os.Stderr, "private-key output failed")
		os.Exit(1)
	}
	fmt.Println(hex.EncodeToString(publicKey))
}
