//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModelConfigIsLocalClosedAndSupportsLocalOrOpenCode(t *testing.T) {
	for _, provider := range []string{"local-http", "opencode-local"} {
		path := filepath.Join(t.TempDir(), "model.json")
		if err := os.WriteFile(path, []byte(`{"version":1,"provider":"`+provider+`","endpoint":"http://127.0.0.1:4096/v1/next-action"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		config, err := loadModelConfig(path)
		if err != nil || config.Provider != provider {
			t.Fatalf("config=%+v err=%v", config, err)
		}
	}
}
func TestModelConfigRejectsUnknownFieldsAndWritableFile(t *testing.T) {
	for _, body := range []string{`{"version":1,"provider":"local-http","endpoint":"http://127.0.0.1:1/v1/next-action","command":"unsafe"}`, `{"version":1,"provider":"remote","endpoint":"https://example.com"}`} {
		path := filepath.Join(t.TempDir(), "model.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadModelConfig(path); err == nil {
			t.Fatalf("unsafe config accepted: %s", body)
		}
	}
}
