package mcpserver

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

var publicEdgeOperationIDPattern = regexp.MustCompile(`^eo_[a-f0-9]{32}$`)

type edgeOperationListParams struct {
	Target string `json:"target"`
	Limit  int    `json:"limit,omitempty"`
}

type edgeOperationIDParams struct {
	OperationID string `json:"operation_id"`
}

type edgeOperationLifecyclePublicView struct {
	OperationID     string                  `json:"operation_id"`
	Kind            edge.OperationKind      `json:"kind"`
	State           edge.OperationState     `json:"state"`
	CancelRequested bool                    `json:"cancel_requested"`
	Cancellable     bool                    `json:"cancellable"`
	Progress        *edge.OperationProgress `json:"progress,omitempty"`
	ProjectAlias    string                  `json:"project_alias,omitempty"`
	Target          string                  `json:"target,omitempty"`
	Reason          string                  `json:"reason,omitempty"`
	CreatedAt       time.Time               `json:"created_at"`
	QueueUS         *int64                  `json:"queue_us,omitempty"`
	PickupUS        *int64                  `json:"pickup_us,omitempty"`
	EdgeWorkUS      *int64                  `json:"edge_work_us,omitempty"`
	CompletionUS    *int64                  `json:"completion_us,omitempty"`
	TotalUS         *int64                  `json:"total_us,omitempty"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

func (s *Server) addEdgeOperationLifecycleTools(projectSchema map[string]any) {
	operationIDSchema := stringSchema("durable Edge operation id", `^eo_[a-f0-9]{32}$`, 35)
	readHints := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	s.addDirectTool(toolDef{
		Name: "edge_operation_list", Description: "List bounded non-sensitive queued and running operations for one paired Edge target so another chat can recover current work without already knowing an operation id.",
		InputSchema: closedObject(map[string]any{
			"target": projectSchema["target"],
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
		}, []string{"target"}), Version: "1", Annotations: readHints,
	}, s.handleEdgeOperationList)
	s.addDirectTool(toolDef{
		Name: "edge_operation_status", Description: "Return bounded durable state, progress and cancellation metadata for one Edge operation without exposing device, workspace, path, request body or raw output.",
		InputSchema: closedObject(map[string]any{"operation_id": operationIDSchema}, []string{"operation_id"}), Version: "1", Annotations: readHints,
	}, s.handleEdgeOperationStatus)
	s.addDirectTool(toolDef{
		Name: "edge_operation_cancel", Description: "Request idempotent cancellation of one queued operation or one interruptible running Edge operation. The Edge observes a running request through its signed progress heartbeat and records a cancelled terminal state.",
		InputSchema: closedObject(map[string]any{"operation_id": operationIDSchema}, []string{"operation_id"}), Version: "1",
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false},
	}, s.handleEdgeOperationCancel)
}

func (s *Server) handleEdgeOperationList(arguments json.RawMessage) (string, error) {
	if s.edgeOperations == nil || s.edgeDevices == nil {
		return "", errEdgeStoreUnavailable
	}
	resolver, ok := s.edgeDevices.(edgeDeviceAliasRegistry)
	if !ok {
		return "", errors.New("edge target alias resolution is unavailable")
	}
	var params edgeOperationListParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	if params.Limit == 0 {
		params.Limit = 20
	}
	if params.Limit < 1 || params.Limit > 50 {
		return "", errors.New("operation list limit is invalid")
	}
	device, err := resolver.ResolveActiveDeviceName(params.Target)
	if err != nil {
		return "", err
	}
	operations, err := s.edgeOperations.ActiveOperations(device.ID, params.Limit)
	if err != nil {
		return "", err
	}
	views := make([]edgeOperationLifecyclePublicView, 0, len(operations))
	for _, operation := range operations {
		views = append(views, publicEdgeOperationLifecycle(operation))
	}
	return marshalToolValue(views, nil)
}

func (s *Server) handleEdgeOperationStatus(arguments json.RawMessage) (string, error) {
	if s.edgeOperations == nil {
		return "", errEdgeStoreUnavailable
	}
	var params edgeOperationIDParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	if !publicEdgeOperationIDPattern.MatchString(params.OperationID) {
		return "", errors.New("operation id is invalid")
	}
	operation, err := s.edgeOperations.OperationLifecycleStatus(params.OperationID)
	return marshalToolValue(publicEdgeOperationLifecycle(operation), err)
}

func (s *Server) handleEdgeOperationCancel(arguments json.RawMessage) (string, error) {
	if s.edgeOperations == nil {
		return "", errEdgeStoreUnavailable
	}
	var params edgeOperationIDParams
	if err := decodeClosed(arguments, &params); err != nil {
		return "", err
	}
	if !publicEdgeOperationIDPattern.MatchString(params.OperationID) {
		return "", errors.New("operation id is invalid")
	}
	operation, err := s.edgeOperations.RequestOperationCancel(params.OperationID)
	return marshalToolValue(publicEdgeOperationLifecycle(operation), err)
}

func publicEdgeOperationLifecycle(operation edge.Operation) edgeOperationLifecyclePublicView {
	var progress *edge.OperationProgress
	if operation.Progress.Revision > 0 {
		value := operation.Progress
		progress = &value
	}
	return edgeOperationLifecyclePublicView{
		OperationID:     operation.ID,
		Kind:            operation.Kind,
		State:           operation.State,
		CancelRequested: operation.CancelRequested,
		Cancellable:     edge.OperationCanCancel(operation),
		Progress:        progress,
		ProjectAlias:    operation.Request.Alias,
		Target:          operation.Request.TargetAlias,
		Reason:          operation.SafeCode,
		CreatedAt:       operation.CreatedAt,
		QueueUS:         operationDurationMicros(operation.CreatedAt, operation.LeasedAt),
		PickupUS:        operationDurationMicros(operation.LeasedAt, operation.RunningAt),
		EdgeWorkUS:      operationDurationMicros(operation.RunningAt, operation.FinalizingAt),
		CompletionUS:    operationDurationMicros(operation.FinalizingAt, operation.UpdatedAt),
		TotalUS:         operationDurationMicros(operation.CreatedAt, operation.UpdatedAt),
		UpdatedAt:       operation.UpdatedAt,
	}
}

func operationDurationMicros(start, end time.Time) *int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return nil
	}
	value := end.Sub(start).Microseconds()
	return &value
}
