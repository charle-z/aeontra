package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestMaintainerOperationsAreDisabledByDefault(t *testing.T) {
	service, _ := newTestService(t, config.ModeAllow)

	for name, operation := range map[string]func() error{
		"edge release": func() error {
			_, err := service.SourceEdgeReleaseStatus()
			return err
		},
		"front door": func() error {
			_, err := service.PlatformFrontDoorStatus()
			return err
		},
		"coordinator": func() error {
			_, err := service.PlatformFrontDoorTransitionStatus()
			return err
		},
	} {
		err := operation()
		if err == nil || !strings.Contains(err.Error(), "maintainer operation is disabled") {
			t.Fatalf("%s error=%v", name, err)
		}
	}
}

func TestMaintainerDeploymentFailsBeforeReadingPrivatePlatformState(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()

	service, _ := newTestService(t, config.ModeAllow)
	client := NewCoolifyClient(server.URL, "token", []string{managedBackendAppUUID})
	client.do = server.Client().Do
	service.WithCoolify(client)

	if _, err := service.PlatformDeployPreview(managedBackendAppUUID); err == nil || !strings.Contains(err.Error(), "maintainer operation is disabled") {
		t.Fatalf("managed backend preview error=%v", err)
	}
	if _, err := service.PlatformDeployWithoutCachePreview(managedBackendAppUUID); err == nil || !strings.Contains(err.Error(), "force deployments are forbidden") {
		t.Fatalf("managed backend force preview error=%v", err)
	}
	if requests != 0 {
		t.Fatalf("disabled maintainer operations made %d platform requests", requests)
	}
}

func TestMaintainerProfileRequiresExactValue(t *testing.T) {
	service, _ := newTestService(t, config.ModeAllow)
	for _, profile := range []string{"", "true", "production", "charle-z"} {
		service.WithMaintainerProfile(profile)
		if service.maintainerProfileEnabled() {
			t.Fatalf("profile %q enabled maintainer authority", profile)
		}
	}
	service.WithMaintainerProfile(MaintainerProfileCharleZProduction)
	if !service.maintainerProfileEnabled() {
		t.Fatal("exact maintainer profile was not enabled")
	}
}
