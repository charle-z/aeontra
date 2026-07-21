package devaction

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefinitionsExposeOnlyClosedCredentialFreeGitActions(t *testing.T) {
	definitions := Definitions()
	if len(definitions) != 3 {
		t.Fatalf("definitions=%d", len(definitions))
	}
	encoded, err := json.Marshal(definitions)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(encoded))
	for _, forbidden := range []string{`"token"`, `"password"`, `"url"`, `"force"`, `"refspec"`, `"command"`} {
		if strings.Contains(lower, forbidden+":") {
			t.Fatalf("definitions expose %s: %s", forbidden, encoded)
		}
	}
	for _, definition := range definitions {
		if definition.InputSchema["additionalProperties"] != false {
			t.Fatalf("schema is open: %s", definition.Name)
		}
	}
}
