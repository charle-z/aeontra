package edgeclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
)

const maxEdgeResponse = 1 << 20

type Transport struct {
	identity  Identity
	stateRoot string
	key       ed25519.PrivateKey
	client    *http.Client
	now       func() time.Time
}

func NewTransport(stateRoot string, client *http.Client) (*Transport, error) {
	identity, key, err := LoadIdentity(stateRoot)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: clientTimeout}
	}
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Transport{identity: identity, stateRoot: filepath.Clean(stateRoot), key: key, client: &clone, now: time.Now}, nil
}

func (t *Transport) Lease(ctx context.Context, holder string, ttl time.Duration) (*edge.Lease, error) {
	request := map[string]any{"workcell": "development", "holder": holder, "lease_seconds": int(ttl.Seconds())}
	var lease edge.Lease
	status, err := t.do(ctx, http.MethodPost, "/edge/v1/tasks/lease", request, &lease)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("edge lease rejected with HTTP %d", status)
	}
	return &lease, nil
}

func (t *Transport) Heartbeat(ctx context.Context, taskID, leaseID string, ttl time.Duration) (edge.HeartbeatStatus, error) {
	request := map[string]any{"lease_id": leaseID, "lease_seconds": int(ttl.Seconds())}
	var status edge.HeartbeatStatus
	code, err := t.do(ctx, http.MethodPost, "/edge/v1/tasks/"+taskID+"/heartbeat", request, &status)
	if err != nil {
		return edge.HeartbeatStatus{}, err
	}
	if code != http.StatusOK {
		return edge.HeartbeatStatus{}, fmt.Errorf("edge heartbeat rejected with HTTP %d", code)
	}
	return status, nil
}

func (t *Transport) Complete(ctx context.Context, taskID, leaseID string, result edge.TaskResult) (edge.Task, error) {
	request := map[string]any{"lease_id": leaseID, "outcome": result.Outcome, "summary": result.Summary}
	if result.ResultRef != "" {
		request["result_ref"] = result.ResultRef
	}
	var task edge.Task
	code, err := t.do(ctx, http.MethodPost, "/edge/v1/tasks/"+taskID+"/complete", request, &task)
	if err != nil {
		return edge.Task{}, err
	}
	if code != http.StatusOK {
		return edge.Task{}, fmt.Errorf("edge completion rejected with HTTP %d", code)
	}
	return task, nil
}

func (t *Transport) LeaseOperation(ctx context.Context, ttl time.Duration) (*edge.OperationLease, error) {
	var lease edge.OperationLease
	status, err := t.do(ctx, http.MethodPost, "/edge/v1/operations/lease", map[string]any{"lease_seconds": int(ttl.Seconds())}, &lease)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("edge operation lease rejected with HTTP %d", status)
	}
	return &lease, nil
}

func (t *Transport) CompleteOperation(ctx context.Context, operationID, leaseID string, result edge.OperationResult, safeCode string) (edge.Operation, error) {
	request := map[string]any{"lease_id": leaseID, "result": result}
	if safeCode != "" {
		request["safe_code"] = safeCode
	}
	var operation edge.Operation
	status, err := t.do(ctx, http.MethodPost, "/edge/v1/operations/"+operationID+"/complete", request, &operation)
	if err != nil {
		return edge.Operation{}, err
	}
	if status != http.StatusOK {
		return edge.Operation{}, fmt.Errorf("edge operation completion rejected with HTTP %d", status)
	}
	return operation, nil
}

func (t *Transport) do(ctx context.Context, method, path string, input, output any) (int, error) {
	return t.doLimited(ctx, method, path, input, output, maxEdgeResponse)
}

func (t *Transport) doLimited(ctx context.Context, method, path string, input, output any, maxResponse int64) (int, error) {
	if maxResponse <= 0 || maxResponse > 8<<20 {
		return 0, errors.New("edge response limit is invalid")
	}
	body, err := marshalEdgeRequest(input)
	if err != nil {
		return 0, errors.New("edge request encoding failed")
	}
	nonceBytes := make([]byte, 18)
	if _, err := rand.Read(nonceBytes); err != nil {
		return 0, errors.New("edge request nonce failed")
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	signed := edge.SignedRequest{
		DeviceID:  t.identity.DeviceID,
		Timestamp: t.now().UTC().Unix(),
		Nonce:     nonce,
		Method:    method,
		Path:      path,
		Body:      body,
	}
	signed.Signature = ed25519.Sign(t.key, signed.Canonical())
	request, err := http.NewRequestWithContext(ctx, method, t.identity.ServerURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, errors.New("edge request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(edge.HeaderDevice, signed.DeviceID)
	request.Header.Set(edge.HeaderTimestamp, strconv.FormatInt(signed.Timestamp, 10))
	request.Header.Set(edge.HeaderNonce, signed.Nonce)
	request.Header.Set(edge.HeaderSignature, base64.RawURLEncoding.EncodeToString(signed.Signature))
	response, err := t.client.Do(request)
	if err != nil {
		return 0, errors.New("edge endpoint unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return response.StatusCode, nil
	}
	limited := io.LimitReader(response.Body, maxResponse+1)
	content, err := io.ReadAll(limited)
	if err != nil || int64(len(content)) > maxResponse {
		return response.StatusCode, errors.New("edge response is invalid")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decodeSingleJSON(decoder, output); err != nil {
		return response.StatusCode, errors.New("edge response is invalid")
	}
	return response.StatusCode, nil
}

func marshalEdgeRequest(input any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(input); err != nil {
		return nil, err
	}
	body := buffer.Bytes()
	if len(body) > 0 && body[len(body)-1] == '\n' {
		body = body[:len(body)-1]
	}
	return append([]byte(nil), body...), nil
}
