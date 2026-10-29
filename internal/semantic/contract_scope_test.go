package semantic_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func scopePut(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func scopeIndex(req, contract string) string {
	return "# Contracts\n> 需求: " + req + "\n> Shared model policy: none\n> Shared model reason: This fixture has no shared wire representation.\n\n## 合同清单\n| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |\n|:---|:---|:---|:---|:---|:---|\n| " + contract + " | 后端 | Main | locked | v1.0.0 | N/A |\n"
}

func contractScopeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	scopePut(t, root, "docs/dev/contracts/CONTRACTS-001.md", scopeIndex("REQ-001", "BE-001"))
	scopePut(t, root, "docs/dev/contracts/CONTRACTS-002.md", scopeIndex("REQ-002", "BE-002"))
	scopePut(t, root, "docs/dev/contracts/BE-001.md", "# Current\n| 本合同条款 | 行为 |\n|---|---|\n| §1 | CASE-CURRENT FR-001 |\n")
	scopePut(t, root, "docs/dev/contracts/BE-002.md", "# History\n| 本合同条款 | 行为 |\n|---|---|\n| §1 | CASE-HISTORY FR-002 |\n")
	scopePut(t, root, "docs/requirements/REQ-001.md", "# REQ-001\n## §C 当前需求\n| ID | 行为 | 指向 |\n|---|---|---|\n| FR-001 | Current | S-001 |\n")
	scopePut(t, root, "docs/requirements/REQ-002.md", "# REQ-002\n## §C 当前需求\n| ID | 行为 | 指向 |\n|---|---|---|\n| FR-002 | Historical | S-001 |\n")
	scopePut(t, root, "docs/design/prototypes/service/scenario-model.json", `{"module":"service","rules":[{"id":"rule-current","source_refs":["REQ-001/FR-001"],"branches":[{"case_id":"CASE-CURRENT"}]},{"id":"rule-history","source_refs":["REQ-002/FR-002"],"branches":[{"case_id":"CASE-HISTORY"}]}]}`)
	scopePut(t, root, "docs/design/prototypes/service/cases.json", `{"module":"service","cases":[{"id":"CASE-CURRENT"},{"id":"CASE-HISTORY"}]}`)
	return root
}

func scopedCheck(t *testing.T, root string) string {
	t.Helper()
	result, err := semantic.ContractsCheckWithFiles(root, fileview.Disk{Root: root}, "REQ-001")
	if err != nil {
		return err.Error()
	}
	return strings.Join(result.Problems, "\n")
}

func TestScopedContractsCannotBorrowForeignCASECoverage(t *testing.T) {
	root := contractScopeFixture(t)
	scopePut(t, root, "docs/dev/contracts/BE-001.md", "# Current\nFR-001\n")
	scopePut(t, root, "docs/dev/contracts/BE-002.md", "# History\nCASE-CURRENT CASE-HISTORY FR-002\n")
	if problems := scopedCheck(t, root); !strings.Contains(problems, "reverse closure: CASE-CURRENT") {
		t.Fatalf("foreign contract filled the current batch: %s", problems)
	}
}

func TestScopedContractsDoNotDemandHistoryOrReadForeignBodies(t *testing.T) {
	root := contractScopeFixture(t)
	scopePut(t, root, "docs/dev/contracts/BE-002.md", "# Historical diagnostic problem\nCASE-UNKNOWN FR-404\n")
	result, err := semantic.ContractsCheckWithFiles(root, fileview.Disk{Root: root}, "REQ-001")
	if err != nil || len(result.Problems) > 0 || result.Contracts != 2 {
		t.Fatalf("current batch leaked unrelated contract/history: %+v %v", result, err)
	}
}

func TestScopedBareFRCannotBorrowAnotherREQTable(t *testing.T) {
	root := contractScopeFixture(t)
	scopePut(t, root, "docs/dev/contracts/BE-001.md", "# Current\nCASE-CURRENT FR-999\n")
	scopePut(t, root, "docs/requirements/REQ-002.md", "# REQ-002\n| FR-002 | Historical | S-001 |\n| FR-999 | Only foreign | S-001 |\n")
	if problems := scopedCheck(t, root); !strings.Contains(problems, "FR-999") || !strings.Contains(problems, "REQ-001") {
		t.Fatalf("bare FR borrowed a foreign table: %s", problems)
	}
}

