package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
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
		payload := map[string]any{
			"key": key, "value": vars[key],
			"comment":    frontdoorcoordinator.ManagedEnvironmentComment(c.token, key, vars[key]),
			"is_preview": false, "is_literal": true,
			"is_runtime": true, "is_buildtime": false,
		}
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

func (c *CoolifyClient) deleteEnvironmentVariable(ctx context.Context, app, envUUID string) error {
	if !coolifyUUIDRe.MatchString(envUUID) {
		return fmt.Errorf("coolify environment identity is invalid")
	}
	path := "/api/v1/applications/" + url.PathEscape(app) + "/envs/" + url.PathEscape(envUUID)
	status, _, err := c.request(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("coolify delete env request failed: %w", err)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("coolify delete env -> HTTP %d", status)
	}
	return nil
}
