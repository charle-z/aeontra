package console

type ActivityWindowData struct {
	Requests               int64  `json:"requests"`
	ToolCalls              int64  `json:"tool_calls"`
	InputBytes             int64  `json:"input_bytes"`
	OutputBytes            int64  `json:"output_bytes"`
	EstimatedPayloadTokens int64  `json:"estimated_payload_tokens"`
	ClientErrors           int64  `json:"client_errors"`
	ServerErrors           int64  `json:"server_errors"`
	ExternalWaitMS         int64  `json:"external_wait_ms"`
	UpdatedAt              string `json:"updated_at"`
}

type DurableActivityData struct {
	Last24Hours ActivityWindowData `json:"last_24_hours"`
	Last7Days   ActivityWindowData `json:"last_7_days"`
	Last30Days  ActivityWindowData `json:"last_30_days"`
	Last90Days  ActivityWindowData `json:"last_90_days"`
	Lifetime    ActivityWindowData `json:"lifetime"`
}
