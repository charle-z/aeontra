package tools

import (
	"context"
	"fmt"
	"net/http"
)

func (c *CoolifyClient) setEnvironmentVariables(ctx context.Context, app string, vars map[string]string, keys []string) ([]string, error) {
	entries, err := c.listEnvironmentVariables(ctx, app)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string][]coolifyEnvironmentVariable, len(entries))
	for _, entry := range entries {
		if entry.IsPreview {
			continue
		}
		byKey[entry.Key] = append(byKey[entry.Key], entry)
	}
	for _, key := range keys {
		if len(byKey[key]) > 1 {
			return nil, fmt.Errorf("coolify environment conflict: key %s exists more than once", key)
		}
	}

	summaries := make([]string, 0, len(keys))
	basePath := "/api/v1/applications/" + app + "/envs"
	for _, key := range keys {
		payload := map[string]any{"key": key, "value": vars[key]}
		method := http.MethodPost
		path := basePath
		operation := "created"
		if existing := byKey[key]; len(existing) == 1 {
			method = http.MethodPatch
			operation = "updated"
		}
		status, _, err := c.request(ctx, method, path, payload)
		if err != nil {
			return nil, fmt.Errorf("coolify %s env request failed: %w", operation, err)
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("coolify %s env %s -> HTTP %d", operation, key, status)
		}
		summaries = append(summaries, fmt.Sprintf("%s -> %s (HTTP %d)", key, operation, status))
	}
	return summaries, nil
}
