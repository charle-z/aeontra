package edge

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestOperationLeaseJSONRemainsCompatibleWithLegacyEdge(t *testing.T) {
	lease := OperationLease{
		Operation: Operation{
			ID:        "eo_0123456789abcdef0123456789abcdef",
			DeviceID:  "ed_0123456789abcdef0123456789abcdef",
			Kind:      OperationBundleStatus,
			Request:   OperationRequest{},
			State:     OperationLeased,
			CreatedAt: time.Unix(1, 0).UTC(),
			UpdatedAt: time.Unix(2, 0).UTC(),
		},
		LeaseID:          "el_0123456789abcdef0123456789abcdef",
		ExpiresAt:        time.Unix(3, 0).UTC(),
		ControlSignature: "signature",
	}

	body, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte(`"progress"`)) {
		t.Fatalf("empty progress leaked into legacy lease JSON: %s", body)
	}

	var legacy struct {
		Operation struct {
			ID        string          `json:"operation_id"`
			DeviceID  string          `json:"device_id"`
			Kind      OperationKind   `json:"kind"`
			Request   json.RawMessage `json:"request"`
			State     OperationState  `json:"state"`
			Result    json.RawMessage `json:"result"`
			SafeCode  string          `json:"safe_code"`
			CreatedAt time.Time       `json:"created_at"`
			UpdatedAt time.Time       `json:"updated_at"`
		} `json:"operation"`
		LeaseID          string    `json:"lease_id"`
		ExpiresAt        time.Time `json:"lease_expires_at"`
		ControlSignature string    `json:"control_signature"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&legacy); err != nil {
		t.Fatalf("legacy Edge rejected lease JSON: %v\n%s", err, body)
	}
}

func TestOperationJSONIncludesNonEmptyProgress(t *testing.T) {
	body, err := json.Marshal(Operation{
		ID:       "eo_0123456789abcdef0123456789abcdef",
		DeviceID: "ed_0123456789abcdef0123456789abcdef",
		Kind:     OperationBundleStatus,
		Request:  OperationRequest{},
		State:    OperationLeased,
		Progress: OperationProgress{Revision: 2, Phase: "running"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"progress":{"revision":2,"phase":"running"}`)) {
		t.Fatalf("non-empty progress missing from operation JSON: %s", body)
	}
}
