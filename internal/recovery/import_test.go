package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/evidence"
)

func TestImportBuildsProjectionWithoutReadingActiveState(t *testing.T) {
	root := t.TempDir()
	writeRecoveryImportFile(t, root, "docs/requirements/REQ-001.md", "# REQ-001\n\nStatus: locked\nVersion: v1.0.0\n")
	writeRecoveryImportFile(t, root, "docs/design/DESIGN-001.md", "# Design\n\nREQ: REQ-001\nStatus: locked\nVersion: v1.0.0\n")
	writeRecoveryImportFile(t, root, ".claude/loop-state.json", "\ufeff{not-json")

	designBytes, err := os.ReadFile(filepath.Join(root, "docs/design/DESIGN-001.md"))
	if err != nil {
		t.Fatal(err)
	}
	evidence := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-design-001",
		"kind":                    "planning_design",
		"runtime_id":              "loop-REQ-001",
		"baseline_generation":     1,
		"review_round":            nil,
		"producer_agent_id":       "agent-architect",
		"producer_responsibility": "architect",
		"subject_refs":            []any{map[string]any{"path": "docs/design/DESIGN-001.md", "version": "v1.0.0", "sha256": sha256RecoveryImport(designBytes)}},
		"conclusion":              "pass",
		"requested_event":         "",
		"invalidated_by":          "",
	}
	writeRecoveryImportJSON(t, root, ".claude/evidence/ev-design-001.json", evidence)

	result, err := Import(root, REQBinding{ID: "REQ-001", Path: "docs/requirements/REQ-001.md", Status: "locked", Version: "v1.0.0", SHA256: sha256RecoveryImportFile(t, root, "docs/requirements/REQ-001.md")})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.CursorInference != ImportCursorInferenceNone {
		t.Fatalf("cursor inference = %q, want %q", result.CursorInference, ImportCursorInferenceNone)
	}
	if result.TargetCursor != PlanSeedCursor {
		t.Fatalf("target cursor = %q, want conservative seed %q", result.TargetCursor, PlanSeedCursor)
	}
	if len(result.Documents) != 2 {
		t.Fatalf("trusted documents = %d, want selected REQ plus design", len(result.Documents))
	}
	if len(result.Evidence) != 1 || result.Evidence[0]["id"] != "ev-design-001" {
		t.Fatalf("trusted evidence = %#v, want ev-design-001", result.Evidence)
	}
	entities, ok := result.Entities["agents"]
	if !ok || len(entities) != 0 {
		t.Fatalf("agents = %#v, want empty inactive projection", entities)
	}
	if _, ok := result.Projection["lifecycle"]; ok {
		t.Fatal("import projection must not infer or overwrite lifecycle")
	}
}

