package app

import (
	"os"
	"testing"
)

func TestApplicationOrchestrationIsSplitByConcern(t *testing.T) {
	for _, path := range []string{"run.go", "env.go", "oauth.go", "serve.go", "grant.go"} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected application module %s: %v", path, err)
		}
	}
	if _, err := os.Stat("app.go"); !os.IsNotExist(err) {
		t.Error("app.go monolith must not remain after the application split")
	}
}
