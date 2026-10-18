package console

import "testing"

func TestValidateTimezoneDefaultsAndAcceptsExplicitIANA(t *testing.T) {
	for input, want := range map[string]string{
		"":                               DefaultTimezone,
		"America/Bogota":                 "America/Bogota",
		"America/Argentina/Buenos_Aires": "America/Argentina/Buenos_Aires",
		"Europe/Moscow":                  "Europe/Moscow",
		"UTC":                            "UTC",
	} {
		got, err := ValidateTimezone(input)
		if err != nil || got != want {
			t.Fatalf("ValidateTimezone(%q)=%q err=%v want=%q", input, got, err, want)
		}
	}
}

func TestValidateTimezoneRejectsAmbiguousInvalidAndOversizedValues(t *testing.T) {
	for _, input := range []string{
		"COT",
		"EST",
		"CST",
		"GMT-5",
		"America/Not_A_Real_Zone",
		"America/../Bogota",
		"America/" + string(make([]byte, maxTimezoneBytes)),
	} {
		if _, err := ValidateTimezone(input); err == nil {
			t.Fatalf("ValidateTimezone(%q) succeeded", input)
		}
	}
}
