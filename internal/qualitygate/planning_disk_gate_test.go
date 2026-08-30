package qualitygate_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

// listingFiles extends memoryFiles with directory listing so the evaluator
// can discover disk-declared artifacts.
type listingFiles map[string][]byte

func (m listingFiles) ReadFile(path string) ([]byte, error) {
	data, ok := m[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (m listingFiles) ReadDir(dir string) ([]os.DirEntry, error) {
	prefix := ""
	if dir != "." {
		prefix = strings.TrimSuffix(dir, "/") + "/"
	}
	var entries []os.DirEntry
	seen := map[string]bool{}
	for path := range m {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		name := strings.SplitN(strings.TrimPrefix(path, prefix), "/", 2)[0]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		entries = append(entries, memoryDirEntry{name: name})
	}
	return entries, nil
}

type memoryDirEntry struct{ name string }

func (e memoryDirEntry) Name() string               { return e.name }
func (e memoryDirEntry) IsDir() bool                { return !strings.HasSuffix(e.name, ".md") }
func (e memoryDirEntry) Type() os.FileMode          { return 0 }
func (e memoryDirEntry) Info() (os.FileInfo, error) { return nil, os.ErrNotExist }

// TestPlanningGatesReadDiskDeclaredArtifacts verifies that the planning
// gates' document precondition must be satisfiable by disk-declared facts
// (contract Status: locked / task Status: complete) — the registration that
// documents[] carries is produced by the gated transitions themselves, so
// requiring it up front deadlocks the hook auto-advance path.
func TestPlanningGatesReadDiskDeclaredArtifacts(t *testing.T) {
	evaluator := newTestEvaluator(t)

	contractData := []byte("# BE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-contract",
		"kind":                    "planning_contract",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"producer_agent_id":       "planner-1",
		"producer_responsibility": "Contract Planner",
		"subject_refs": []any{map[string]any{
			"path": "docs/contracts/BE-001.md", "version": "v1.0.0",
			"sha256": sha256Hex(contractData),
		}},
		"conclusion": "pass",
		"created_at": "2026-08-17T00:00:00Z",
	}
	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	input := qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 3,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "planning", "phase": "contracts", "phase_revision": float64(1)},
				"baseline":   map[string]any{"generation": float64(1)},
				"review":     map[string]any{"round": float64(0)},
				"documents":  []any{},
				"evidence": []any{map[string]any{
					"id": "ev-contract", "kind": "planning_contract", "path": "evidence/contract.json",
					"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": float64(1),
					"review_round": nil, "produced_by": []any{"planner-1"}, "invalidated_by": nil,
					"responsibility_id": "Contract Planner", "scope_refs": []any{},
				}},
			},
		},
		TransitionID: "PTR-PLAN-02",
		GateID:       "GATE-PLANNING-CONTRACTS-COMPLETE",
		Files: listingFiles{
			"docs/contracts/BE-001.md": contractData,
			"evidence/contract.json":   envelopeData,
		},
	}
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusSatisfied {
		t.Fatalf("disk-declared locked contract + qualified planning evidence must satisfy the gate without a pre-existing documents[] registration; got status=%q missing=%#v", result.Status, result.Missing)
	}
}

// TestPlanningGatesStillRefuseWhenDiskAlsoLacks: the disk fallback must not
// weaken the gate — no disk contract and no registration stays NOT_READY.
func TestPlanningGatesStillRefuseWhenDiskAlsoLacks(t *testing.T) {
	evaluator := newTestEvaluator(t)
	input := planningInputForGate(t, "GATE-PLANNING-CONTRACTS-COMPLETE", "PTR-PLAN-02", "contracts", "planning_contract_record", "Contract Planner")
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusNotReady {
		t.Fatalf("gate must stay NOT_READY when neither disk nor documents[] declares the artifact, got %q", result.Status)
	}
}

