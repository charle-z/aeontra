package app

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/grantadmin"
)

func grant(args []string) error {
	fs := flag.NewFlagSet("grant", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	adminFile := fs.String("admin-file", "", "absolute private grant channel descriptor created by the daemon")
	ttl := fs.String("ttl", "5m", "short grant ttl, e.g. 5m")
	raw := fs.Bool("raw", false, "approve unredacted secret output")
	confirmRaw := fs.Bool("confirm-raw", false, "required together with --raw")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("grant requires exactly one REQUEST_ID")
	}
	descriptor, err := readGrantAdminDescriptor(*adminFile)
	if err != nil {
		return err
	}
	if *raw && !*confirmRaw {
		return fmt.Errorf("--raw requires --confirm-raw")
	}
	body, err := json.Marshal(map[string]any{"ttl": *ttl, "raw": *raw})
	if err != nil {
		return err
	}
	url := strings.TrimRight(descriptor.BaseURL, "/") + grantadmin.DefaultPath + "/" + fs.Arg(0)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+descriptor.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("grant rejected: %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	fmt.Fprintln(os.Stdout, strings.TrimSpace(string(respBody)))
	return nil
}
