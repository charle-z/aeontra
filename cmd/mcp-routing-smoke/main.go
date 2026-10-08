// Command mcp-routing-smoke verifies that discovery and invocation remain coherent
// through the deployed authenticated MCP route. It never prints credentials, session
// identifiers, tool output, repository paths, or command output.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/mcpserver"
)

const (
	defaultBearerEnv     = "MCP_DEVBOX_TOKEN"
	maxSmokeResponse     = 8 << 20
	maxSmokeTimeout      = 2 * time.Minute
	requiredRuntimeTool  = "system_runtime_info"
	requiredSandboxTool  = "sandbox_status"
	optionalExecutorTool = "sandbox_exec"
)

var (
	environmentNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
	gitObjectPattern       = regexp.MustCompile(`(?m)^[0-9a-f]{40,64}$`)
)

type versionResponse struct {
	Status          string `json:"status"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocol_version"`
	Commit          string `json:"commit"`
	BuiltAt         string `json:"built_at"`
	ToolCount       int    `json:"tool_count"`
	CatalogHash     string `json:"catalog_hash"`
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

type routingClient struct {
	client     *http.Client
	endpoint   *url.URL
	credential string
	expected   versionResponse
	sessionID  string
	nextID     int
	recoveries int
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Getenv, nil); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-routing-smoke:", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer, getenv func(string) string, client *http.Client) error {
	flags := flag.NewFlagSet("mcp-routing-smoke", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	baseURL := flags.String("url", "", "deployed HTTPS base URL or /mcp URL")
	expectedCommit := flags.String("expected-commit", "", "exact commit expected from the deployment")
	bearerEnv := flags.String("bearer-env", defaultBearerEnv, "environment variable containing the bearer credential")
	sandboxCWD := flags.String("sandbox-cwd", "", "optional authorized repository used for read-only sandbox execution probes")
	timeout := flags.Duration("timeout", 30*time.Second, "HTTP request timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if strings.TrimSpace(*expectedCommit) == "" {
		return errors.New("--expected-commit is required")
	}
	if !environmentNamePattern.MatchString(strings.TrimSpace(*bearerEnv)) {
		return errors.New("--bearer-env must name a non-empty uppercase environment variable")
	}
	if *timeout <= 0 || *timeout > maxSmokeTimeout {
		return errors.New("--timeout must be greater than zero and at most 2m")
	}
	credential := strings.TrimSpace(getenv(strings.TrimSpace(*bearerEnv)))
	if credential == "" {
		return errors.New("configured bearer environment variable is empty")
	}
	versionURL, mcpURL, err := smokeEndpoints(*baseURL)
	if err != nil {
		return err
	}
	if client == nil {
		client = &http.Client{
			Timeout:       *timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}

	local, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		return fmt.Errorf("building local catalog identity: %w", err)
	}
	remote, err := readVersion(client, versionURL)
	if err != nil {
		return err
	}
	if err := validateVersion(remote, strings.TrimSpace(*expectedCommit), local); err != nil {
		return err
	}
	routing := &routingClient{client: client, endpoint: mcpURL, credential: credential, expected: remote}
	if err := routing.initialize(); err != nil {
		return err
	}
	tools, err := routing.listTools()
	if err != nil {
		return err
	}
	if err := validateMaterializedCatalog(tools, remote.ToolCount, *sandboxCWD != ""); err != nil {
		return err
	}
	if err := routing.verifyRuntimeIdentity(); err != nil {
		return err
	}
	if _, err := routing.callTool(requiredSandboxTool, map[string]any{}); err != nil {
		return fmt.Errorf("calling %s: %w", requiredSandboxTool, err)
	}
	if strings.TrimSpace(*sandboxCWD) != "" {
		if err := routing.verifySandboxExecution(strings.TrimSpace(*sandboxCWD)); err != nil {
			return err
		}
	}

	fmt.Fprintln(output, "routing smoke passed")
	fmt.Fprintf(output, "url=%s\n", versionURL.Redacted())
	fmt.Fprintf(output, "commit=%s\n", remote.Commit)
	fmt.Fprintf(output, "tool_count=%d\n", remote.ToolCount)
	fmt.Fprintf(output, "catalog_hash=%s\n", remote.CatalogHash)
	fmt.Fprintln(output, "catalog_materialized=true")
	fmt.Fprintln(output, "runtime_call=true")
	fmt.Fprintln(output, "sandbox_status_call=true")
	if strings.TrimSpace(*sandboxCWD) != "" {
		fmt.Fprintln(output, "sandbox_exec_calls=true")
		fmt.Fprintln(output, "workspace_unchanged=true")
	}
	fmt.Fprintf(output, "session_recoveries=%d\n", routing.recoveries)
	return nil
}

func smokeEndpoints(raw string) (*url.URL, *url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil, errors.New("--url is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing --url: %w", err)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
		return nil, nil, errors.New("--url must contain only a scheme, host, optional port, and supported path")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return nil, nil, errors.New("--url must use HTTPS except for loopback testing")
	}
	switch strings.TrimRight(parsed.EscapedPath(), "/") {
	case "", "/mcp", "/version":
	default:
		return nil, nil, errors.New("--url path must be empty, /, /mcp, or /version")
	}
	versionURL := *parsed
	versionURL.Path, versionURL.RawPath = "/version", ""
	mcpURL := *parsed
	mcpURL.Path, mcpURL.RawPath = "/mcp", ""
	return &versionURL, &mcpURL, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func readVersion(client *http.Client, endpoint *url.URL) (versionResponse, error) {
	request, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return versionResponse{}, fmt.Errorf("creating version request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return versionResponse{}, fmt.Errorf("requesting deployed version: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return versionResponse{}, fmt.Errorf("deployed version returned HTTP %d", response.StatusCode)
	}
	body, err := readBounded(response.Body)
	if err != nil {
		return versionResponse{}, err
	}
	var remote versionResponse
	if err := decodeExact(body, &remote); err != nil {
		return versionResponse{}, fmt.Errorf("decoding deployed version: %w", err)
	}
	if err := validateIdentityHeaders(response.Header, remote); err != nil {
		return versionResponse{}, err
	}
	return remote, nil
}

func validateVersion(remote versionResponse, expectedCommit string, local mcpserver.RuntimeInfo) error {
	if remote.Status != "ok" {
		return fmt.Errorf("deployed status = %q, want ok", remote.Status)
	}
	if remote.Commit != expectedCommit {
		return fmt.Errorf("deployed commit = %q, want %q", remote.Commit, expectedCommit)
	}
	if remote.Version != local.Version || remote.ProtocolVersion != local.ProtocolVersion || remote.ToolCount != local.ToolCount || remote.CatalogHash != local.CatalogHash {
		return errors.New("deployed runtime identity does not match the local source")
	}
	return nil
}

func validateIdentityHeaders(headers http.Header, expected versionResponse) error {
	if headers.Get("X-MCP-Server-Commit") != expected.Commit || headers.Get("X-MCP-Catalog-Hash") != expected.CatalogHash {
		return errors.New("runtime identity headers do not match the expected deployment")
	}
	count, err := strconv.Atoi(headers.Get("X-MCP-Tool-Count"))
	if err != nil || count != expected.ToolCount {
		return errors.New("runtime tool count header does not match the expected deployment")
	}
	if !strings.Contains(strings.ToLower(headers.Get("Cache-Control")), "no-store") || !strings.EqualFold(headers.Get("Pragma"), "no-cache") {
		return errors.New("runtime response is missing no-cache headers")
	}
	return nil
}

func (c *routingClient) initialize() error {
	c.nextID++
	response, body, err := c.rpc(http.MethodPost, "", map[string]any{
		"jsonrpc": "2.0", "id": c.nextID, "method": "initialize",
		"params": map[string]any{"protocolVersion": c.expected.ProtocolVersion, "capabilities": map[string]any{}},
	})
	if err != nil {
		return fmt.Errorf("initializing MCP session: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("initialize returned HTTP %d", response.StatusCode)
	}
	if err := validateIdentityHeaders(response.Header, c.expected); err != nil {
		return err
	}
	var envelope rpcEnvelope
	if err := decodeJSON(body, &envelope); err != nil {
		return fmt.Errorf("decoding initialize response: %w", err)
	}
	if err := validateRPCEnvelope(envelope, c.nextID); err != nil {
		return fmt.Errorf("validating initialize response: %w", err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("initialize returned MCP error code %d", envelope.Error.Code)
	}
	sessionID := strings.TrimSpace(response.Header.Get("Mcp-Session-Id"))
	if sessionID == "" {
		return errors.New("initialize returned no MCP session")
	}
	c.sessionID = sessionID
	return nil
}

func (c *routingClient) sessionRPC(method string, params map[string]any) (rpcEnvelope, error) {
	for attempt := 0; attempt < 2; attempt++ {
		c.nextID++
		response, body, err := c.rpc(http.MethodPost, c.sessionID, map[string]any{
			"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params,
		})
		if err != nil {
			return rpcEnvelope{}, err
		}
		if err := validateIdentityHeaders(response.Header, c.expected); err != nil {
			return rpcEnvelope{}, err
		}
		if response.StatusCode == http.StatusNotFound && attempt == 0 {
			c.recoveries++
			c.sessionID = ""
			if err := c.initialize(); err != nil {
				return rpcEnvelope{}, fmt.Errorf("refreshing stale MCP session: %w", err)
			}
			continue
		}
		if response.StatusCode != http.StatusOK {
			return rpcEnvelope{}, fmt.Errorf("%s returned HTTP %d", method, response.StatusCode)
		}
		var envelope rpcEnvelope
		if err := decodeJSON(body, &envelope); err != nil {
			return rpcEnvelope{}, fmt.Errorf("decoding %s response: %w", method, err)
		}
		if err := validateRPCEnvelope(envelope, c.nextID); err != nil {
			return rpcEnvelope{}, fmt.Errorf("validating %s response: %w", method, err)
		}
		if envelope.Error != nil {
			return rpcEnvelope{}, fmt.Errorf("%s returned MCP error code %d", method, envelope.Error.Code)
		}
		return envelope, nil
	}
	return rpcEnvelope{}, errors.New("stale MCP session remained unavailable after one refresh")
}

func validateRPCEnvelope(envelope rpcEnvelope, expectedID int) error {
	if envelope.JSONRPC != "2.0" {
		return errors.New("response has an invalid JSON-RPC version")
	}
	var actualID int
	if err := json.Unmarshal(envelope.ID, &actualID); err != nil || actualID != expectedID {
		return errors.New("response id does not match the request")
	}
	return nil
}

func (c *routingClient) listTools() ([]string, error) {
	envelope, err := c.sessionRPC("tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
		NextCursor string `json:"nextCursor,omitempty"`
	}
	if err := decodeJSON(envelope.Result, &result); err != nil {
		return nil, fmt.Errorf("decoding tools/list result: %w", err)
	}
	if result.NextCursor != "" {
		return nil, errors.New("tools/list unexpectedly returned a partial paginated catalog")
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names, nil
}

func validateMaterializedCatalog(names []string, expected int, requireExecutor bool) error {
	if len(names) != expected {
		return fmt.Errorf("materialized tools/list contains %d tools, expected %d", len(names), expected)
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	for index, name := range sorted {
		if strings.TrimSpace(name) == "" || (index > 0 && name == sorted[index-1]) {
			return errors.New("materialized tools/list contains an empty or duplicate tool name")
		}
	}
	required := []string{requiredRuntimeTool, requiredSandboxTool}
	if requireExecutor {
		required = append(required, optionalExecutorTool)
	}
	for _, name := range required {
		index := sort.SearchStrings(sorted, name)
		if index == len(sorted) || sorted[index] != name {
			return fmt.Errorf("materialized tools/list is missing %s", name)
		}
	}
	return nil
}

func (c *routingClient) callTool(name string, arguments map[string]any) (string, error) {
	envelope, err := c.sessionRPC("tools/call", map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return "", err
	}
	var result toolResult
	if err := decodeJSON(envelope.Result, &result); err != nil {
		return "", fmt.Errorf("decoding %s result: %w", name, err)
	}
	if result.IsError || len(result.Content) != 1 || result.Content[0].Type != "text" {
		return "", fmt.Errorf("%s returned an error or invalid content shape", name)
	}
	return result.Content[0].Text, nil
}

func (c *routingClient) verifyRuntimeIdentity() error {
	text, err := c.callTool(requiredRuntimeTool, map[string]any{})
	if err != nil {
		return fmt.Errorf("calling %s: %w", requiredRuntimeTool, err)
	}
	var actual versionResponse
	if err := decodeExact([]byte(text), &actual); err != nil {
		return fmt.Errorf("decoding %s: %w", requiredRuntimeTool, err)
	}
	if actual.Status != c.expected.Status || actual.Version != c.expected.Version || actual.ProtocolVersion != c.expected.ProtocolVersion || actual.Commit != c.expected.Commit || actual.ToolCount != c.expected.ToolCount || actual.CatalogHash != c.expected.CatalogHash {
		return errors.New("system_runtime_info does not match the authenticated transport identity")
	}
	return nil
}

func (c *routingClient) verifySandboxExecution(cwd string) error {
	before, err := c.callTool(optionalExecutorTool, map[string]any{"command": []string{"git", "status", "--porcelain=v1"}, "cwd": cwd})
	if err != nil {
		return fmt.Errorf("reading pre-probe workspace state: %w", err)
	}
	if _, err := c.callTool(optionalExecutorTool, map[string]any{"command": []string{"pwd"}, "cwd": cwd}); err != nil {
		return fmt.Errorf("executing pwd probe: %w", err)
	}
	head, err := c.callTool(optionalExecutorTool, map[string]any{"command": []string{"git", "rev-parse", "HEAD"}, "cwd": cwd})
	if err != nil {
		return fmt.Errorf("executing git identity probe: %w", err)
	}
	if !gitObjectPattern.MatchString(head) {
		return errors.New("git identity probe returned no full object id")
	}
	after, err := c.callTool(optionalExecutorTool, map[string]any{"command": []string{"git", "status", "--porcelain=v1"}, "cwd": cwd})
	if err != nil {
		return fmt.Errorf("reading post-probe workspace state: %w", err)
	}
	if before != after {
		return errors.New("sandbox execution probes changed the workspace state")
	}
	return nil
}

func (c *routingClient) rpc(method, sessionID string, payload map[string]any) (*http.Response, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, errors.New("encoding MCP request failed")
	}
	request, err := http.NewRequest(method, c.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, errors.New("creating MCP request failed")
	}
	request.Header.Set("Authorization", "Bearer "+c.credential)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if sessionID != "" {
		request.Header.Set("Mcp-Session-Id", sessionID)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, nil, errors.New("MCP request transport failed")
	}
	defer response.Body.Close()
	responseBody, err := readBounded(response.Body)
	if err != nil {
		return nil, nil, err
	}
	return response, responseBody, nil
}

func readBounded(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxSmokeResponse+1))
	if err != nil {
		return nil, errors.New("reading smoke response failed")
	}
	if len(body) > maxSmokeResponse {
		return nil, fmt.Errorf("smoke response exceeds %d bytes", maxSmokeResponse)
	}
	return body, nil
}

func decodeExact(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decodeOne(decoder, destination)
}

func decodeJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	return decodeOne(decoder, destination)
}

func decodeOne(decoder *json.Decoder, destination any) error {
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("response contains trailing JSON data")
	}
	return nil
}
