// Command mcp-catalog-smoke verifies that a deployed MCP server is running the
// expected commit and the same deterministic tool catalog as this source tree.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/mcpserver"
)

const maxVersionResponseBytes = 64 << 10

type versionResponse struct {
	Status          string `json:"status"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocol_version"`
	Commit          string `json:"commit"`
	BuiltAt         string `json:"built_at"`
	ToolCount       int    `json:"tool_count"`
	CatalogHash     string `json:"catalog_hash"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-catalog-smoke:", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("mcp-catalog-smoke", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	baseURL := flags.String("url", "", "deployed HTTPS base URL or /version URL")
	expectedCommit := flags.String("expected-commit", "", "exact commit expected from the deployment")
	timeout := flags.Duration("timeout", 15*time.Second, "HTTP request timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if strings.TrimSpace(*expectedCommit) == "" {
		return fmt.Errorf("--expected-commit is required")
	}
	if *timeout <= 0 || *timeout > 2*time.Minute {
		return fmt.Errorf("--timeout must be greater than zero and at most 2m")
	}

	endpoint, err := versionEndpoint(*baseURL)
	if err != nil {
		return err
	}
	localInfo, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		return fmt.Errorf("building local catalog identity: %w", err)
	}

	client := &http.Client{
		Timeout: *timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("creating version request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("requesting deployed version: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("deployed version returned HTTP %d", response.StatusCode)
	}

	limited := io.LimitReader(response.Body, maxVersionResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("reading deployed version: %w", err)
	}
	if len(body) > maxVersionResponseBytes {
		return fmt.Errorf("deployed version response exceeds %d bytes", maxVersionResponseBytes)
	}
	var remote versionResponse
	if err := json.Unmarshal(body, &remote); err != nil {
		return fmt.Errorf("decoding deployed version: %w", err)
	}
	if err := validateRemote(remote, response.Header, strings.TrimSpace(*expectedCommit), localInfo); err != nil {
		return err
	}

	fmt.Fprintf(output, "catalog smoke passed\n")
	fmt.Fprintf(output, "url=%s\n", endpoint.Redacted())
	fmt.Fprintf(output, "version=%s\n", remote.Version)
	fmt.Fprintf(output, "protocol_version=%s\n", remote.ProtocolVersion)
	fmt.Fprintf(output, "commit=%s\n", remote.Commit)
	fmt.Fprintf(output, "tool_count=%d\n", remote.ToolCount)
	fmt.Fprintf(output, "catalog_hash=%s\n", remote.CatalogHash)
	return nil
}

func validateRemote(remote versionResponse, headers http.Header, expectedCommit string, local mcpserver.RuntimeInfo) error {
	if remote.Status != "ok" {
		return fmt.Errorf("deployed status = %q, want ok", remote.Status)
	}
	if remote.Commit != expectedCommit {
		return fmt.Errorf("deployed commit = %q, want %q", remote.Commit, expectedCommit)
	}
	if remote.Version != local.Version {
		return fmt.Errorf("deployed version = %q, local = %q", remote.Version, local.Version)
	}
	if remote.ProtocolVersion != local.ProtocolVersion {
		return fmt.Errorf("deployed protocol version = %q, local = %q", remote.ProtocolVersion, local.ProtocolVersion)
	}
	if remote.ToolCount != local.ToolCount {
		return fmt.Errorf("deployed tool count = %d, local = %d", remote.ToolCount, local.ToolCount)
	}
	if remote.CatalogHash != local.CatalogHash {
		return fmt.Errorf("deployed catalog hash = %q, local = %q", remote.CatalogHash, local.CatalogHash)
	}
	if headers.Get("X-MCP-Server-Commit") != remote.Commit {
		return fmt.Errorf("commit header does not match response body")
	}
	if headers.Get("X-MCP-Catalog-Hash") != remote.CatalogHash {
		return fmt.Errorf("catalog hash header does not match response body")
	}
	headerCount, err := strconv.Atoi(headers.Get("X-MCP-Tool-Count"))
	if err != nil || headerCount != remote.ToolCount {
		return fmt.Errorf("tool count header does not match response body")
	}
	cacheControl := strings.ToLower(headers.Get("Cache-Control"))
	if !strings.Contains(cacheControl, "no-store") || strings.ToLower(headers.Get("Pragma")) != "no-cache" {
		return fmt.Errorf("deployed version response is missing no-cache headers")
	}
	return nil
}

func versionEndpoint(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("--url is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing --url: %w", err)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("--url must not contain credentials, query parameters, or fragments")
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("--url must include a host")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return nil, fmt.Errorf("--url must use HTTPS except for loopback testing")
	}
	switch strings.TrimRight(parsed.EscapedPath(), "/") {
	case "", "/version":
		parsed.Path = "/version"
		parsed.RawPath = ""
	default:
		return nil, fmt.Errorf("--url path must be empty, /, or /version")
	}
	return parsed, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
