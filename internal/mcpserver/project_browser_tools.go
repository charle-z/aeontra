package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

type projectBrowserCreateParams struct {
	Alias             string `json:"alias"`
	Target            string `json:"target"`
	IdempotencyKey    string `json:"idempotency_key"`
	NetworkScope      string `json:"network_scope"`
	InitialURL        string `json:"initial_url,omitempty"`
	ViewportWidth     int    `json:"viewport_width,omitempty"`
	ViewportHeight    int    `json:"viewport_height,omitempty"`
	IgnoreHTTPSErrors bool   `json:"ignore_https_errors,omitempty"`
}
type projectBrowserSessionParams struct {
	Alias     string `json:"alias"`
	Target    string `json:"target"`
	SessionID string `json:"session_id"`
}
type projectBrowserRunParams struct {
	Alias          string             `json:"alias"`
	Target         string             `json:"target"`
	SessionID      string             `json:"session_id"`
	IdempotencyKey string             `json:"idempotency_key"`
	Steps          []edge.BrowserStep `json:"steps"`
	Capture        string             `json:"capture"`
	FullPage       bool               `json:"full_page,omitempty"`
	TimeoutSeconds int                `json:"timeout_seconds"`
}
type projectBrowserArtifactParams struct {
	Alias      string `json:"alias"`
	Target     string `json:"target"`
	SessionID  string `json:"session_id"`
	ArtifactID string `json:"artifact_id"`
	Offset     int64  `json:"offset"`
	Limit      int    `json:"limit"`
}
type projectBrowserCloseParams struct {
	Alias          string `json:"alias"`
	Target         string `json:"target"`
	SessionID      string `json:"session_id"`
	IdempotencyKey string `json:"idempotency_key"`
}
type projectBrowserListParams struct {
	Alias  string `json:"alias"`
	Target string `json:"target"`
}
type projectBrowserCleanupParams struct {
	Alias          string `json:"alias"`
	Target         string `json:"target"`
	SessionID      string `json:"session_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

type projectBrowserPublicView struct {
	OperationID        string                       `json:"operation_id"`
	State              edge.OperationState          `json:"state"`
	Alias              string                       `json:"alias"`
	Repository         string                       `json:"repository,omitempty"`
	Target             string                       `json:"target"`
	Profile            string                       `json:"profile,omitempty"`
	Mode               string                       `json:"mode,omitempty"`
	Reused             bool                         `json:"reused"`
	Reason             string                       `json:"reason,omitempty"`
	SessionID          string                       `json:"session_id,omitempty"`
	SessionState       string                       `json:"session_state,omitempty"`
	NetworkScope       string                       `json:"network_scope,omitempty"`
	SafeURL            string                       `json:"safe_url,omitempty"`
	Title              string                       `json:"title,omitempty"`
	Revision           uint64                       `json:"revision,omitempty"`
	CreatedAt          string                       `json:"created_at,omitempty"`
	UpdatedAt          string                       `json:"updated_at,omitempty"`
	Text               string                       `json:"text,omitempty"`
	TextTruncated      bool                         `json:"text_truncated,omitempty"`
	ArtifactID         string                       `json:"artifact_id,omitempty"`
	ArtifactMediaType  string                       `json:"artifact_media_type,omitempty"`
	ArtifactBytes      int64                        `json:"artifact_bytes,omitempty"`
	ArtifactSHA256     string                       `json:"artifact_sha256,omitempty"`
	ArtifactOffset     int64                        `json:"artifact_offset,omitempty"`
	ArtifactNext       int64                        `json:"artifact_next,omitempty"`
	ArtifactEOF        bool                         `json:"artifact_eof,omitempty"`
	ArtifactDataBase64 string                       `json:"artifact_data_base64,omitempty"`
	Sessions           []edge.BrowserSessionSummary `json:"sessions,omitempty"`
	CleanupRemoved     int                          `json:"cleanup_removed,omitempty"`
	CleanupArtifacts   int                          `json:"cleanup_artifacts,omitempty"`
}

func (s *Server) addProjectBrowserTools(projectSchema map[string]any) {
	common := map[string]any{"alias": projectSchema["alias"], "target": projectSchema["target"]}
	idempotency := stringSchema("caller-generated durable operation key", `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`, 128)
	session := stringSchema("opaque managed browser session id", `^br_[a-f0-9]{32}$`, 35)
	artifact := stringSchema("opaque managed browser artifact id", `^ba_[a-f0-9]{32}$`, 35)
	stepSchema := closedObject(map[string]any{
		"action": map[string]any{"type": "string", "enum": []string{"navigate", "click", "type", "press", "select", "wait"}},
		"url":    map[string]any{"type": "string", "maxLength": 2048}, "selector": map[string]any{"type": "string", "maxLength": 1024},
		"selector_type": map[string]any{"type": "string", "enum": []string{"css", "text"}}, "text": map[string]any{"type": "string", "maxLength": edge.MaxBrowserTextBytes},
		"key":   map[string]any{"type": "string", "enum": []string{"Enter", "Tab", "Escape", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Home", "End", "PageUp", "PageDown", "Backspace", "Delete"}},
		"value": map[string]any{"type": "string", "maxLength": 4096}, "clear": map[string]any{"type": "boolean"}, "milliseconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 5000},
	}, []string{"action"})

	createProps := copySchema(common)
	createProps["idempotency_key"] = idempotency
	createProps["network_scope"] = map[string]any{"type": "string", "enum": []string{"general"}}
	createProps["initial_url"] = map[string]any{"type": "string", "maxLength": 2048}
	createProps["viewport_width"] = map[string]any{"type": "integer", "minimum": 320, "maximum": 1920}
	createProps["viewport_height"] = map[string]any{"type": "integer", "minimum": 240, "maximum": 1080}
	createProps["ignore_https_errors"] = map[string]any{"type": "boolean"}
	s.addDirectTool(toolDef{Name: "project_browser_create", Description: "Create or reuse one durable managed browser session on the selected trusted Edge project. The private profile stays on the Edge and the browser uses the authorized workcell network for general HTTP(S), including Internet, private development endpoints and localhost.", InputSchema: closedObject(createProps, []string{"alias", "target", "idempotency_key", "network_scope"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}}, s.handleProjectBrowserCreate)

	statusProps := copySchema(common)
	statusProps["session_id"] = session
	s.addDirectTool(toolDef{Name: "project_browser_status", Description: "Read bounded safe metadata for one durable managed browser session without exposing cookies, profile paths, full URLs, browser flags, sockets or process identity.", InputSchema: closedObject(statusProps, []string{"alias", "target", "session_id"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}, func(a json.RawMessage) (string, error) {
		return s.handleProjectBrowserSessionRead(a, edge.OperationProjectBrowserStatus)
	})
	s.addDirectTool(toolDef{Name: "project_browser_list", Description: "List at most 20 managed browser session summaries bound to one Edge project and target.", InputSchema: closedObject(common, []string{"alias", "target"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}, s.handleProjectBrowserList)

	runProps := copySchema(common)
	runProps["session_id"] = session
	runProps["idempotency_key"] = idempotency
	runProps["steps"] = map[string]any{"type": "array", "minItems": 1, "maxItems": edge.MaxBrowserSteps, "items": stepSchema}
	runProps["capture"] = map[string]any{"type": "string", "enum": []string{"none", "text", "screenshot", "both"}}
	runProps["full_page"] = map[string]any{"type": "boolean"}
	runProps["timeout_seconds"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 120}
	s.addDirectTool(toolDef{Name: "project_browser_run", Description: "Execute up to 32 closed navigation and locator actions in one ephemeral Chromium instance backed by a durable Edge-private profile. No arbitrary JavaScript, headers, cookies, executable, flags, filesystem path or CDP endpoint is accepted. Page interaction may submit external effects.", InputSchema: closedObject(runProps, []string{"alias", "target", "session_id", "idempotency_key", "steps", "capture", "timeout_seconds"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": true}}, s.handleProjectBrowserRun)

	artifactProps := copySchema(common)
	artifactProps["session_id"] = session
	artifactProps["artifact_id"] = artifact
	artifactProps["offset"] = map[string]any{"type": "integer", "minimum": 0}
	artifactProps["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": edge.MaxBrowserArtifactChunk}
	s.addDirectTool(toolDef{Name: "project_browser_artifact_read", Description: "Read one exact bounded base64 chunk from an Edge-private JPEG browser artifact by opaque session and artifact identities.", InputSchema: closedObject(artifactProps, []string{"alias", "target", "session_id", "artifact_id", "offset", "limit"}), Version: "1", Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}, s.handleProjectBrowserArtifactRead)

	closeProps := copySchema(statusProps)
	closeProps["idempotency_key"] = idempotency
	s.addDirectTool(toolDef{Name: "project_browser_close", Description: "Idempotently close one non-busy managed browser session while preserving its private profile and artifacts until explicit cleanup.", InputSchema: closedObject(closeProps, []string{"alias", "target", "session_id", "idempotency_key"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false}}, func(a json.RawMessage) (string, error) { return s.handleProjectBrowserClose(a) })
	cleanupProps := copySchema(common)
	cleanupProps["session_id"] = session
	cleanupProps["idempotency_key"] = idempotency
	s.addDirectTool(toolDef{Name: "project_browser_cleanup", Description: "Explicitly remove only closed managed browser sessions, profiles and artifacts bound to the selected Edge project. Active and ready sessions are preserved.", InputSchema: closedObject(cleanupProps, []string{"alias", "target", "idempotency_key"}), Version: "1", Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false}}, s.handleProjectBrowserCleanup)
}

func copySchema(input map[string]any) map[string]any {
	output := make(map[string]any, len(input)+8)
	for k, v := range input {
		output[k] = v
	}
	return output
}

func (s *Server) handleProjectBrowserCreate(arguments json.RawMessage) (string, error) {
	var p projectBrowserCreateParams
	if err := decodeClosed(arguments, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowser(p.Alias, p.Target, edge.OperationProjectBrowserCreate, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", IdempotencyKey: p.IdempotencyKey, BrowserNetworkScope: p.NetworkScope, BrowserInitialURL: p.InitialURL, BrowserViewportWidth: p.ViewportWidth, BrowserViewportHeight: p.ViewportHeight, BrowserIgnoreHTTPSErrors: p.IgnoreHTTPSErrors})
}
func (s *Server) handleProjectBrowserSessionRead(arguments json.RawMessage, kind edge.OperationKind) (string, error) {
	var p projectBrowserSessionParams
	if err := decodeClosed(arguments, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowser(p.Alias, p.Target, kind, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", BrowserSessionID: p.SessionID})
}
func (s *Server) handleProjectBrowserList(arguments json.RawMessage) (string, error) {
	var p projectBrowserListParams
	if err := decodeClosed(arguments, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowser(p.Alias, p.Target, edge.OperationProjectBrowserList, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell"})
}
func (s *Server) handleProjectBrowserRun(arguments json.RawMessage) (string, error) {
	var p projectBrowserRunParams
	if err := decodeClosed(arguments, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowser(p.Alias, p.Target, edge.OperationProjectBrowserRun, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", BrowserSessionID: p.SessionID, IdempotencyKey: p.IdempotencyKey, BrowserSteps: p.Steps, BrowserCapture: p.Capture, BrowserFullPage: p.FullPage, BrowserTimeoutSeconds: p.TimeoutSeconds})
}
func (s *Server) handleProjectBrowserArtifactRead(arguments json.RawMessage) (string, error) {
	var p projectBrowserArtifactParams
	if err := decodeClosed(arguments, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowser(p.Alias, p.Target, edge.OperationProjectBrowserArtifactRead, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", BrowserSessionID: p.SessionID, BrowserArtifactID: p.ArtifactID, BrowserArtifactOffset: p.Offset, BrowserArtifactLimit: p.Limit})
}
func (s *Server) handleProjectBrowserClose(arguments json.RawMessage) (string, error) {
	var p projectBrowserCloseParams
	if err := decodeClosed(arguments, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowser(p.Alias, p.Target, edge.OperationProjectBrowserClose, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", BrowserSessionID: p.SessionID, IdempotencyKey: p.IdempotencyKey})
}
func (s *Server) handleProjectBrowserCleanup(arguments json.RawMessage) (string, error) {
	var p projectBrowserCleanupParams
	if err := decodeClosed(arguments, &p); err != nil {
		return "", err
	}
	return s.queueProjectBrowser(p.Alias, p.Target, edge.OperationProjectBrowserCleanup, edge.OperationRequest{Alias: p.Alias, TargetAlias: p.Target, Profile: "linux-workcell", BrowserSessionID: p.SessionID, IdempotencyKey: p.IdempotencyKey})
}

func (s *Server) queueProjectBrowser(alias, target string, kind edge.OperationKind, request edge.OperationRequest) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	device, err := resolver.ResolveActiveDeviceName(target)
	if err != nil {
		return "", err
	}
	operation, created, err := s.edgeOperations.CreateOperation(device.ID, kind, request)
	if err == nil {
		operation, err = s.edgeOperations.WaitOperation(context.Background(), operation.ID, 180*time.Second)
	}
	view := projectBrowserPublicView{OperationID: operation.ID, State: operation.State, Alias: alias, Target: target, Reused: err == nil && !created}
	if operation.State == edge.OperationSucceeded {
		result := operation.Result
		view.Alias = result.ProjectAlias
		view.Repository = result.ProjectOwner + "/" + result.ProjectRepository
		view.Target = result.ProjectTarget
		view.Profile = result.ProjectProfile
		view.Mode = result.ProjectMode
		view.SessionID = result.BrowserSessionID
		view.SessionState = result.BrowserState
		view.NetworkScope = result.BrowserNetworkScope
		view.SafeURL = result.BrowserSafeURL
		view.Title = result.BrowserTitle
		view.Revision = result.BrowserRevision
		view.CreatedAt = result.BrowserCreatedAt
		view.UpdatedAt = result.BrowserUpdatedAt
		view.Text = result.BrowserText
		view.TextTruncated = result.BrowserTextTruncated
		view.ArtifactID = result.BrowserArtifactID
		view.ArtifactMediaType = result.BrowserArtifactMediaType
		view.ArtifactBytes = result.BrowserArtifactBytes
		view.ArtifactSHA256 = result.BrowserArtifactSHA256
		view.ArtifactOffset = result.BrowserArtifactOffset
		view.ArtifactNext = result.BrowserArtifactNext
		view.ArtifactEOF = result.BrowserArtifactEOF
		view.ArtifactDataBase64 = result.BrowserArtifactDataBase64
		view.Sessions = result.BrowserSessions
		view.CleanupRemoved = result.BrowserCleanupRemoved
		view.CleanupArtifacts = result.BrowserCleanupArtifacts
	} else if operation.State == edge.OperationFailed || operation.State == edge.OperationCancelled {
		view.Reason = operation.SafeCode
	}
	return marshalToolValue(view, err)
}
