package app

import (
	"io"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/console"
)

func TestConsoleTimezoneDefaultsToBogotaAndAcceptsIANA(t *testing.T) {
	root := t.TempDir()
	for input, want := range map[string]string{
		"":               console.DefaultTimezone,
		"America/Bogota": "America/Bogota",
		"Europe/Moscow":  "Europe/Moscow",
		"UTC":            "UTC",
	} {
		t.Run(want, func(t *testing.T) {
			t.Setenv(consoleTimezoneEnv, input)
			opts, err := parseServeOptions([]string{"--root", root}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if opts.ConsoleTimezone != want {
				t.Fatalf("timezone=%q want=%q", opts.ConsoleTimezone, want)
			}
		})
	}
}

func TestConsoleTimezoneInvalidConfigurationBlocksStartup(t *testing.T) {
	t.Setenv(consoleTimezoneEnv, "COT")
	_, err := parseServeOptions([]string{"--root", t.TempDir()}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), consoleTimezoneEnv) || !strings.Contains(err.Error(), "IANA") {
		t.Fatalf("error=%v", err)
	}
}