func TestImportFindingsAreDeterministicAndLateArtifactsDoNotAdvanceCursor(t *testing.T) {
	root := t.TempDir()
	writeRecoveryImportFile(t, root, "docs/requirements/REQ-002.md", "# REQ-002\n\nStatus: locked\nVersion: v2.0.0\n")
	writeRecoveryImportFile(t, root, "docs/reports/bugs/BUG-002.md", "# BUG-002\n\nREQ: REQ-002\nStatus: accepted\nVersion: v1.0.0\n")
	writeRecoveryImportFile(t, root, "docs/reports/acceptance/ACC-002.md", "# ACC-002\n\nREQ: REQ-002\nStatus: passed\nVersion: v1.0.0\n")
	writeRecoveryImportFile(t, root, ".claude/evidence/late.json", `{"schema_version":"1.0.0","evidence_id":"ev-late","kind":"acceptance","runtime_id":"loop-REQ-002","baseline_generation":1,"producer_responsibility":"release","subject_refs":[],"conclusion":"pass"}`)

	result, err := Import(root, REQBinding{ID: "REQ-002", Path: "docs/requirements/REQ-002.md", Status: "locked", Version: "v2.0.0", SHA256: sha256RecoveryImportFile(t, root, "docs/requirements/REQ-002.md")})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetCursor != PlanSeedCursor || result.CursorInference != ImportCursorInferenceNone {
		t.Fatalf("late artifacts changed recovery cursor: target=%q inference=%q", result.TargetCursor, result.CursorInference)
	}
	if len(result.Findings) == 0 {
		t.Fatal("late evidence missing producer/schema facts should produce findings")
	}
	first, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := Import(root, REQBinding{ID: "REQ-002", Path: "docs/requirements/REQ-002.md", Status: "locked", Version: "v2.0.0", SHA256: sha256RecoveryImportFile(t, root, "docs/requirements/REQ-002.md")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(repeated)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("same input produced different result:\n%s\n%s", first, second)
	}
}

func TestImportRejectsMaliciousPathDigestDriftAndCorruptEvidence(t *testing.T) {
	root := t.TempDir()
	writeRecoveryImportFile(t, root, "docs/requirements/REQ-003.md", "# REQ-003\n\nStatus: locked\nVersion: v3.0.0\n")
	writeRecoveryImportFile(t, root, "docs/design/DESIGN-003.md", "# Design\n\nREQ: REQ-003\nStatus: locked\nVersion: v1.0.0\nSHA256: deadbeef\n")
	writeRecoveryImportFile(t, root, ".claude/evidence/bad.json", `{"schema_version":"1.0.0","evidence_id":"ev-bad","kind":"planning_design","runtime_id":"loop-REQ-003","baseline_generation":1,"producer_agent_id":"agent-architect","producer_responsibility":"architect","subject_refs":[{"path":"../../outside.md","version":"v1","sha256":"bad"}],"conclusion":"pass"}`)

	result, err := Import(root, REQBinding{ID: "REQ-003", Path: "docs/requirements/REQ-003.md", Status: "locked", Version: "v3.0.0", SHA256: sha256RecoveryImportFile(t, root, "docs/requirements/REQ-003.md")})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Evidence) != 0 {
		t.Fatalf("corrupt evidence entered trusted projection: %#v", result.Evidence)
	}
	if len(result.Findings) < 2 {
		t.Fatalf("findings = %#v, want path and digest/schema findings", result.Findings)
	}
	joined := make([]string, 0, len(result.Findings))
	for _, finding := range result.Findings {
		joined = append(joined, finding.Code+":"+finding.Path+":"+finding.Reason)
	}
	text := strings.Join(joined, "\n")
	if !strings.Contains(text, "outside") || !strings.Contains(strings.ToLower(text), "digest") {
		t.Fatalf("findings do not explain untrusted inputs: %s", text)
	}
}

func TestImportAcceptsPlanOrInventoryAndNeverRevivesAgents(t *testing.T) {
	root := t.TempDir()
	writeRecoveryImportFile(t, root, "docs/requirements/REQ-004.md", "# REQ-004\n\nStatus: locked\nVersion: v4.0.0\n")
	writeRecoveryImportFile(t, root, ".claude/workgroups/active.json", `{"agents":[{"id":"agent-live","state":"working"}]}`)

	inventory, err := Inspect(root, "docs/requirements/REQ-004.md")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(inventory)
	if err != nil {
		t.Fatal(err)
	}
	fromInventory, err := Import(root, inventory)
	if err != nil {
		t.Fatal(err)
	}
	fromPlan, err := Import(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromInventory.Projection, fromPlan.Projection) {
		t.Fatalf("inventory and plan projections differ:\n%#v\n%#v", fromInventory.Projection, fromPlan.Projection)
	}
	if len(fromPlan.Entities["agents"]) != 0 {
		t.Fatalf("active agents were revived: %#v", fromPlan.Entities["agents"])
	}
}

