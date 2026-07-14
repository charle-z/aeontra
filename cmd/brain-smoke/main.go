// Command brain-smoke verifies a deployed configured Brain without printing tokens,
// note bodies, search queries, private paths, or context contents.
package main

import (
	"bytes"
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

	brainpkg "github.com/charle-z/mcp-devbox/internal/brain"
	"github.com/charle-z/mcp-devbox/internal/mcpserver"
)

const (
	defaultBearerEnv   = "MCP_DEVBOX_TOKEN"
	maxSmokeResponse   = 64 << 10
	maxSmokeContextLen = brainpkg.MaxContextBytes
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
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Getenv, nil); err != nil {
		fmt.Fprintln(os.Stderr, "brain-smoke:", err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer, getenv func(string) string, suppliedClient *http.Client) error {
	flags := flag.NewFlagSet("brain-smoke", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	baseURL := flags.String("url", "", "deployed HTTPS base URL or /mcp URL")
	expectedCommit := flags.String("expected-commit", "", "exact commit expected from the deployment")
	bearerEnv := flags.String("bearer-env", defaultBearerEnv, "environment variable containing the bearer credential")
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
	if err := validateEnvName(*bearerEnv); err != nil {
		return err
	}
	credential := ""
	if getenv != nil {
		credential = strings.TrimSpace(getenv(*bearerEnv))
	}
	if credential == "" {
		return fmt.Errorf("configured bearer environment variable is empty")
	}
	endpoint, err := mcpEndpoint(*baseURL)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: *timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if suppliedClient != nil {
		*client = *suppliedClient
		client.Timeout = *timeout
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	local, err := mcpserver.New(nil).RuntimeInfo()
	if err != nil {
		return fmt.Errorf("building local catalog identity: %w", err)
	}
	remote, err := readVersion(client, endpoint)
	if err != nil {
		return err
	}
	if err := validateVersion(remote, strings.TrimSpace(*expectedCommit), local); err != nil {
		return err
	}

	statusText, err := callTool(client, endpoint, credential, 1, "brain_index", map[string]any{"action": "status"})
	if err != nil {
		return err
	}
	var status brainpkg.IndexStatus
	decoder := json.NewDecoder(strings.NewReader(statusText))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&status); err != nil {
		return fmt.Errorf("decoding Brain index status: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return err
	}
	if !status.Ready || status.SchemaVersion != brainpkg.IndexSchemaVersion || status.NoteCount < 0 || status.SourceBytes < 0 || status.LinkCount < 0 || status.BrokenLinkCount < 0 {
		return fmt.Errorf("Brain index status is invalid")
	}
	contextText, err := callTool(client, endpoint, credential, 2, "brain_context", map[string]any{"limit": brainpkg.MaxContextNotes})
	if err != nil {
		return err
	}
	if len(contextText) > maxSmokeContextLen {
		return fmt.Errorf("Brain context exceeds %d bytes", maxSmokeContextLen)
	}

	fmt.Fprintln(output, "brain smoke passed")
	fmt.Fprintf(output, "url=%s\n", endpoint.Redacted())
	fmt.Fprintf(output, "commit=%s\n", remote.Commit)
	fmt.Fprintf(output, "tool_count=%d\n", remote.ToolCount)
	fmt.Fprintf(output, "catalog_hash=%s\n", remote.CatalogHash)
	fmt.Fprintf(output, "index_ready=%t\n", status.Ready)
	fmt.Fprintf(output, "schema_version=%d\n", status.SchemaVersion)
	fmt.Fprintf(output, "note_count=%d\n", status.NoteCount)
	fmt.Fprintf(output, "context_bytes=%d\n", len(contextText))
	return nil
}

func validateEnvName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return fmt.Errorf("--bearer-env must name a non-empty environment variable")
	}
	for index, char := range value {
		if !((char >= 'A' && char <= 'Z') || char == '_' || (index > 0 && char >= '0' && char <= '9')) {
			return fmt.Errorf("--bearer-env is invalid")
		}
	}
	return nil
}

func readVersion(client *http.Client, endpoint *url.URL) (versionResponse, error) {
	versionURL := *endpoint
	versionURL.Path = "/version"
	request, err := http.NewRequest(http.MethodGet, versionURL.String(), nil)
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
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var remote versionResponse
	if err := decoder.Decode(&remote); err != nil {
		return versionResponse{}, fmt.Errorf("decoding deployed version: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return versionResponse{}, err
	}
	if response.Header.Get("X-MCP-Server-Commit") != remote.Commit || response.Header.Get("X-MCP-Catalog-Hash") != remote.CatalogHash {
		return versionResponse{}, fmt.Errorf("runtime identity headers do not match version body")
	}
	count, err := strconv.Atoi(response.Header.Get("X-MCP-Tool-Count"))
	if err != nil || count != remote.ToolCount {
		return versionResponse{}, fmt.Errorf("runtime tool count header does not match version body")
	}
	return remote, nil
}

func validateVersion(remote versionResponse, expectedCommit string, local mcpserver.RuntimeInfo) error {
	if remote.Status != "ok" || remote.Commit != expectedCommit {
		return fmt.Errorf("deployed Brain runtime commit/status mismatch")
	}
	if remote.Version != local.Version || remote.ProtocolVersion != local.ProtocolVersion || remote.ToolCount != local.ToolCount || remote.CatalogHash != local.CatalogHash {
		return fmt.Errorf("deployed Brain catalog identity does not match local source")
	}
	return nil
}

func callTool(client *http.Client, endpoint *url.URL, credential string, id int, name string, arguments map[string]any) (string, error) {
	requestBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	})
	if err != nil {
		return "", fmt.Errorf("encoding Brain smoke request: %w", err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return "", fmt.Errorf("creating Brain smoke request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("requesting Brain operation: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Brain operation returned HTTP %d", response.StatusCode)
	}
	body, err := readBounded(response.Body)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var envelope rpcEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return "", fmt.Errorf("decoding Brain RPC response: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return "", err
	}
	if envelope.Error != nil {
		return "", fmt.Errorf("Brain RPC failed with code %d", envelope.Error.Code)
	}
	var result toolResult
	resultDecoder := json.NewDecoder(bytes.NewReader(envelope.Result))
	resultDecoder.DisallowUnknownFields()
	if err := resultDecoder.Decode(&result); err != nil {
		return "", fmt.Errorf("decoding Brain tool result: %w", err)
	}
	if err := ensureEOF(resultDecoder); err != nil {
		return "", err
	}
	if result.IsError || len(result.Content) != 1 || result.Content[0].Type != "text" {
		return "", fmt.Errorf("Brain tool returned an error or invalid content shape")
	}
	return result.Content[0].Text, nil
}

func readBounded(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxSmokeResponse+1))
	if err != nil {
		return nil, fmt.Errorf("reading Brain smoke response: %w", err)
	}
	if len(body) > maxSmokeResponse {
		return nil, fmt.Errorf("Brain smoke response exceeds %d bytes", maxSmokeResponse)
	}
	return body, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("Brain smoke response contains trailing JSON data")
	}
	return nil
}

func mcpEndpoint(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("--url is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing --url: %w", err)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("--url must include a host without credentials, query parameters, or fragments")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return nil, fmt.Errorf("--url must use HTTPS except for loopback testing")
	}
	switch strings.TrimRight(parsed.EscapedPath(), "/") {
	case "", mcpserver.DefaultMCPPath:
		parsed.Path = mcpserver.DefaultMCPPath
		parsed.RawPath = ""
	default:
		return nil, fmt.Errorf("--url path must be empty, /, or /mcp")
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