// TestPlanningDesignGateReadsDiskDeclaredArchitecture verifies that:
// the S2 exit gate must accept a disk-declared locked architecture document
// without a pre-existing documents[] registration (the registration happens
// at PTR-PLAN-01's commit; nothing produced it before, so the organic S2
// exit was deadlocked).
func TestPlanningDesignGateReadsDiskDeclaredArchitecture(t *testing.T) {
	evaluator := newTestEvaluator(t)
	archData := []byte("# ARCHITECTURE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	reqData := []byte("# REQ-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-design",
		"kind":                    "planning_design",
		"runtime_id":              "loop-test",
		"baseline_generation":     1,
		"producer_agent_id":       "architect-1",
		"producer_responsibility": "Architect",
		"subject_refs": []any{
			map[string]any{"path": "docs/design/architecture/ARCHITECTURE-001.md", "version": "v1.0.0", "sha256": sha256Hex(archData)},
			map[string]any{"path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "sha256": sha256Hex(reqData)},
		},
		"conclusion": "pass",
		"created_at": "2026-08-18T00:00:00Z",
	}
	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	input := qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 2,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "planning", "phase": "design", "phase_revision": float64(1)},
				"baseline":   map[string]any{"generation": float64(1)},
				"review":     map[string]any{"round": float64(0)},
				"documents": []any{ // req registered by bind; design NOT registered (organic pre-commit state)
					map[string]any{"id": "REQ-001", "kind": "req", "path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "sha256": sha256Hex(reqData), "status": "locked", "generation": float64(1)},
				},
				"evidence": []any{map[string]any{
					"id": "ev-design", "kind": "planning_design", "path": "evidence/design.json",
					"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": float64(1),
					"review_round": nil, "produced_by": []any{"architect-1"}, "invalidated_by": nil,
					"responsibility_id": "Architect", "scope_refs": []any{},
				}},
			},
		},
		TransitionID: "PTR-PLAN-01",
		GateID:       "GATE-PLANNING-DESIGN-COMPLETE",
		Files: listingFiles{
			"docs/design/architecture/ARCHITECTURE-001.md": archData,
			"docs/requirements/REQ-001.md":                 reqData,
			"evidence/design.json":                         envelopeData,
		},
	}
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != qualitygate.StatusSatisfied {
		t.Fatalf("disk-declared locked architecture + registered req must satisfy the S2 exit gate; got status=%q missing=%#v conflicts=%v", result.Status, result.Missing, result.Conflicts)
	}
}

// TestDocumentPassGateFlagsRegisteredDocumentDrift verifies that:
// a registered document whose on-disk sha no longer matches must block
// GATE-DOCUMENT-PASS with the path named — otherwise a document the
// reviewers never saw can be re-registered from disk and locked into
// building by TR-003's commit.
func TestDocumentPassGateFlagsRegisteredDocumentDrift(t *testing.T) {
	evaluator := newTestEvaluator(t)
	contractData := []byte("# BE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	driftedData := []byte("# BE-001 (edited after review)\n\n> 状态：locked\n> 版本：v1.0.0\n")
	doc := func(data []byte) map[string]any {
		return map[string]any{"id": "BE-001", "kind": "contract", "path": "docs/contracts/BE-001.md", "version": "v1.0.0", "sha256": sha256Hex(data), "status": "locked", "generation": float64(1)}
	}
	input := qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 5,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "document_verification", "phase": nil, "phase_revision": float64(1)},
				"baseline":   map[string]any{"generation": float64(1)},
				"review":     map[string]any{"round": float64(0)},
				// registered with the ORIGINAL sha; disk carries the edited bytes
				"documents": []any{doc(contractData)},
				"evidence":  []any{},
			},
		},
		TransitionID: "TR-003",
		GateID:       "GATE-DOCUMENT-PASS",
		Files: listingFiles{
			"docs/contracts/BE-001.md": driftedData,
		},
	}
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	blocked := false
	for _, conflict := range result.Conflicts {
		if strings.Contains(conflict, "document_drift:docs/contracts/BE-001.md") {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("a registered document drifting on disk must produce a document_drift conflict naming the path; got status=%q conflicts=%v missing=%v", result.Status, result.Conflicts, result.Missing)
	}
}

