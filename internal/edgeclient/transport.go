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
	"sync"
	"time"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
	"github.com/charle-z/mcp-devbox/internal/edge"
)

const maxEdgeResponse = 1 << 20

type edgeEndpointUnavailableError struct {
	cause error
}

func (e *edgeEndpointUnavailableError) Error() string {
	return "edge endpoint unavailable"
}

func (e *edgeEndpointUnavailableError) Unwrap() error {
	return e.cause
}

type Transport struct {
	identityMu sync.RWMutex
	identity   Identity
	stateRoot  string
	key        ed25519.PrivateKey
	client     *http.Client
	now        func() time.Time
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
	if err := t.ensureControlPublicKey(ctx); err != nil {
		return nil, err
	}
	var lease edge.OperationLease
	request := map[string]any{"lease_seconds": int(ttl.Seconds())}
	status, err := t.do(ctx, http.MethodPost, "/edge/v1/operations/lease", request, &lease)
	if err != nil {
		return nil, err
	}
	// A conflict is the capability probe used by compatibility-aware servers.
	// Retrying with the stamped bundle identity keeps new Edge binaries able to
	// poll older servers, whose strict JSON decoders would reject new fields.
	if status == http.StatusConflict {
		request["edge_protocol"] = buildinfo.EdgeBundleProtocolVersion
		request["edge_catalog"] = buildinfo.EdgeBundleCatalogHash
		status, err = t.do(ctx, http.MethodPost, "/edge/v1/operations/lease", request, &lease)
		if err != nil {
			return nil, err
		}
	}
	if status == http.StatusNoContent {
		return nil, nil
	}
	if status != http.StatusOK {
		if status == http.StatusConflict {
			return nil, errors.New("edge version skew")
		}
		return nil, fmt.Errorf("edge operation lease rejected with HTTP %d", status)
	}
	t.identityMu.RLock()
	controlPublicKey := t.identity.ControlPublicKey
	t.identityMu.RUnlock()
	publicKey, err := edge.DecodePublicKey(controlPublicKey)
	if err != nil {
		return nil, errors.New("edge control trust is invalid")
	}
	signature, err := base64.RawURLEncoding.DecodeString(lease.ControlSignature)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, lease.ControlCanonical(), signature) {
		return nil, errors.New("edge operation signature is invalid")
	}
	return &lease, nil
}

func (t *Transport) ensureControlPublicKey(ctx context.Context) error {
	t.identityMu.RLock()
	identity := t.identity
	t.identityMu.RUnlock()
	if identity.SchemaVersion == 2 {
		_, err := edge.DecodePublicKey(identity.ControlPublicKey)
		if err != nil {
			return errors.New("edge control trust is invalid")
		}
		return nil
	}
	if identity.SchemaVersion != 1 || identity.ControlPublicKey != "" {
		return errors.New("edge control trust is invalid")
	}
	var response struct {
		PublicKey string `json:"public_key"`
	}
	status, err := t.do(ctx, http.MethodPost, "/edge/v1/control-key", struct{}{}, &response)
	if err != nil || status != http.StatusOK {
		return errors.New("edge control trust bootstrap failed")
	}
	if _, err := edge.DecodePublicKey(response.PublicKey); err != nil {
		return errors.New("edge control trust bootstrap failed")
	}
	t.identityMu.Lock()
	defer t.identityMu.Unlock()
	upgraded := t.identity
	if upgraded.SchemaVersion == 2 {
		return nil
	}
	upgraded.SchemaVersion = 2
	upgraded.ControlPublicKey = response.PublicKey
	if err := persistIdentityOnly(t.stateRoot, upgraded); err != nil {
		return err
	}
	t.identity = upgraded
	return nil
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

func (t *Transport) ReportAutopilot(ctx context.Context, result edge.OperationResult) error {
	status, err := t.do(ctx, http.MethodPost, "/edge/v1/autopilot/report", result, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return fmt.Errorf("edge autopilot report rejected with HTTP %d", status)
	}
	return nil
}

func (t *Transport) do(ctx context.Context, method, path string, input, output any) (int, error) {
	return t.doLimited(ctx, method, path, input, output, maxEdgeResponse)
}

func (t *Transport) doLimited(ctx context.Context, method, path string, input, output any, maxResponse int64) (int, error) {
	return t.doLimitedWithClient(ctx, t.client, method, path, input, output, maxResponse)
}

func (t *Transport) doLimitedWithClient(ctx context.Context, client *http.Client, method, path string, input, output any, maxResponse int64) (int, error) {
	if client == nil {
		return 0, errors.New("edge HTTP client is unavailable")
	}
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
	t.identityMu.RLock()
	identity := t.identity
	key := append(ed25519.PrivateKey(nil), t.key...)
	t.identityMu.RUnlock()
	signed := edge.SignedRequest{
		DeviceID:  identity.DeviceID,
		Timestamp: t.now().UTC().Unix(),
		Nonce:     nonce,
		Method:    method,
		Path:      path,
		Body:      body,
	}
	signed.Signature = ed25519.Sign(key, signed.Canonical())
	request, err := http.NewRequestWithContext(ctx, method, identity.ServerURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, errors.New("edge request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(edge.HeaderDevice, signed.DeviceID)
	request.Header.Set(edge.HeaderTimestamp, strconv.FormatInt(signed.Timestamp, 10))
	request.Header.Set(edge.HeaderNonce, signed.Nonce)
	request.Header.Set(edge.HeaderSignature, base64.RawURLEncoding.EncodeToString(signed.Signature))
	response, err := client.Do(request)
	if err != nil {
		return 0, &edgeEndpointUnavailableError{cause: err}
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
