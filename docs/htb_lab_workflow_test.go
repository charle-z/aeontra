package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestHTBLabWorkflowDocumentsOneCommandSetupAndLocalCredentialBroker(t *testing.T) {
	body, err := os.ReadFile("htb-lab-workflow.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, expected := range []string{
		"mcp-edge lab init",
		"mcp-edge lab ssh-exec",
		"private Unix-socket broker",
		"accepts no target field",
		"one-use askpass file",
		"--save-output loot/user.txt",
		"source=loot/capture-0-strings.txt",
		"shares the host network",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("HTB workflow missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		"--password ",
		"sshpass -p",
		"/var/run/docker.sock",
		"target-only packet filter is enforced",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("HTB workflow contains forbidden claim %q", forbidden)
		}
	}
}
