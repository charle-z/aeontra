package console

import (
	"context"
	"encoding/json"
	"net/http"
)

const dataPath = "/console/data"

type SystemData struct {
	Available            bool    `json:"available"`
	CPUCount             int     `json:"cpu_count"`
	MemoryTotalBytes     uint64  `json:"memory_total_bytes"`
	MemoryAvailableBytes uint64  `json:"memory_available_bytes"`
	DiskTotalBytes       uint64  `json:"disk_total_bytes"`
	DiskAvailableBytes   uint64  `json:"disk_available_bytes"`
	Load1                float64 `json:"load_1"`
	Load5                float64 `json:"load_5"`
	Load15               float64 `json:"load_15"`
}

type PayloadData struct {
	ProcessStartedAt       string `json:"process_started_at"`
	ToolCallCount          uint64 `json:"tool_call_count"`
	EstimatedPayloadTokens uint64 `json:"estimated_payload_tokens"`
	Warning                string `json:"warning"`
	RequestCount           uint64 `json:"request_count"`
	InputBytes             uint64 `json:"input_bytes"`
	OutputBytes            uint64 `json:"output_bytes"`
	InputTokensEstimate    uint64 `json:"input_tokens_estimate"`
	OutputTokensEstimate   uint64 `json:"output_tokens_estimate"`
	Formula                string `json:"formula"`
}

type BrainNode struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Trust   string `json:"trust"`
	Degree  int    `json:"degree"`
}

type BrainEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type BrainData struct {
	Available       bool        `json:"available"`
	Ready           bool        `json:"ready"`
	SchemaVersion   int         `json:"schema_version"`
	NoteCount       int         `json:"note_count"`
	SourceBytes     int64       `json:"source_bytes"`
	LinkCount       int         `json:"link_count"`
	BrokenLinkCount int         `json:"broken_link_count"`
	IndexedAt       string      `json:"indexed_at"`
	GraphTruncated  bool        `json:"graph_truncated"`
	Nodes           []BrainNode `json:"nodes"`
	Edges           []BrainEdge `json:"edges"`
}

type ObservabilityRoute struct {
	Route     string `json:"route"`
	Requests  uint64 `json:"requests"`
	Client4XX uint64 `json:"client_4xx"`
	Server5XX uint64 `json:"server_5xx"`
	P95MS     int64  `json:"p95_ms"`
}

type ObservabilityData struct {
	Enabled  bool                 `json:"enabled"`
	Failures uint64               `json:"failures"`
	Routes   []ObservabilityRoute `json:"routes"`
}

type SecurityData struct {
	OAuthEnabled     bool   `json:"oauth_enabled"`
	BearerRecovery   bool   `json:"bearer_recovery"`
	QueryAuth        string `json:"query_auth"`
	FreeShell        string `json:"free_shell"`
	Cookie           string `json:"cookie"`
	ConsoleAuthority string `json:"console_authority"`
}

type EdgeData struct {
	State   string           `json:"state"`
	Devices []EdgeDeviceData `json:"devices"`
}

type DataSnapshot struct {
	SchemaVersion   int                 `json:"schema_version"`
	System          SystemData          `json:"system"`
	Payload         PayloadData         `json:"payload"`
	DurableActivity DurableActivityData `json:"durable_activity"`
	Controllers     []ControllerData    `json:"controllers"`
	Runtimes        []RuntimeData       `json:"runtimes"`
	Projects        []ProjectData       `json:"projects"`
	Storage         StorageData         `json:"storage"`
	Brain           BrainData           `json:"brain"`
	Observability   ObservabilityData   `json:"observability"`
	Security        SecurityData        `json:"security"`
	Edge            EdgeData            `json:"edge"`
}

type DataProvider func(context.Context) (DataSnapshot, error)

func (h *Handler) handleData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !h.authorized(r) {
		writeUnauthorized(w)
		return
	}
	if h.dataProvider == nil {
		writeGenericError(w, http.StatusServiceUnavailable)
		return
	}
	snapshot, err := h.dataProvider(r.Context())
	if err != nil {
		writeGenericError(w, http.StatusServiceUnavailable)
		return
	}
	snapshot.SchemaVersion = 3
	hardenResponse(w, "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(snapshot)
}
