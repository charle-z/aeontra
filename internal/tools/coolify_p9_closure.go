package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const (
	p9BrainAppUUID     = "jqf7qz5ensoqtvl1tb197gcv"
	p9BrainStorageName = "mcp-devbox-brain"
	p9BrainMountPath   = "/brain"
)

var p9BrainStoragePayload = map[string]any{
	"type":       "persistent",
	"name":       p9BrainStorageName,
	"mount_path": p9BrainMountPath,
}

type coolifyEnvironmentVariable struct {
	UUID      string `json:"uuid"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	IsPreview bool   `json:"is_preview"`
}

type coolifyStorage struct {
	UUID      string `json:"uuid"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	MountPath string `json:"mount_path"`
}

func decodeCoolifyCollection[T any](body string) ([]T, error) {
	var direct []T
	if err := json.Unmarshal([]byte(body), &direct); err == nil {
		return direct, nil
	}
	var wrapped struct {
		Data []T `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &wrapped); err == nil && wrapped.Data != nil {
		return wrapped.Data, nil
	}
	return nil, fmt.Errorf("unexpected Coolify collection response")
}

func (c *CoolifyClient) listEnvironmentVariables(ctx context.Context, app string) ([]coolifyEnvironmentVariable, error) {
	status, body, err := c.request(ctx, http.MethodGet, "/api/v1/applications/"+url.PathEscape(app)+"/envs", nil)
	if err != nil {
		return nil, fmt.Errorf("coolify list envs request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("coolify list envs -> HTTP %d", status)
	}
	entries, err := decodeCoolifyCollection[coolifyEnvironmentVariable](body)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func (c *CoolifyClient) listStorages(ctx context.Context) ([]coolifyStorage, error) {
	path := "/api/v1/applications/" + p9BrainAppUUID + "/storages"
	status, body, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("coolify list P9 Brain storages request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("coolify list P9 Brain storages -> HTTP %d", status)
	}
	entries, err := decodeCoolifyStorages(body)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func classifyP9BrainStorage(storages []coolifyStorage) (bool, error) {
	exact := 0
	for _, storage := range storages {
		nameMatches := storage.Name == p9BrainStorageName
		mountMatches := storage.MountPath == p9BrainMountPath
		if !nameMatches && !mountMatches {
			continue
		}
		if nameMatches && mountMatches && storage.Type == "persistent" {
			exact++
			continue
		}
		return false, fmt.Errorf("P9 Brain storage conflict: existing storage reuses the reserved name or mount path")
	}
	if exact > 1 {
		return false, fmt.Errorf("P9 Brain storage conflict: multiple exact persistent storages exist")
	}
	return exact == 1, nil
}

func (c *CoolifyClient) ensureP9BrainStorage(ctx context.Context) (string, error) {
	if c == nil || !c.Configured() {
		return "", fmt.Errorf("P9 Brain storage helper is not configured")
	}
	if !c.appAllowed(p9BrainAppUUID) {
		return "", fmt.Errorf("app %q is not in COOLIFY_ALLOWED_APPS", p9BrainAppUUID)
	}
	storages, err := c.listStorages(ctx)
	if err != nil {
		return "", err
	}
	present, err := classifyP9BrainStorage(storages)
	if err != nil {
		return "", err
	}
	if present {
		return fmt.Sprintf("P9 Brain storage verified: persistent %s mounted at %s on %s", p9BrainStorageName, p9BrainMountPath, p9BrainAppUUID), nil
	}

	path := "/api/v1/applications/" + p9BrainAppUUID + "/storages"
	status, _, err := c.request(ctx, http.MethodPost, path, p9BrainStoragePayload)
	if err != nil {
		return "", fmt.Errorf("coolify create P9 Brain storage request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("coolify create P9 Brain storage -> HTTP %d", status)
	}

	storages, err = c.listStorages(ctx)
	if err != nil {
		return "", fmt.Errorf("P9 Brain storage was created but verification failed: %w", err)
	}
	present, err = classifyP9BrainStorage(storages)
	if err != nil {
		return "", err
	}
	if !present {
		return "", fmt.Errorf("P9 Brain storage creation did not produce the exact persistent storage")
	}
	return fmt.Sprintf("P9 Brain storage created and verified: persistent %s mounted at %s on %s", p9BrainStorageName, p9BrainMountPath, p9BrainAppUUID), nil
}
