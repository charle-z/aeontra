package main

import (
	"strings"
	"testing"
)

func TestValidationProfilesUseWritableEphemeralHome(t *testing.T) {
	c := config{root: "/repos", hostRoot: "/host/repos", image: "node:22-alpine", store: "store", user: "10001:10001"}
	for _, profile := range []string{"pnpm-lockfile", "pnpm-validate"} {
		args, err := c.argv("/repos/demo", profile)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(args, " ")
		for _, want := range []string{
			"HOME=/tmp/home",
			"XDG_CONFIG_HOME=/tmp/config",
			"ASTRO_TELEMETRY_DISABLED=1",
		} {
			if !strings.Contains(joined, want) {
				t.Fatalf("profile %s lacks %s: %s", profile, want, joined)
			}
		}
	}
}
