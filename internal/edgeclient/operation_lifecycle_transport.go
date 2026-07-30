package edgeclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

func (t *Transport) ReportOperationProgress(ctx context.Context, operationID, leaseID string, progress edge.OperationProgress) (edge.OperationControl, error) {
	request := map[string]any{"lease_id": leaseID, "progress": progress}
	var control edge.OperationControl
	status, err := t.do(ctx, http.MethodPost, "/edge/v1/operations/"+operationID+"/progress", request, &control)
	if err != nil {
		return edge.OperationControl{}, err
	}
	if status != http.StatusOK {
		return edge.OperationControl{}, fmt.Errorf("edge operation progress rejected with HTTP %d", status)
	}
	return control, nil
}

func (t *Transport) CancelOperation(ctx context.Context, operationID, leaseID string) (edge.Operation, error) {
	request := map[string]any{"lease_id": leaseID}
	var operation edge.Operation
	status, err := t.do(ctx, http.MethodPost, "/edge/v1/operations/"+operationID+"/cancel", request, &operation)
	if err != nil {
		return edge.Operation{}, err
	}
	if status != http.StatusOK {
		return edge.Operation{}, fmt.Errorf("edge operation cancellation rejected with HTTP %d", status)
	}
	return operation, nil
}
