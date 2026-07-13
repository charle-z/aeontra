package policy

import (
	"errors"
	"testing"
)

func TestCheckCommandRejectsPathQualifiedAllowlistedPrograms(t *testing.T) {
	allowed := []string{"git", "go", "git.exe"}
	for _, program := range []string{
		"./git",
		"../git",
		"/usr/bin/git",
		`C:\tools\git.exe`,
		`..\git.exe`,
		"C:git.exe",
		" git",
		"git ",
	} {
		if err := CheckCommand(allowed, program, []string{"status"}); !errors.Is(err, ErrCommandPathQualified) {
			t.Errorf("CheckCommand(%q) = %v, want ErrCommandPathQualified", program, err)
		}
	}
}

func TestCheckCommandStillAllowsBareExecutableNames(t *testing.T) {
	allowed := []string{"git", "go"}
	for _, program := range []string{"git", "GIT", "git.exe", "go"} {
		if err := CheckCommand(allowed, program, []string{"status"}); err != nil {
			t.Errorf("CheckCommand(%q) = %v, want nil", program, err)
		}
	}
}
