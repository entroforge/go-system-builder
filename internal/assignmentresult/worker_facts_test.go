package assignmentresult

import (
	"fmt"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"testing"
)

func workerFactRecord(t *testing.T, commands []string) assignmentrecord.Record {
	t.Helper()
	record := resultTestRecord(t)
	checks := make([]assignmentrecord.WorkerCheck, 0, len(commands))
	for i, command := range commands {
		profile := checkexec.Profile{
			ID: fmt.Sprintf("worker-profile-%d", i), Version: "1.0.0", CheckIndex: i,
			Command: command, Location: "worker", TimeoutSeconds: 30,
			EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{},
			Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{},
			InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never",
		}
		checks = append(checks, assignmentrecord.WorkerCheck{Index: i, Command: command, Profile: profile})
	}
	record.ClosingContract = &assignmentrecord.ClosingContract{Contract: S6ClosingContract, WorkerChecks: checks}
	record.IntegrationCheckMode = "post_merge"
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		t.Fatalf("test record invalid: %v", err)
	}
	return record
}
