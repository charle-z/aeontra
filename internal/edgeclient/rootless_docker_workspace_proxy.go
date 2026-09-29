//go:build !windows

package edgeclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	rootlessDockerProxySocketName = "c.sock"
	maxDockerCreateBody           = 16 << 20
)

// startRootlessDockerWorkspaceProxy keeps Docker's user socket on the host side
// of the workcell. Only container-create bind sources beneath /workspace are
// translated; every other Docker API request retains the existing rootless
// authority and semantics.
func startRootlessDockerWorkspaceProxy(ctx context.Context, endpoint RootlessContainerEndpoint, workspace, runtimeDir string, uid int) (string, <-chan error, func(), error) {
	if endpoint.Engine != "docker" || !filepath.IsAbs(workspace) || !filepath.IsAbs(runtimeDir) || uid <= 0 {
		return "", nil, nil, errors.New("rootless Docker workspace proxy contract is invalid")
	}
	runtimeRoot := filepath.Join("/run/user", strconv.Itoa(uid))
	if err := validateRootlessContainerSocket(endpoint.SocketPath, runtimeRoot, uid); err != nil {
		return "", nil, nil, errors.New("rootless Docker endpoint changed before runtime start")
	}
	aliasDir, err := os.MkdirTemp(runtimeRoot, "mcp-devbox-bind-")
	if err != nil {
		return "", nil, nil, errors.New("rootless Docker bind alias directory is unavailable")
	}
	socketPath := filepath.Join(runtimeDir, rootlessDockerProxySocketName)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(aliasDir)
		return "", nil, nil, errors.New("rootless Docker proxy socket is unavailable")
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		_ = os.RemoveAll(aliasDir)
		return "", nil, nil, errors.New("rootless Docker proxy socket permissions failed")
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", endpoint.SocketPath)
	}}
	upstream, _ := url.Parse("http://docker")
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Transport = transport
	proxy.FlushInterval = -1
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(writer, "rootless Docker endpoint unavailable", http.StatusBadGateway)
	}
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost && dockerContainerCreatePath(request.URL.Path) {
			body, readErr := io.ReadAll(io.LimitReader(request.Body, maxDockerCreateBody+1))
			if readErr != nil || len(body) > maxDockerCreateBody {
				http.Error(writer, "Docker container create request is too large", http.StatusRequestEntityTooLarge)
				return
			}
			rewritten, rewriteErr := rewriteDockerCreateWorkspaceBinds(body, workspace, aliasDir)
			if rewriteErr != nil {
				http.Error(writer, "Docker workspace bind source is invalid", http.StatusBadRequest)
				return
			}
			request.Body = io.NopCloser(bytes.NewReader(rewritten))
			request.ContentLength = int64(len(rewritten))
			request.TransferEncoding = nil
		}
		proxy.ServeHTTP(writer, request)
	})}
	done := make(chan error, 1)
	go func() {
		serveErr := server.Serve(listener)
		if ctx.Err() == nil {
			if serveErr == nil || errors.Is(serveErr, http.ErrServerClosed) {
				serveErr = errors.New("rootless Docker workspace proxy stopped unexpectedly")
			}
			done <- serveErr
		} else {
			done <- nil
		}
	}()
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	cleanup := func() {
		_ = server.Close()
		transport.CloseIdleConnections()
		_ = os.Remove(socketPath)
		_ = os.RemoveAll(aliasDir)
	}
	return socketPath, done, cleanup, nil
}

func dockerContainerCreatePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 {
		return parts[0] == "containers" && parts[1] == "create"
	}
	return len(parts) == 3 && strings.HasPrefix(parts[0], "v1.") && parts[1] == "containers" && parts[2] == "create"
}

func rewriteDockerCreateWorkspaceBinds(body []byte, workspace, aliasDir string) ([]byte, error) {
	var create map[string]json.RawMessage
	if err := json.Unmarshal(body, &create); err != nil {
		return nil, err
	}
	if len(create["HostConfig"]) == 0 || string(create["HostConfig"]) == "null" {
		return body, nil
	}
	var host map[string]json.RawMessage
	if err := json.Unmarshal(create["HostConfig"], &host); err != nil {
		return nil, err
	}
	changed := false
	if raw := host["Binds"]; len(raw) > 0 && string(raw) != "null" {
		var binds []string
		if err := json.Unmarshal(raw, &binds); err != nil {
			return nil, err
		}
		for index, bind := range binds {
			parts := strings.SplitN(bind, ":", 2)
			if len(parts) != 2 || !workspaceDockerPath(parts[0]) {
				continue
			}
			alias, err := aliasWorkspaceDockerPath(parts[0], workspace, aliasDir)
			if err != nil {
				return nil, err
			}
			binds[index] = alias + ":" + parts[1]
			changed = true
		}
		if changed {
			host["Binds"], _ = json.Marshal(binds)
		}
	}
	if raw := host["Mounts"]; len(raw) > 0 && string(raw) != "null" {
		var mounts []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &mounts); err != nil {
			return nil, err
		}
		mountsChanged := false
		for _, mount := range mounts {
			var kind, source string
			if err := json.Unmarshal(mount["Type"], &kind); err != nil {
				continue
			}
			if err := json.Unmarshal(mount["Source"], &source); err != nil || kind != "bind" || !workspaceDockerPath(source) {
				continue
			}
			alias, err := aliasWorkspaceDockerPath(source, workspace, aliasDir)
			if err != nil {
				return nil, err
			}
			mount["Source"], _ = json.Marshal(alias)
			mountsChanged, changed = true, true
		}
		if mountsChanged {
			host["Mounts"], _ = json.Marshal(mounts)
		}
	}
	if !changed {
		return body, nil
	}
	var err error
	create["HostConfig"], err = json.Marshal(host)
	if err != nil {
		return nil, err
	}
	return json.Marshal(create)
}

func workspaceDockerPath(path string) bool {
	return path == openCodeSandboxWorkspace || strings.HasPrefix(path, openCodeSandboxWorkspace+"/")
}

func aliasWorkspaceDockerPath(source, workspace, aliasDir string) (string, error) {
	if !workspaceDockerPath(source) {
		return "", errors.New("docker bind source is outside the workcell")
	}
	for _, part := range strings.Split(source, "/") {
		if part == ".." {
			return "", errors.New("docker bind source contains traversal")
		}
	}
	relative, err := filepath.Rel(openCodeSandboxWorkspace, filepath.Clean(source))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errors.New("docker bind source is outside the workcell")
	}
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", errors.New("docker workspace root is unavailable")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, relative))
	if err != nil || !pathInside(root, resolved) {
		return "", errors.New("docker bind source is missing or escapes the workcell")
	}
	digest := sha256.Sum256([]byte(resolved))
	alias := filepath.Join(aliasDir, hex.EncodeToString(digest[:]))
	if err := os.Symlink(resolved, alias); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", errors.New("docker bind alias could not be created")
		}
		previous, readErr := os.Readlink(alias)
		if readErr != nil || previous != resolved {
			return "", fmt.Errorf("docker bind alias identity changed: %w", err)
		}
	}
	return alias, nil
}
