package catalog

import (
	"encoding/json"

	"github.com/charle-z/mcp-devbox/internal/assets"
)

type AssetService interface {
	AssetSearch(string, int) (string, error)
	AssetMaterializePreview(string, string, string) (string, error)
	AssetMaterialize(string, bool) (string, error)
}

// RegisterAssets keeps the optional capability visible but disabled by default.
func RegisterAssets(register Register, service AssetService) {
	register(Tool{Name: "asset_search", Description: "Search the immutable operator-reviewed asset library locally. Returns bounded PNG/JPEG identities, pinned hashes, license review and attribution; no network request or legal clearance is implied.", Version: "1", InputSchema: closedObject(map[string]any{
		"query": boundedStringProp("bounded plain-text match over reviewed metadata", 1, assets.MaxQueryBytes),
		"limit": integerProp("maximum results; defaults to 5", 1, assets.MaxSearchResults),
	}, "query"), Handler: func(arguments json.RawMessage) (string, error) {
		var p struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := decodeStrict(arguments, &p); err != nil {
			return "", err
		}
		return service.AssetSearch(p.Query, p.Limit)
	}})
	register(Tool{Name: "asset_materialize_preview", Description: "Review one operator-pinned raster image and exact new relative path inside a configured backend repository. Bind a five-minute single-use plan to library, repository/parent identities, absence, MIME and expected byte hash. No download or write occurs; no Edge workspace is selected.", Version: "1", InputSchema: closedObject(map[string]any{
		"asset_id": patternedStringProp("operator-owned library entry ID", `^[a-z0-9][a-z0-9_-]{0,63}$`, 1, 64),
		"repo":     boundedStringProp("optional relative repository/workdir below the primary configured root", 1, 240),
		"path":     boundedStringProp("exact relative new PNG/JPEG path; existing parent required, no overwrite", 1, 240),
	}, "asset_id", "path"), Handler: func(arguments json.RawMessage) (string, error) {
		var p struct {
			ID   string `json:"asset_id"`
			Repo string `json:"repo"`
			Path string `json:"path"`
		}
		if err := decodeStrict(arguments, &p); err != nil {
			return "", err
		}
		return service.AssetMaterializePreview(p.ID, p.Repo, p.Path)
	}})
	register(Tool{Name: "asset_materialize", Description: "Consume and revalidate one exact asset plan, fetch only its reviewed credential-free public HTTPS source without redirects/proxy, verify bounded actual SHA-256/MIME/raster bytes, and exclusively create the new file. Ask-mode requires approve=true. Returns license/provenance receipt; failed writes may leave a new partial file and never auto-delete uncertain bytes. Linux backend only; no Edge materialization.", Version: "1", InputSchema: closedObject(map[string]any{
		"plan_id": boundedStringProp("opaque single-use ID returned by asset_materialize_preview", 1, 128),
		"approve": boolProp("execute the exact reviewed file creation when approval is required"),
	}, "plan_id"), Handler: func(arguments json.RawMessage) (string, error) {
		var p struct {
			PlanID  string `json:"plan_id"`
			Approve bool   `json:"approve"`
		}
		if err := decodeStrict(arguments, &p); err != nil {
			return "", err
		}
		return service.AssetMaterialize(p.PlanID, p.Approve)
	}})
}
