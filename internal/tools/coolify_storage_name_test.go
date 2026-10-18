package tools

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestListStoragesNormalizesFixedApplicationVolumePrefix(t *testing.T) {
	client := fakeCoolify(t, "https://coolify.example.com", "tok", []string{p9BrainAppUUID}, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{
			"persistent_storages":[
				{"uuid":"storage-1","name":"jqf7qz5ensoqtvl1tb197gcv-mcp-devbox-brain","mount_path":"/brain"}
			],
			"file_storages":[]
		}`))}, nil
	})

	storages, err := client.listStorages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(storages) != 1 || storages[0].Name != p9BrainStorageName || storages[0].Type != "persistent" || storages[0].MountPath != p9BrainMountPath {
		t.Fatalf("Coolify physical volume name was not normalized to the exact logical name: %#v", storages)
	}
	present, err := classifyP9BrainStorage(storages)
	if err != nil || !present {
		t.Fatalf("normalized exact Brain storage should be idempotent success: present=%v err=%v", present, err)
	}
}
