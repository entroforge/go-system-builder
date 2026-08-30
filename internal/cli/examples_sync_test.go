package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestS7S9RequestExamplesTrackRequestContracts pins each domain request
// example listed below under docs/examples/s7-s9/ to the CLI request contract (the Go struct json
// tags the verb decodes). The examples teach agents the copyable `--file`
// shape; when a request struct is renamed the example must follow, or the
// first real submission rejects a shape the docs promised. Round-13 review
// finding N1: the examples were briefly judged against the *artifact*
// schemas and failed — the right invariant is against the *request* tags,
// which is what this test encodes (persisted records use different names by
// design; see docs/examples/s7-s9/README.md).
func TestS7S9RequestExamplesTrackRequestContracts(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "examples", "s7-s9")

	type field struct {
		path  string // top-level key
		want  string // non-empty expectation for scalar strings; "" = presence only
		isRef bool   // must be an object with path+sha256
	}
	cases := map[string][]field{
		// repair.PlanReportRequest json tags
		"repair-plan-report.json": {
			{path: "session_ref", isRef: true}, {path: "plan_ref", isRef: true},
			{path: "assignment_id", want: ""}, {path: "agent_id", want: ""},
			{path: "report_id", want: "repair-plan-report-"}, {path: "plan", want: ""},
			{path: "red_checks"}, {path: "proposed_paths"},
		},
		// repair.RepairResultRequest json tags
		"repair-result.json": {
			{path: "contract", isRef: true}, {path: "session", isRef: true}, {path: "plan", isRef: true},
			{path: "result_id", want: ""}, {path: "producer_agent_id", want: ""},
			{path: "unit_results"}, {path: "changed_artifacts"}, {path: "result", want: "pass"},
			{path: "assignment_id", want: ""}, {path: "plan_report", isRef: true},
			{path: "before_fix_checks"}, {path: "checks"},
		},
		// repair.ChangeImpactRequest json tags
		"change-impact.json": {
			{path: "impact_id", want: "impact-"}, {path: "runtime_id", want: ""},
			{path: "req_id", want: "REQ-"}, {path: "baseline_generation"},
			{path: "source_bug_ids"}, {path: "change_types"}, {path: "changed_artifacts"},
			{path: "decisions"}, {path: "escalation_level"}, {path: "invalidated_evidence_ids"},
			{path: "retained_evidence_ids"}, {path: "analyzed_by", want: ""},
		},
		// repair.TargetedReverificationRequest json tags
		"targeted-reverification.json": {
			{path: "reverification_id", want: "reverify-"}, {path: "runtime_id", want: ""},
			{path: "baseline_generation"}, {path: "original_assignment_id", want: ""},
			{path: "performing_assignment_id", want: ""}, {path: "continuity_reason", want: ""},
			{path: "impact_id", want: ""}, {path: "assertion_results"},
			{path: "scope_compliance", want: "pass"}, {path: "result", want: "pass"},
		},
		// repair.HandoffRequest json tags
		"repair-handoff.json": {
			{path: "handoff_id", want: "repair-handoff-"},
			{path: "session", isRef: true}, {path: "plan", isRef: true}, {path: "contract", isRef: true},
			{path: "result", isRef: true}, {path: "changeset", isRef: true}, {path: "change_impact", isRef: true},
			{path: "targeted_reverifications"}, {path: "handed_off_by", want: ""}, {path: "next_action", want: ""},
		},
	}
	for file, fields := range cases {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		var document map[string]json.RawMessage
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("%s: decode: %v", file, err)
		}
		for _, f := range fields {
			raw, ok := document[f.path]
			if !ok {
				t.Errorf("%s: request example lost the %q key — rename the struct tag or the example together", file, f.path)
				continue
			}
			if f.isRef {
				var ref struct {
					Path   string `json:"path"`
					SHA256 string `json:"sha256"`
				}
				if err := json.Unmarshal(raw, &ref); err != nil || ref.Path == "" || ref.SHA256 == "" {
					t.Errorf("%s: %q must be an object with path+sha256, got %s", file, f.path, string(raw))
				}
				continue
			}
			if f.want == "" {
				continue // presence only
			}
			var value string
			if err := json.Unmarshal(raw, &value); err != nil || !strings.HasPrefix(value, f.want) {
				t.Errorf("%s: %q should carry the %q prefix, got %s", file, f.path, f.want, string(raw))
			}
		}
	}
}
