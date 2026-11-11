package assignmentresult

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

func TestMainFactsValidateActualPathsAndChecksWithoutDigest(t *testing.T) {
	record := assignmentrecord.Record{RuntimeID: "loop-test", BaselineGeneration: 1, AssignmentID: "assignment-test", Revision: 1, EffectiveWritePaths: []string{"internal/pkg"}, RequiredChecks: []string{"go test ./internal/pkg"}}
	result := AcceptedResult{AssignmentRecord: record.Reference(), ResultRevision: 1}
	facts := MainFacts{Binding: ResultBinding{AssignmentRecord: record.Reference(), ResultPath: ".claude/evidence/result.json", ResultRevision: 1}, MainRoot: "/actual-main", TargetBranch: "development", ChangedPaths: []string{"internal/pkg/a.go"}, Checks: []MainCheckFact{{CheckIndex: 0, Command: "go test ./internal/pkg", Status: "pass"}}}
	validate := func(f MainFacts) error {
		return ValidateMainFacts(record, result, facts.Binding.ResultPath, f)
	}
	if err := validate(facts); err != nil {
		t.Fatal(err)
	}
	invalid := facts
	invalid.ChangedPaths = []string{"internal/other.go"}
	if err := validate(invalid); err == nil {
		t.Fatal("different out-of-scope actual path accepted")
	}
	invalid = facts
	invalid.Checks = nil
	if err := validate(invalid); err == nil {
		t.Fatal("missing actual Main check accepted")
	}
	invalid = facts
	invalid.Binding.ResultRevision = 2
	if err := validate(invalid); err == nil {
		t.Fatal("another accepted Result revision reused the checkpoint")
	}
	data, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"sha256", "digest", "source_head", "tested_head", "merge_commit", "merge_base"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("new facts contain %s: %s", forbidden, data)
		}
	}
}

func TestGitChangedPathsPreserveWhitespaceAndRejectAmbiguousSets(t *testing.T) {
	paths, err := CanonicalChangedPaths([]string{"internal/pkg/with\nnewline.go", "internal/pkg/with space.go"})
	if err != nil || len(paths) != 2 {
		t.Fatalf("Git paths=%v err=%v", paths, err)
	}
	for _, invalid := range [][]string{nil, {"../escape"}, {"b", "a"}, {"a", "a"}, {"/absolute"}} {
		if err := ValidateChangedPaths(invalid); err == nil {
			t.Fatalf("invalid actual path set accepted: %v", invalid)
		}
	}
}
