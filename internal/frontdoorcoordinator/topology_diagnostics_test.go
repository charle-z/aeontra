package frontdoorcoordinator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTopologyReturnsOnlySanitizedStageErrors(t *testing.T) {
	t.Parallel()
	stage := ""
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/applications/front1":
			if stage == "front" {
				response.WriteHeader(http.StatusBadGateway)
				_, _ = response.Write([]byte("raw front secret"))
				return
			}
			branch := "front-door-stable"
			if stage == "identity" {
				branch = "wrong"
			}
			_, _ = response.Write([]byte(`{"uuid":"front1","git_repository":"charle-z/mcp-devbox","git_branch":"` + branch + `","fqdn":"` + FrontTemporaryOrigin + `"}`))
		case "/api/v1/applications/backend1":
			if stage == "backend" {
				response.WriteHeader(http.StatusBadGateway)
				_, _ = response.Write([]byte("raw backend secret"))
				return
			}
			_, _ = response.Write([]byte(`{"uuid":"backend1","git_repository":"charle-z/mcp-devbox","git_branch":"main","fqdn":"` + FrontPublicOrigin + `"}`))
		case "/api/v1/applications/front1/envs":
			if stage == "environment" {
				response.WriteHeader(http.StatusBadGateway)
				_, _ = response.Write([]byte("raw environment secret"))
				return
			}
			_, _ = response.Write([]byte(`[` + managedEnvironmentEntryJSON("token", "MCP_FRONT_DOOR_BACKEND_URL", FrontPublicOrigin) + `]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client, err := NewClient(validClientConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	client.http = server.Client()
	cases := []struct {
		stage string
		want  error
	}{
		{stage: "front", want: ErrTopologyFrontApplication},
		{stage: "backend", want: ErrTopologyBackendApplication},
		{stage: "identity", want: ErrTopologyManagedIdentity},
		{stage: "environment", want: ErrTopologyFrontBackend},
	}
	for _, testCase := range cases {
		stage = testCase.stage
		_, err := client.Topology(context.Background())
		if !errors.Is(err, testCase.want) {
			t.Fatalf("stage=%s error=%v want=%v", testCase.stage, err, testCase.want)
		}
		if err.Error() != testCase.want.Error() {
			t.Fatalf("stage=%s leaked wrapped detail: %q", testCase.stage, err.Error())
		}
	}
}