func TestImportEvidenceValidationTable(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantReason string
	}{
		{
			name:       "malformed JSON",
			content:    `{"schema_version":`,
			wantReason: "malformed",
		},
		{
			name:       "UTF-8 BOM",
			content:    "\ufeff{}",
			wantReason: "BOM",
		},
		{
			name:       "runtime mismatch",
			content:    `{"schema_version":"1.0.0","evidence_id":"ev-runtime","kind":"planning_design","runtime_id":"loop-REQ-other","baseline_generation":1,"producer_agent_id":"agent","producer_responsibility":"architect","subject_refs":[],"conclusion":"pass"}`,
			wantReason: "RUNTIME_MISMATCH",
		},
		{
			name:       "baseline missing",
			content:    `{"schema_version":"1.0.0","evidence_id":"ev-baseline","kind":"planning_design","runtime_id":"loop-REQ-005","baseline_generation":0,"producer_agent_id":"agent","producer_responsibility":"architect","subject_refs":[],"conclusion":"pass"}`,
			wantReason: "BASELINE_INVALID",
		},
		{
			name:       "malicious subject path",
			content:    `{"schema_version":"1.0.0","evidence_id":"ev-path","kind":"planning_design","runtime_id":"loop-REQ-005","baseline_generation":1,"producer_agent_id":"agent","producer_responsibility":"architect","subject_refs":[{"path":"../../outside","version":"v1","sha256":"bad"}],"conclusion":"pass"}`,
			wantReason: "PATH_INVALID",
		},
		{
			name:       "summary digest drift",
			content:    `{"schema_version":"1.0.0","evidence_id":"ev-digest","kind":"planning_design","runtime_id":"loop-REQ-005","baseline_generation":1,"producer_agent_id":"agent","producer_responsibility":"architect","file_sha256":"0000000000000000000000000000000000000000000000000000000000000000","subject_refs":[],"conclusion":"pass"}`,
			wantReason: "DIGEST_MISMATCH",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeRecoveryImportFile(t, root, "docs/requirements/REQ-005.md", "# REQ-005\n\nStatus: locked\nVersion: v1.0.0\n")
			writeRecoveryImportFile(t, root, ".claude/evidence/case.json", test.content)
			result, err := Import(root, REQBinding{
				ID: "REQ-005", Path: "docs/requirements/REQ-005.md", Status: "locked", Version: "v1.0.0",
				SHA256: sha256RecoveryImportFile(t, root, "docs/requirements/REQ-005.md"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Evidence) != 0 || len(result.Findings) == 0 {
				t.Fatalf("evidence=%#v findings=%#v; untrusted evidence must be rejected", result.Evidence, result.Findings)
			}
			found := false
			for _, finding := range result.Findings {
				if strings.Contains(finding.Code, test.wantReason) || strings.Contains(strings.ToLower(finding.Reason), strings.ToLower(test.wantReason)) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("findings=%#v; want reason containing %q", result.Findings, test.wantReason)
			}
		})
	}
}