func TestQualifiedFRUsesExactREQSource(t *testing.T) {
	for _, tc := range []struct{ name, refs, want string }{
		{"real historical ref", "REQ-002/FR-002", ""},
		{"wrong owner same number", "REQ-002/FR-001", "REQ-002/FR-001"},
		{"bare FR remains current", "REQ-002/FR-002 FR-002", "REQ-001"},
		{"neighbor name", "REQ-0010/FR-001", "REQ-0010"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := contractScopeFixture(t)
			scopePut(t, root, "docs/dev/contracts/BE-001.md", "# Current\nCASE-CURRENT "+tc.refs+"\n")
			problems := scopedCheck(t, root)
			if tc.want == "" && problems != "" || tc.want != "" && !strings.Contains(problems, tc.want) {
				t.Fatalf("qualified scope %q: %s", tc.refs, problems)
			}
		})
	}
}

func TestScopedContractsUseBoundPathAndCurrentRequirementSection(t *testing.T) {
	root := contractScopeFixture(t)
	actual := "docs/requirements/current-goal.md"
	if err := os.Rename(filepath.Join(root, "docs/requirements/REQ-001.md"), filepath.Join(root, actual)); err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(map[string]any{"lifecycle": map[string]any{"state": "planning", "phase": "contracts"}, "bound_req": map[string]any{"id": "REQ-001", "path": actual}})
	scopePut(t, root, ".claude/loop-state.json", string(state))
	if problems := scopedCheck(t, root); problems != "" {
		t.Fatalf("original bound path ignored: %s", problems)
	}
	scopePut(t, root, actual, "# REQ-001\n## §C 当前需求\n| FR-001 | Current | S-001 |\n## §F History\n| FR-999 | Old | S-001 |\n")
	scopePut(t, root, "docs/dev/contracts/BE-001.md", "# Current\nCASE-CURRENT FR-999\n")
	if problems := scopedCheck(t, root); !strings.Contains(problems, "FR-999") {
		t.Fatalf("historical section satisfied current FR: %s", problems)
	}
}

func TestScopedReverseClosureDoesNotTakeNeighborREQCases(t *testing.T) {
	root := contractScopeFixture(t)
	scopePut(t, root, "docs/design/prototypes/service/scenario-model.json", `{"rules":[{"source_refs":["REQ-001/FR-001"],"branches":[{"case_id":"CASE-CURRENT"}]},{"source_refs":["REQ-0010/FR-001"],"branches":[{"case_id":"CASE-HISTORY"}]}]}`)
	if problems := scopedCheck(t, root); problems != "" {
		t.Fatalf("neighbor scope entered current obligations: %s", problems)
	}
}

func TestScopedContractsStillRejectUnknownCASEAndUnverifiableSources(t *testing.T) {
	for _, fault := range []string{"unknown_case", "missing_rule_source", "duplicate_current_index"} {
		t.Run(fault, func(t *testing.T) {
			root := contractScopeFixture(t)
			switch fault {
			case "unknown_case":
				scopePut(t, root, "docs/dev/contracts/BE-001.md", "CASE-CURRENT CASE-UNKNOWN FR-001")
			case "missing_rule_source":
				scopePut(t, root, "docs/design/prototypes/service/scenario-model.json", `{"rules":[{"branches":[{"case_id":"CASE-CURRENT"},{"case_id":"CASE-HISTORY"}]}]}`)
			case "duplicate_current_index":
				scopePut(t, root, "docs/dev/contracts/CONTRACTS-OTHER.md", scopeIndex("REQ-001", "BE-001"))
			}
			if problems := scopedCheck(t, root); problems == "" {
				t.Fatalf("%s was silently accepted", fault)
			}
		})
	}
}

func TestScopedCASEOwnershipIncludesOtherReferencesOfExactREQ(t *testing.T) {
	root := contractScopeFixture(t)
	scopePut(t, root, "docs/design/prototypes/service/scenario-model.json", `{"rules":[{"source_refs":["REQ-001/NFR-001"],"branches":[{"case_id":"CASE-CURRENT"}]},{"source_refs":["REQ-002/FR-002"],"branches":[{"case_id":"CASE-HISTORY"}]}]}`)
	scopePut(t, root, "docs/dev/contracts/BE-001.md", "FR-001")
	scopePut(t, root, "docs/dev/contracts/BE-002.md", "CASE-CURRENT CASE-HISTORY FR-002")
	if problems := scopedCheck(t, root); !strings.Contains(problems, "reverse closure: CASE-CURRENT") {
		t.Fatalf("current NFR-owned CASE lost its batch obligation: %s", problems)
	}
}

