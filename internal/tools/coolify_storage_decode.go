package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

func decodeCoolifyStorages(body string) ([]coolifyStorage, error) {
	if entries, err := decodeCoolifyCollection[coolifyStorage](body); err == nil {
		return entries, nil
	}
	var grouped struct {
		Persistent []coolifyStorage `json:"persistent_storages"`
		Files      []coolifyStorage `json:"file_storages"`
	}
	if err := json.Unmarshal([]byte(body), &grouped); err != nil || (grouped.Persistent == nil && grouped.Files == nil) {
		return nil, fmt.Errorf("unexpected Coolify storage response")
	}
	entries := make([]coolifyStorage, 0, len(grouped.Persistent)+len(grouped.Files))
	for _, storage := range grouped.Persistent {
		storage.Type = "persistent"
		storage.Name = strings.TrimPrefix(storage.Name, p9BrainAppUUID+"-")
		entries = append(entries, storage)
	}
	for _, storage := range grouped.Files {
		storage.Type = "file"
		storage.Name = strings.TrimPrefix(storage.Name, p9BrainAppUUID+"-")
		entries = append(entries, storage)
	}
	return entries, nil
}
