package runtime

import (
	"reflect"
	"strings"
	"testing"
)

func TestRepairResultProfilesBindOriginalReportCommandAndLocation(t *testing.T) {
	sha := strings.Repeat("a", 64)
	commands := []string{"test -f internal/api/repaired.go", "test -s internal/api/repaired.go"}
	first, err := RepairResultCheckProfiles(sha, commands, "main")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := RepairResultCheckProfiles(sha, commands, "main")
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatalf("profile format drift: %v", err)
	}
	for index, profile := range first {
		if profile.CheckIndex != index || profile.Command != commands[index] || profile.Location != "main" || profile.ReusePolicy != "never" || profile.EnvironmentPolicy != "inherited" || profile.InputClosure != "whole_tree" || profile.SideEffects != "unknown" || len(profile.Environment) != 0 || profile.Environment == nil || profile.Dependencies == nil || profile.Fixtures == nil {
			t.Fatalf("profile guessed scope/reuse or lost original check: %+v", profile)
		}
	}
	variants := []struct {
		sha      string
		commands []string
		location string
	}{{strings.Repeat("b", 64), commands, "main"}, {sha, []string{commands[1], commands[0]}, "main"}, {sha, commands, "worker"}}
	for _, v := range variants {
		profiles, err := RepairResultCheckProfiles(v.sha, v.commands, v.location)
		if err != nil {
			t.Fatal(err)
		}
		if reflect.DeepEqual(first, profiles) {
			t.Fatal("profile identity ignored report/command/location change")
		}
	}
	for _, v := range []struct {
		sha      string
		commands []string
		location string
	}{{"", commands, "main"}, {sha, nil, "main"}, {sha, commands, ""}, {sha, commands, "native"}, {sha, []string{" "}, "main"}, {sha, []string{"locked:internal/api"}, "main"}, {sha, []string{commands[0], commands[0]}, "main"}} {
		if _, err := RepairResultCheckProfiles(v.sha, v.commands, v.location); err == nil {
			t.Fatalf("invalid original check profile admitted: %+v", v)
		}
	}
}
