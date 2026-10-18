// Command console-smoke verifies the deployed authenticated console without printing
// or persisting the configured MCP token or the opaque console session cookie.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/console"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
)

const (
	consoleTokenEnv        = "MCP_DEVBOX_TOKEN"
	maxConsoleResponseSize = 64 << 10
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Getenv, nil); err != nil {
		fmt.Fprintln(os.Stderr, "console-smoke:", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer, getenv func(string) string, suppliedClient *http.Client) error {
	flags := flag.NewFlagSet("console-smoke", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	baseURL := flags.String("url", "", "deployed HTTPS base URL or /console URL")
	expectedCommit := flags.String("expected-commit", "", "exact commit expected from the deployment")
	timeout := flags.Duration("timeout", 20*time.Second, "HTTP request timeout")
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
	endpoint, err := consoleEndpoint(*baseURL)
	if err != nil {
		return err
	}
	token := ""
	if getenv != nil {
		token = strings.TrimSpace(getenv(consoleTokenEnv))
	}
	if token == "" {
		return fmt.Errorf("%s is required for authenticated console smoke", consoleTokenEnv)
	}
	local, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		return fmt.Errorf("building local runtime identity: %w", err)
	}

	client, err := consoleClient(*timeout, suppliedClient)
	if err != nil {
		return err
	}
	if err := login(client, endpoint, token); err != nil {
		return err
	}
	status, headers, err := readStatus(client, endpoint)
	if err != nil {
		return err
	}
	if err := validateStatus(status, headers, strings.TrimSpace(*expectedCommit), local); err != nil {
		return err
	}

	fmt.Fprintln(output, "console smoke passed")
	fmt.Fprintf(output, "url=%s\n", endpoint.Redacted())
	fmt.Fprintf(output, "commit=%s\n", status.Commit)
	fmt.Fprintf(output, "tool_count=%d\n", status.ToolCount)
	fmt.Fprintf(output, "catalog_hash=%s\n", status.CatalogHash)
	fmt.Fprintf(output, "surface=%s\n", status.Surface)
	return nil
}

func consoleClient(timeout time.Duration, supplied *http.Client) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("creating private cookie jar: %w", err)
	}
	client := &http.Client{}
	if supplied != nil {
		*client = *supplied
	}
	client.Timeout = timeout
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client, nil
}

func login(client *http.Client, endpoint *url.URL, token string) error {
	loginURL := *endpoint
	loginURL.Path = "/console/login"
	form := url.Values{"token": {token}}
	request, err := http.NewRequest(http.MethodPost, loginURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("creating console login request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("requesting console login: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		return fmt.Errorf("console login returned HTTP %d", response.StatusCode)
	}
	if response.Header.Get("Location") != "/console" {
		return fmt.Errorf("console login returned an unexpected redirect")
	}
	if err := validateBrowserHeaders(response.Header); err != nil {
		return fmt.Errorf("console login headers: %w", err)
	}
	cookies := response.Cookies()
	if len(cookies) != 1 {
		return fmt.Errorf("console login returned %d cookies, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Value == "" || cookie.Value == token || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		return fmt.Errorf("console login returned an unsafe session cookie")
	}
	return nil
}

func readStatus(client *http.Client, endpoint *url.URL) (console.Status, http.Header, error) {
	statusURL := *endpoint
	statusURL.Path = "/console/status"
	request, err := http.NewRequest(http.MethodGet, statusURL.String(), nil)
	if err != nil {
		return console.Status{}, nil, fmt.Errorf("creating console status request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return console.Status{}, nil, fmt.Errorf("requesting console status: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return console.Status{}, response.Header, fmt.Errorf("console status returned HTTP %d", response.StatusCode)
	}
	if err := validateBrowserHeaders(response.Header); err != nil {
		return console.Status{}, response.Header, fmt.Errorf("console status headers: %w", err)
	}
	limited := io.LimitReader(response.Body, maxConsoleResponseSize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return console.Status{}, response.Header, fmt.Errorf("reading console status: %w", err)
	}
	if len(body) > maxConsoleResponseSize {
		return console.Status{}, response.Header, fmt.Errorf("console status exceeds %d bytes", maxConsoleResponseSize)
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	var status console.Status
	if err := decoder.Decode(&status); err != nil {
		return console.Status{}, response.Header, fmt.Errorf("decoding console status: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return console.Status{}, response.Header, err
	}
	return status, response.Header, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("console status contains trailing JSON data")
	}
	return nil
}

func validateStatus(status console.Status, headers http.Header, expectedCommit string, local mcpserver.RuntimeInfo) error {
	if status.Status != "ok" || !status.Authenticated || status.Surface != "presentation-only" {
		return fmt.Errorf("console status is not authenticated and healthy")
	}
	if status.Commit != expectedCommit {
		return fmt.Errorf("console commit = %q, want %q", status.Commit, expectedCommit)
	}
	if status.Version != local.Version || status.ProtocolVersion != local.ProtocolVersion {
		return fmt.Errorf("console runtime version does not match local source")
	}
	if status.ToolCount != local.ToolCount || status.CatalogHash != local.CatalogHash {
		return fmt.Errorf("console catalog identity does not match local source")
	}
	if headers.Get("X-MCP-Server-Commit") != status.Commit {
		return fmt.Errorf("console commit header does not match status")
	}
	if headers.Get("X-MCP-Catalog-Hash") != status.CatalogHash {
		return fmt.Errorf("console catalog hash header does not match status")
	}
	headerCount, err := strconv.Atoi(headers.Get("X-MCP-Tool-Count"))
	if err != nil || headerCount != status.ToolCount {
		return fmt.Errorf("console tool count header does not match status")
	}
	return nil
}

func validateBrowserHeaders(headers http.Header) error {
	for _, name := range []string{
		"Content-Security-Policy",
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
		"Permissions-Policy",
		"Cache-Control",
	} {
		if strings.TrimSpace(headers.Get(name)) == "" {
			return fmt.Errorf("missing %s", name)
		}
	}
	if strings.ToLower(headers.Get("X-Content-Type-Options")) != "nosniff" || strings.ToUpper(headers.Get("X-Frame-Options")) != "DENY" {
		return fmt.Errorf("browser hardening headers are invalid")
	}
	if strings.ToLower(headers.Get("Referrer-Policy")) != "no-referrer" || !strings.Contains(strings.ToLower(headers.Get("Cache-Control")), "no-store") {
		return fmt.Errorf("privacy/cache headers are invalid")
	}
	csp := headers.Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		return fmt.Errorf("content security policy is too broad")
	}
	return nil
}

func consoleEndpoint(raw string) (*url.URL, error) {
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
	case "", "/console":
		parsed.Path = "/console"
		parsed.RawPath = ""
	default:
		return nil, fmt.Errorf("--url path must be empty, /, or /console")
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