// TestREVTemplateEnvelopeTeachesTheTruth verifies that the §0
// envelope skeleton in REV-template.md, filled with real values, must
// pass the evaluator's full field validation — if the template drifts
// from what the machine checks (field renamed, conclusion vocabulary
// changed), this test goes red.
func TestREVTemplateEnvelopeTeachesTheTruth(t *testing.T) {
	template, err := os.ReadFile(filepath.Join("..", "..", "docs", "reports", "review", "REV-template.md"))
	if err != nil {
		t.Fatalf("read REV-template: %v", err)
	}
	content := string(template)
	start := strings.Index(content, "```json")
	if start < 0 {
		t.Fatalf("template §0 must carry a fenced JSON skeleton")
	}
	body := content[start+7:]
	end := strings.Index(body, "```")
	if end < 0 {
		t.Fatalf("template §0 JSON block is not closed")
	}
	block := body[:end]

	contractData := []byte("# BE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	replace := map[string]string{
		`"填写 REV-{runid}-{resp}（与文件名一致，机器互证）"`: `"ev-dv-spec"`,
		`"document_review"`: `"document_review"`,
		`"填写当前 runtime id（从 .claude/loop-state.json 顶部复制）"`:                                                                  `"loop-test"`,
		`"填写当前 baseline generation（同上）"`:                                                                                     `1`,
		`"填写你的 agent id（你是谁就写谁——独立性机器核对两条证据互异）"`:                                                                             `"dv-spec-1"`,
		`"填写 DV-SPEC-CONSISTENCY 或 DV-TASK-EXECUTABILITY（激活信封指定的职责，错值 gate 直接 Unknown）"`:                                     `"DV-SPEC-CONSISTENCY"`,
		`"审查完成后回填，三选一：pass / fix_required / req_change_required（与 gate 同词，全流程没有第二套枚举）"`:                                      `"pass"`,
		`"仅 fix_required 时填 document_fix_required（触发 TR-004 回 planning）；pass 留空；req_change_required 时填 req_change_required"`: `""`,
		`"填写 ISO 时间戳"`: `"2026-08-18T00:00:00Z"`,
	}
	filled := block
	for old, newVal := range replace {
		filled = strings.ReplaceAll(filled, old, newVal)
	}
	// subject_refs placeholder row → the real current documents set
	manualNote := "手动从 .claude/loop-state.json 的 documents[] 逐条复制 {path, version, sha256}——多一少一都拒。故意没有自动命令：逐条抄写就是'我签的是哪一版'的对峙，这一步的笨拙是审查的锚"
	filled = strings.ReplaceAll(filled,
		`[`+"\n    "+`"`+manualNote+`"`+"\n  ]",
		`[{"path": "docs/contracts/BE-001.md", "version": "v1.0.0", "sha256": "`+sha256Hex(contractData)+`"}]`)
	if strings.Contains(filled, "填写") || strings.Contains(filled, "手动从") {
		t.Fatalf("test does not know how to fill the template anymore — template placeholders changed:\n%q", filled)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(filled), &envelope); err != nil {
		t.Fatalf("filled template envelope is not valid JSON: %v\n%s", err, filled)
	}
	envelopeData, _ := json.Marshal(envelope)

	evaluator := newTestEvaluator(t)
	input := qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 5,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "document_verification", "phase": nil, "phase_revision": float64(1)},
				"baseline":   map[string]any{"generation": float64(1)},
				"review":     map[string]any{"round": float64(0)},
				"documents": []any{map[string]any{
					"id": "BE-001", "kind": "contract", "path": "docs/contracts/BE-001.md",
					"version": "v1.0.0", "sha256": sha256Hex(contractData), "status": "locked", "generation": float64(1),
				}},
				"evidence": []any{map[string]any{
					"id": "ev-dv-spec", "kind": "document_review", "path": "evidence/dv-spec.json",
					"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": float64(1),
					"review_round": nil, "produced_by": []any{"dv-spec-1"}, "invalidated_by": nil,
					"responsibility_id": "DV-SPEC-CONSISTENCY", "scope_refs": []any{},
				}},
			},
		},
		TransitionID: "TR-003",
		GateID:       "GATE-DOCUMENT-PASS",
		Files: listingFiles{
			"docs/contracts/BE-001.md": contractData,
			"evidence/dv-spec.json":    envelopeData,
		},
	}
	// A single responsibility passes only its own requirement; the gate
	// stays not_ready for the other one — assert OUR evidence qualified.
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	qualified := false
	for _, ref := range result.EvidenceRefs {
		if ref == "ev-dv-spec" {
			qualified = true
		}
	}
	if !qualified {
		t.Fatalf("the template-taught envelope must qualify at the gate; got status=%q missing=%v conflicts=%v", result.Status, result.Missing, result.Conflicts)
	}
}

// TestREVTemplateEnvelopeFixRequiredVariant extends the C5 lock to the
// fix_required branch: the taught requested_event value must satisfy
// GATE-DOCUMENT-FIX-REQUIRED's requested requirement (round-3 H2/#1 —
// the template once taught a dead value on the req_change branch).
func TestREVTemplateEnvelopeFixRequiredVariant(t *testing.T) {
	evaluator := newTestEvaluator(t)
	contractData := []byte("# BE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-dv-fix", "kind": "document_review",
		"runtime_id": "loop-test", "baseline_generation": 1,
		"producer_agent_id": "dv-spec-1", "producer_responsibility": "DV-SPEC-CONSISTENCY",
		"subject_refs": []any{map[string]any{"path": "docs/contracts/BE-001.md", "version": "v1.0.0", "sha256": sha256Hex(contractData)}},
		"conclusion":   "fix_required", "requested_event": "document_fix_required",
		"created_at": "2026-08-18T00:00:00Z",
	}
	envelopeData, _ := json.Marshal(envelope)
	input := qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 5,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "document_verification", "phase": nil, "phase_revision": float64(1)},
				"baseline":   map[string]any{"generation": float64(1)},
				"review":     map[string]any{"round": float64(0)},
				"documents": []any{map[string]any{
					"id": "BE-001", "kind": "contract", "path": "docs/contracts/BE-001.md",
					"version": "v1.0.0", "sha256": sha256Hex(contractData), "status": "locked", "generation": float64(1),
				}},
				"evidence": []any{map[string]any{
					"id": "ev-dv-fix", "kind": "document_review", "path": "evidence/dv-fix.json",
					"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": float64(1),
					"review_round": nil, "produced_by": []any{"dv-spec-1"}, "invalidated_by": nil,
					"responsibility_id": "DV-SPEC-CONSISTENCY", "scope_refs": []any{},
				}},
			},
		},
		TransitionID: "TR-004",
		GateID:       "GATE-DOCUMENT-FIX-REQUIRED",
		Files: listingFiles{
			"docs/contracts/BE-001.md": contractData,
			"evidence/dv-fix.json":     envelopeData,
		},
	}
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	qualified := false
	for _, ref := range result.EvidenceRefs {
		if ref == "ev-dv-fix" {
			qualified = true
		}
	}
	if !qualified {
		t.Fatalf("the taught fix_required envelope must satisfy GATE-DOCUMENT-FIX-REQUIRED; got status=%q missing=%v conflicts=%v", result.Status, result.Missing, result.Conflicts)
	}
}

