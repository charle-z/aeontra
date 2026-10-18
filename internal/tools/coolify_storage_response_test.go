package tools

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestListStoragesNormalizesCoolifyV4GroupedResponse(t *testing.T) {
	client := fakeCoolify(t, "https://coolify.example.com", "tok", []string{p9BrainAppUUID}, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/applications/"+p9BrainAppUUID+"/storages" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{
			"persistent_storages":[
				{"uuid":"storage-1","name":"mcp-devbox-brain","mount_path":"/brain"}
			],
			"file_storages":[
				{"uuid":"file-1","name":"config","mount_path":"/config"}
			]
		}`))}, nil
	})

	storages, err := client.listStorages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(storages) != 2 {
		t.Fatalf("storages=%#v", storages)
	}
	if storages[0].Type != "persistent" || storages[0].Name != p9BrainStorageName || storages[0].MountPath != p9BrainMountPath {
		t.Fatalf("persistent storage was not normalized: %#v", storages[0])
	}
	if storages[1].Type != "file" || storages[1].Name != "config" || storages[1].MountPath != "/config" {
		t.Fatalf("file storage was not normalized: %#v", storages[1])
	}
	present, err := classifyP9BrainStorage(storages)
	if err != nil || !present {
		t.Fatalf("exact persistent Brain storage should be accepted: present=%v err=%v", present, err)
	}
}
