package debian

import (
	"os/exec"
	"testing"
)

func TestP16EdgeStateFixtureBuilds(t *testing.T) {
	output, err := exec.Command("go", "test", "./testdata/edge-state-fixture").CombinedOutput()
	if err != nil {
		t.Fatalf("edge state fixture does not compile: %v: %s", err, output)
	}
}