// TestPlanningEnvelopeTeachesTheTruth pins the v4.5.4 fix: the JSON
// skeleton taught in specification-planning's Planning Evidence Envelopes
// section, filled with real values, must qualify the S2 design gate — a
// markdown path or a missing --expected-revision-style mistake goes red.
func TestPlanningEnvelopeTeachesTheTruth(t *testing.T) {
	skill, err := os.ReadFile(filepath.Join("..", "..", "skills", "specification-planning", "SKILL.md"))
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	content := string(skill)
	start := strings.Index(content, "```json")
	if start < 0 {
		t.Fatalf("skill must carry a fenced JSON envelope skeleton")
	}
	body := content[start+7:]
	end := strings.Index(body, "```")
	if end < 0 {
		t.Fatalf("envelope skeleton not closed")
	}
	block := body[:end]

	archData := []byte("# ARCHITECTURE-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	reqData := []byte("# REQ-001\n\n> 状态：locked\n> 版本：v1.0.0\n")
	replace := map[string]string{
		`"planning-{design|contracts|tasks}-pass"`:              `"planning-design-pass"`,
		`"planning_design | planning_contract | planning_task"`: `"planning_design"`,
		`"从 .claude/loop-state.json 顶部复制"`:                      `"loop-test"`,
		`"{当前 baseline generation——数字，如 1}"`:                    `1`,
		`"你的 agent id"`: `"architect-1"`,
		`"Architect（S2）/ Contract Planner（S3）/ Task Planner（S4）——gate 按此词白名单，逐字匹配"`: `"Architect"`,
	}
	filled := block
	for old, newVal := range replace {
		filled = strings.ReplaceAll(filled, old, newVal)
	}
	if strings.Contains(filled, "{") && strings.Contains(filled, "替换") {
		t.Fatalf("unfilled placeholder remains:\n%s", filled)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(filled), &envelope); err != nil {
		t.Fatalf("taught skeleton is not valid JSON: %v\n%s", err, filled)
	}
	envelopeData, _ := json.Marshal(envelope)

	evaluator := newTestEvaluator(t)
	input := qualitygate.Input{
		Snapshot: runtime.Snapshot{
			Revision: 2,
			State: map[string]any{
				"runtime_id": "loop-test",
				"lifecycle":  map[string]any{"state": "planning", "phase": "design", "phase_revision": float64(1)},
				"baseline":   map[string]any{"generation": float64(1)},
				"review":     map[string]any{"round": float64(0)},
				"documents": []any{map[string]any{
					"id": "REQ-001", "kind": "req", "path": "docs/requirements/REQ-001.md", "version": "v1.0.0", "sha256": sha256Hex(reqData), "status": "locked", "generation": float64(1),
				}},
				"evidence": []any{map[string]any{
					"id": "planning-design-pass", "kind": "planning_design", "path": "evidence/design.json",
					"sha256": sha256Hex(envelopeData), "status": "valid", "baseline_generation": float64(1),
					"review_round": nil, "produced_by": []any{"architect-1"}, "invalidated_by": nil,
					"responsibility_id": "Architect", "scope_refs": []any{},
				}},
			},
		},
		TransitionID: "PTR-PLAN-01",
		GateID:       "GATE-PLANNING-DESIGN-COMPLETE",
		Files: listingFiles{
			"docs/design/architecture/ARCHITECTURE-001.md": archData,
			"docs/requirements/REQ-001.md":                 reqData,
			"evidence/design.json":                         envelopeData,
		},
	}
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	qualified := false
	for _, ref := range result.EvidenceRefs {
		if ref == "planning-design-pass" {
			qualified = true
		}
	}
	if !qualified {
		t.Fatalf("the taught planning envelope must qualify the S2 design gate; got status=%q missing=%v conflicts=%v", result.Status, result.Missing, result.Conflicts)
	}
}
