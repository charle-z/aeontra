package catalog

import "encoding/json"

const resultRefPattern = `^rs_[a-f0-9]{32}$`

type ResultService interface {
	ResultRead(string, int64, int) (string, error)
	ResultFind(string, int) (string, error)
	ResultStage(string, int, int) (string, error)
}

// RegisterResults registers bounded reads over server-owned result state. Opaque
// references cannot name paths and every returned fragment is capped at 16 KiB.
func RegisterResults(register Register, service ResultService) {
	register(Tool{
		Name:        "result_read",
		Description: "Read at most 16 KiB from one redacted persisted tool result using an opaque result_ref. The ref cannot address filesystem paths; use next_offset for bounded continuation.",
		InputSchema: closedObject(map[string]any{
			"result_ref": patternedStringProp("opaque result reference", resultRefPattern, 35, 35),
			"offset":     integerProp("byte offset; defaults to 0", 0, 1<<30),
			"max_bytes":  integerProp("maximum returned bytes; defaults and caps at 16384", 1, 16<<10),
		}, "result_ref"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				ResultRef string `json:"result_ref"`
				Offset    int64  `json:"offset"`
				MaxBytes  int    `json:"max_bytes"`
			}
			if err := decodeStrict(arguments, &params); err != nil {
				return "", err
			}
			return service.ResultRead(params.ResultRef, params.Offset, params.MaxBytes)
		},
	})

	register(Tool{
		Name:        "result_find",
		Description: "Find recent unexpired redacted results by an exact bounded substring. Returns compact metadata only, never full stored output. No semantic search or embeddings.",
		InputSchema: closedObject(map[string]any{
			"query": boundedStringProp("exact substring to find", 1, 16<<10),
			"limit": integerProp("maximum metadata matches; defaults and caps at 20", 1, 20),
		}, "query"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := decodeStrict(arguments, &params); err != nil {
				return "", err
			}
			return service.ResultFind(params.Query, params.Limit)
		},
	})

	register(Tool{
		Name:        "result_stage",
		Description: "Read at most 16 KiB from one indexed stage of a redacted persisted result. Stage indexes come from compact result metadata and cannot address paths.",
		InputSchema: closedObject(map[string]any{
			"result_ref": patternedStringProp("opaque result reference", resultRefPattern, 35, 35),
			"stage":      integerProp("zero-based stage index", 0, 31),
			"max_bytes":  integerProp("maximum returned bytes; defaults and caps at 16384", 1, 16<<10),
		}, "result_ref", "stage"),
		Version: "1",
		Handler: func(arguments json.RawMessage) (string, error) {
			var params struct {
				ResultRef string `json:"result_ref"`
				Stage     int    `json:"stage"`
				MaxBytes  int    `json:"max_bytes"`
			}
			if err := decodeStrict(arguments, &params); err != nil {
				return "", err
			}
			return service.ResultStage(params.ResultRef, params.Stage, params.MaxBytes)
		},
	})
}