func TestImportRejectsSymlinkedExternalEvidence(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeRecoveryImportFile(t, root, "docs/requirements/REQ-006.md", "# REQ-006\n\nStatus: locked\nVersion: v1.0.0\n")
	externalPath := filepath.Join(outside, "evidence.json")
	if err := os.WriteFile(externalPath, []byte(`{"schema_version":"1.0.0","evidence_id":"ev-external","kind":"planning_design","runtime_id":"loop-REQ-006","baseline_generation":1,"producer_agent_id":"agent","producer_responsibility":"architect","subject_refs":[],"conclusion":"pass"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(root, ".claude", "evidence", "escape.json")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalPath, linkPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	result, err := Import(root, REQBinding{
		ID: "REQ-006", Path: "docs/requirements/REQ-006.md", Status: "locked", Version: "v1.0.0",
		SHA256: sha256RecoveryImportFile(t, root, "docs/requirements/REQ-006.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Evidence) != 0 {
		t.Fatalf("external symlink evidence was trusted: %#v", result.Evidence)
	}
	if len(result.Findings) == 0 || !strings.Contains(strings.ToLower(result.Findings[0].Reason), "escape") && !strings.Contains(strings.ToLower(result.Findings[0].Reason), "outside") {
		t.Fatalf("findings=%#v; want repository escape finding", result.Findings)
	}
}

func TestImportAcceptsExactlyCatalogImportableKinds(t *testing.T) {
	importable := evidence.DefaultCatalog().ImportableKinds()
	if len(importable) == 0 {
		t.Fatal("catalog import allowlist must be non-empty")
	}

	for _, kind := range importable {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			writeRecoveryImportFile(t, root, "docs/requirements/REQ-007.md", "# REQ-007\n\nStatus: locked\nVersion: v1.0.0\n")
			writeRecoveryImportFile(t, root, ".claude/evidence/catalog-kind.json", `{"schema_version":"1.0.0","evidence_id":"ev-catalog-kind","kind":"`+kind+`","runtime_id":"loop-REQ-007","baseline_generation":1,"producer_agent_id":"agent","producer_responsibility":"architect","subject_refs":[],"conclusion":"pass"}`)
			result, err := Import(root, REQBinding{
				ID: "REQ-007", Path: "docs/requirements/REQ-007.md", Status: "locked", Version: "v1.0.0",
				SHA256: sha256RecoveryImportFile(t, root, "docs/requirements/REQ-007.md"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Evidence) != 1 || len(result.Findings) != 0 {
				t.Fatalf("kind %q result evidence=%#v findings=%#v; catalog importable kinds must remain trusted", kind, result.Evidence, result.Findings)
			}
		})
	}
}

func TestImportRejectsUnknownEvidenceKind(t *testing.T) {
	root := t.TempDir()
	writeRecoveryImportFile(t, root, "docs/requirements/REQ-008.md", "# REQ-008\n\nStatus: locked\nVersion: v1.0.0\n")
	writeRecoveryImportFile(t, root, ".claude/evidence/unknown.json", `{"schema_version":"1.0.0","evidence_id":"ev-unknown","kind":"unknown_kind","runtime_id":"loop-REQ-008","baseline_generation":1,"producer_agent_id":"agent","producer_responsibility":"architect","subject_refs":[],"conclusion":"pass"}`)
	result, err := Import(root, REQBinding{
		ID: "REQ-008", Path: "docs/requirements/REQ-008.md", Status: "locked", Version: "v1.0.0",
		SHA256: sha256RecoveryImportFile(t, root, "docs/requirements/REQ-008.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Evidence) != 0 {
		t.Fatalf("unknown evidence kind was trusted: %#v", result.Evidence)
	}
	if len(result.Findings) == 0 || !strings.Contains(result.Findings[0].Reason, "not registered") {
		t.Fatalf("findings=%#v; want unknown kind to fail closed", result.Findings)
	}
}

func TestImportRejectsProcessBoundEvidenceFromOldEpoch(t *testing.T) {
	for _, kind := range []string{"agent_activation", "agent_completion", "agent_readback", "team_manifest"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			writeRecoveryImportFile(t, root, "docs/requirements/REQ-010.md", "# REQ-010\n\nStatus: locked\nVersion: v1.0.0\n")
			writeRecoveryImportFile(t, root, ".claude/evidence/process-bound.json", `{"schema_version":"1.0.0","evidence_id":"ev-process-bound","kind":"`+kind+`","runtime_id":"loop-REQ-010","baseline_generation":1,"producer_agent_id":"stale-agent","producer_responsibility":"builder","subject_refs":[],"conclusion":"pass"}`)
			result, err := Import(root, REQBinding{
				ID: "REQ-010", Path: "docs/requirements/REQ-010.md", Status: "locked", Version: "v1.0.0",
				SHA256: sha256RecoveryImportFile(t, root, "docs/requirements/REQ-010.md"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Evidence) != 0 {
				t.Fatalf("old-epoch process evidence %q was trusted: %#v", kind, result.Evidence)
			}
			if len(result.Findings) == 0 || !strings.Contains(result.Findings[0].Reason, "not registered for recovery import") {
				t.Fatalf("findings=%#v; want process-bound recovery rejection", result.Findings)
			}
		})
	}
}

func TestImportRejectsInvalidEvidenceEnvelopeSemantics(t *testing.T) {
	cases := []struct {
		name  string
		field string
		value any
		want  string
	}{
		{name: "schema version", field: "schema_version", value: "9.9.9", want: "schema_version"},
		{name: "status", field: "status", value: "forged", want: "status"},
		{name: "conclusion", field: "conclusion", value: "maybe", want: "conclusion"},
		{name: "producer responsibility", field: "producer_responsibility", value: "forged-role", want: "producer_responsibility"},
		{name: "requested event", field: "requested_event", value: "forged_event", want: "requested_event"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeRecoveryImportFile(t, root, "docs/requirements/REQ-009.md", "# REQ-009\n\nStatus: locked\nVersion: v1.0.0\n")
			envelope := map[string]any{
				"schema_version":          "1.0.0",
				"evidence_id":             "ev-invalid-envelope",
				"kind":                    "planning_design",
				"runtime_id":              "loop-REQ-009",
				"baseline_generation":     1,
				"producer_agent_id":       "agent-architect",
				"producer_responsibility": "architect",
				"subject_refs":            []any{},
				"conclusion":              "pass",
				"requested_event":         "",
				"status":                  "valid",
			}
			envelope[test.field] = test.value
			writeRecoveryImportJSON(t, root, ".claude/evidence/invalid-envelope.json", envelope)

			result, err := Import(root, REQBinding{
				ID: "REQ-009", Path: "docs/requirements/REQ-009.md", Status: "locked", Version: "v1.0.0",
				SHA256: sha256RecoveryImportFile(t, root, "docs/requirements/REQ-009.md"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Evidence) != 0 {
				t.Fatalf("invalid %s entered trusted projection: %#v", test.field, result.Evidence)
			}
			found := false
			for _, finding := range result.Findings {
				if finding.Code == importFindingSchema && strings.Contains(finding.Reason, test.want) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("findings=%#v; want schema finding mentioning %q", result.Findings, test.want)
			}
		})
	}
}

func writeRecoveryImportFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRecoveryImportJSON(t *testing.T, root, relative string, value map[string]any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeRecoveryImportFile(t, root, relative, string(data))
}

func sha256RecoveryImport(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sha256RecoveryImportFile(t *testing.T, root, relative string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return sha256RecoveryImport(data)
}
