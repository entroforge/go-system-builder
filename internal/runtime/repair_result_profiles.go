package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// RepairResultCheckProfiles formats conservative profiles for the original S9
// prepare consumer. The caller must prove that commands and reportSHA belong
// to the current accepted PlanReport before freezing them in the original CAS.
// This function neither grants execution authority nor records a check result.
func RepairResultCheckProfiles(reportSHA string, commands []string, location string) ([]checkexec.Profile, error) {
	if len(reportSHA) != 64 || strings.Trim(reportSHA, "0123456789abcdef") != "" {
		return nil, fmt.Errorf("S9 check profiles need the exact accepted PlanReport SHA")
	}
	if location != "main" && location != "worker" {
		return nil, fmt.Errorf("S9 prepare must select the original main or worker check location")
	}
	profiles := make([]checkexec.Profile, 0, len(commands))
	seenCommands := map[string]bool{}
	for index, command := range commands {
		if strings.TrimSpace(command) == "" {
			return nil, fmt.Errorf("S9 accepted PlanReport contains an empty check command")
		}
		if seenCommands[command] {
			return nil, fmt.Errorf("S9 profile preparation cannot distinguish duplicate command slots; recover the original PlanReport by name")
		}
		seenCommands[command] = true
		if strings.HasPrefix(strings.TrimSpace(command), "locked:") {
			return nil, fmt.Errorf("S9 red-check command cannot become lock metadata")
		}
		identity, err := json.Marshal(struct {
			Contract, ReportSHA256, Command, Location string
			Index                                     int
		}{"s9-accepted-report-check-profile-v1", reportSHA, command, location, index})
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, checkexec.Profile{
			ID: "s9-report-check-" + workpacket.Digest(identity), Version: "v1",
			CheckIndex: index, Command: command, Location: location, TimeoutSeconds: 300,
			EnvironmentPolicy: "inherited", Environment: []checkexec.EnvFact{},
			Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{},
			InputClosure: "whole_tree", SideEffects: "unknown", ReusePolicy: "never",
		})
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("S9 prepare needs non-empty original PlanReport checks")
	}
	if err := checkexec.ValidateProfiles(profiles, commands, location); err != nil {
		return nil, err
	}
	return profiles, nil
}