func TestUnboundContractDiagnosticStatesItsScope(t *testing.T) {
	root := contractScopeFixture(t)
	result, err := semantic.ContractsCheck(root)
	if err != nil || len(result.Problems) > 0 || !strings.Contains(strings.Join(result.Warnings, "\n"), "all-project") {
		t.Fatalf("unbound diagnostic pretended to certify a current batch: %+v %v", result, err)
	}
	scopePut(t, root, ".claude/loop-state.json", `{"lifecycle":{"state":"inactive"},"bound_req":null}`)
	if result, err := semantic.ContractsCheck(root); err != nil || !strings.Contains(strings.Join(result.Warnings, "\n"), "all-project") {
		t.Fatalf("inactive Runtime demanded a binding: %+v %v", result, err)
	}
}

func TestQualifiedHistoricalFRReadsActualDescriptiveSourceWithoutAliases(t *testing.T) {
	root := contractScopeFixture(t)
	old := filepath.Join(root, "docs/requirements/REQ-002.md")
	if err := os.Rename(old, filepath.Join(root, "docs/requirements/REQ-002-historical-service.md")); err != nil {
		t.Fatal(err)
	}
	scopePut(t, root, "docs/dev/contracts/BE-001.md", "CASE-CURRENT REQ-002/FR-002")
	if problems := scopedCheck(t, root); problems != "" {
		t.Fatalf("legitimate historical source demanded an alias/copy: %s", problems)
	}
	scopePut(t, root, "docs/requirements/REQ-002-other.md", "# REQ-OTHER\n| FR-002 | Same-numbered unrelated FR | S-001 |\n")
	if problems := scopedCheck(t, root); problems != "" {
		t.Fatalf("filename prefix overrode real source identity: %s", problems)
	}
	scopePut(t, root, "docs/requirements/REQ-002-ambiguous.md", "# REQ-002\n| FR-002 | Conflicting original source | S-001 |\n")
	if problems := scopedCheck(t, root); !strings.Contains(problems, "ambiguous") {
		t.Fatalf("multiple real sources were guessed: %s", problems)
	}
}

func TestQualifiedHistoricalFRRejectsContradictorySourceIdentity(t *testing.T) {
	root := contractScopeFixture(t)
	scopePut(t, root, "docs/dev/contracts/BE-001.md", "CASE-CURRENT REQ-002/FR-002")
	scopePut(t, root, "docs/requirements/REQ-002.md", "# REQ-OTHER\n| FR-002 | Different owner | S-001 |\n")
	if problems := scopedCheck(t, root); !strings.Contains(problems, "another REQ identity") {
		t.Fatalf("filename overrode contradictory original identity: %s", problems)
	}
}

func TestPublicContractCheckSelectsBoundBatchAndDoesNotFallbackOnMissingScope(t *testing.T) {
	root := contractScopeFixture(t)
	state := map[string]any{"lifecycle": map[string]any{"state": "planning", "phase": "contracts"}, "bound_req": map[string]any{"id": "REQ-001", "path": "docs/requirements/REQ-001.md"}}
	data, _ := json.Marshal(state)
	scopePut(t, root, ".claude/loop-state.json", string(data))
	scopePut(t, root, "docs/dev/contracts/BE-002.md", "CASE-UNKNOWN FR-404")
	result, err := semantic.ContractsCheck(root)
	if err != nil || len(result.Problems) > 0 || result.Contracts != 2 {
		t.Fatalf("public current check leaked foreign scope: %+v %v", result, err)
	}
	delete(state, "bound_req")
	data, _ = json.Marshal(state)
	scopePut(t, root, ".claude/loop-state.json", string(data))
	if _, err := semantic.ContractsCheck(root); err == nil || !strings.Contains(err.Error(), "original bound REQ") {
		t.Fatalf("missing original scope fell back to global proof: %v", err)
	}
}

func TestScopedCheckCannotInventBatchFromUndeclaredDirectoryFiles(t *testing.T) {
	root := contractScopeFixture(t)
	scopePut(t, root, "docs/dev/contracts/CONTRACTS-001.md", "> 需求: REQ-001\n> Shared model policy: none\n> Shared model reason: No shared data in this fixture.\n\nCASE-CURRENT\n")
	if problems := scopedCheck(t, root); !strings.Contains(problems, "declares no contract members") {
		t.Fatalf("partial index silently became a complete batch: %s", problems)
	}
}
