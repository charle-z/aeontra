package mcpserver

import (
	"reflect"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
)

func TestDevelopmentLocationCommandDoesNotRequireUnrelatedToolchains(t *testing.T) {
	inspection := &edge.ProjectDevelopmentInspection{Requirements: []development.CapabilityID{"toolchain.go.v1-26-9", "toolchain.pnpm.v10-13-1"}}
	got, err := developmentCommandSourceRequirements(projectDevelopmentBody{Argv: []string{"pwd"}}, inspection)
	if err != nil || len(got) != 0 {
		t.Fatalf("pwd required unrelated compiler: %v err=%v", got, err)
	}
	for _, command := range []projectDevelopmentBody{
		{Argv: []string{"./pwd"}}, {Argv: []string{"pwd", "custom"}}, {Argv: []string{"env", "pwd"}},
		{Argv: []string{"pwd"}, Environment: map[string]string{"PATH": "custom"}}, {Argv: []string{"pwd"}, RunnerProfile: "external"},
	} {
		got, err := developmentCommandSourceRequirements(command, inspection)
		if err != nil || !reflect.DeepEqual(got, inspection.Requirements) {
			t.Fatalf("unrecognized argv inference changed: %+v %v %v", command, got, err)
		}
	}
}
