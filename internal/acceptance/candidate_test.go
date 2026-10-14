package acceptance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type candidateFiles map[string][]byte

func (f candidateFiles) ReadFile(path string) ([]byte, error) {
	data, ok := f[path]
	if !ok {
		return nil, fmt.Errorf("fixture file %q missing", path)
	}
	return data, nil
}
func candidateSHA(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func completeCandidateFixture(t *testing.T) CandidateInput {
	t.Helper()
	files := candidateFiles{"req.md": []byte("Required behavior"), "contract.md": []byte("Required contract"), "plan.json": []byte(`{"review_round":1,"baseline_generation":1,"claims":[{"claim_id":"claim-1"}]}`), "support.json": []byte("independent observation")}
	var manifest map[string]any
	if err := json.Unmarshal(validAcceptanceManifest(t), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["coverage_inventory"] = append(manifest["coverage_inventory"].([]any), map[string]any{"id": "claim-1", "category": "claim", "source_refs": []string{"plan.json"}, "expected": "required claim", "oracle": "distinguishing check", "owner": "Acceptance", "evidence_refs": []string{"ev-check"}, "disposition": "pass"})
	manifest["counterevidence"] = append(manifest["counterevidence"].([]any), map[string]any{"id": "CE-claim", "inventory_id": "claim-1", "question": "can the claim be disproved?", "evidence_refs": []string{"ev-check"}, "outcome": "pass"})
	files["manifest.json"], _ = json.Marshal(manifest)
	state := map[string]any{"runtime_id": "loop-1", "revision": 7, "baseline": map[string]any{"generation": 1}, "review": map[string]any{"round": 1, "plan": map[string]any{"path": "plan.json", "sha256": candidateSHA(files["plan.json"])}}, "bound_req": map[string]any{"id": "REQ-1", "path": "req.md", "sha256": candidateSHA(files["req.md"])}, "documents": []any{map[string]any{"id": "CONTRACT-1", "kind": "contract", "generation": 1, "path": "contract.md", "version": "v1", "sha256": candidateSHA(files["contract.md"])}}}
	var rows []any
	for _, id := range []string{"ev-audit", "ev-check"} {
		rows = append(rows, map[string]any{"id": id, "kind": "clean_round", "path": "support.json", "sha256": candidateSHA(files["support.json"]), "status": "valid", "baseline_generation": 1, "review_round": 1})
	}
	state["evidence"] = rows
	input := CandidateInput{State: state, Files: files, Kind: "acceptance"}
	appendCandidate(t, input, "A", nil)
	return input
}

func appendCandidate(t *testing.T, input CandidateInput, id string, edit func(map[string]any)) {
	t.Helper()
	files := input.Files.(candidateFiles)
	e := map[string]any{"schema_version": "1.0.0", "evidence_id": id, "kind": "acceptance", "runtime_id": "loop-1", "baseline_generation": 1, "review_round": 1, "producer_agent_id": "auditor", "producer_responsibility": "Acceptance", "subject_refs": []any{}, "conclusion": "pass", "audit_manifest_path": "manifest.json", "audit_manifest_sha256": candidateSHA(files["manifest.json"])}
	if edit != nil {
		edit(e)
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	files[id+".json"] = data
	input.State["evidence"] = append(input.State["evidence"].([]any), map[string]any{"id": id, "kind": "acceptance", "path": id + ".json", "sha256": candidateSHA(data), "status": "valid", "baseline_generation": 1, "review_round": 1, "produced_by": []any{"auditor"}, "responsibility_id": "Acceptance"})
}

func TestCandidateSelectionNeverSubstitutesTheRequestedID(t *testing.T) {
	for _, aGood := range []bool{false, true} {
		for _, bGood := range []bool{false, true} {
			t.Run(fmt.Sprintf("A=%t/B=%t", aGood, bGood), func(t *testing.T) {
				input := completeCandidateFixture(t)
				if !aGood {
					input.Files.(candidateFiles)["A.json"] = []byte("drifted")
				}
				appendCandidate(t, input, "B", func(e map[string]any) {
					if !bGood {
						e["audit_manifest_sha256"] = strings.Repeat("0", 64)
					}
				})
				input.EvidenceID = "A"
				if _, err := ValidateS10Candidate(input); err == nil || !strings.Contains(err.Error(), "selection conflict") {
					t.Fatalf("old A must be rejected explicitly, got %v", err)
				}
				input.EvidenceID = "B"
				got, err := ValidateS10Candidate(input)
				if (err == nil) != bGood {
					t.Fatalf("selected B validity=%t, got %+v / %v", bGood, got, err)
				}
				if got.EvidenceID != "B" {
					t.Fatalf("consumed another ID: %+v", got)
				}
			})
		}
	}
}

func TestCandidateRejectsMissingAuthorityAndInvalidEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"id", func(e map[string]any) { e["evidence_id"] = "other" }},
		{"kind", func(e map[string]any) { e["kind"] = "release_audit" }},
		{"outcome", func(e map[string]any) { e["conclusion"] = "approved" }},
		{"producer", func(e map[string]any) { e["producer_agent_id"] = "builder" }},
		{"role", func(e map[string]any) { e["producer_responsibility"] = "Builder" }},
		{"runtime", func(e map[string]any) { e["runtime_id"] = "other" }},
		{"generation", func(e map[string]any) { e["baseline_generation"] = 2 }},
		{"round", func(e map[string]any) { e["review_round"] = 2 }},
		{"string_subject", func(e map[string]any) { e["subject_refs"] = []string{"contract.md"} }},
		{"unregistered_subject", func(e map[string]any) { e["subject_refs"] = []Subject{{Path: "manifest.json", SHA256: "fake"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := completeCandidateFixture(t)
			appendCandidate(t, input, "B", tc.edit)
			if _, err := ValidateS10Candidate(input); err == nil {
				t.Fatal("invalid current evidence accepted")
			}
		})
	}
	input := completeCandidateFixture(t)
	delete(input.State, "bound_req")
	if _, err := ValidateS10Candidate(input); err == nil {
		t.Fatal("unbound production candidate accepted")
	}
}

func TestCandidateReadsAllDependenciesFromDeclaredView(t *testing.T) {
	input := completeCandidateFixture(t)
	got, err := ValidateS10Candidate(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"A.json", "manifest.json", "req.md", "contract.md", "plan.json", "support.json"} {
		if got.Consumed[path] == "" {
			t.Fatalf("unobserved dependency %s", path)
		}
	}
	input.Files.(candidateFiles)["req.md"] = []byte("changed in declared view")
	if _, err := ValidateS10Candidate(input); err == nil {
		t.Fatal("source drift accepted")
	}
}

func TestCandidateAllowsRegisteredSubjectAndAuthoritativeHandoffReference(t *testing.T) {
	input := completeCandidateFixture(t)
	rows := input.State["evidence"].([]any)
	rows[0].(map[string]any)["kind"] = "repair_handoff"
	appendCandidate(t, input, "B", func(e map[string]any) {
		e["subject_refs"] = []Subject{{Path: "contract.md", Version: "v1", SHA256: candidateSHA(input.Files.(candidateFiles)["contract.md"])}}
	})
	if _, err := ValidateS10Candidate(input); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentCandidateInvalidationDoesNotResurrectOlderPass(t *testing.T) {
	input := completeCandidateFixture(t)
	appendCandidate(t, input, "B", nil)
	rows := input.State["evidence"].([]any)
	current := rows[len(rows)-1].(map[string]any)
	current["status"] = "invalid"
	current["invalidated_by"] = "new-observation"
	got, err := ValidateS10Candidate(input)
	if err == nil || got.EvidenceID != "B" {
		t.Fatalf("older PASS substituted for invalid current candidate: %+v / %v", got, err)
	}
}
