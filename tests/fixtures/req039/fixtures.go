// Package req039fixtures provides shared temp-repository fixtures and Hook
// drivers for REQ-039 CT-039-11~24 and FR-024/FR-025 system tests.
package req039fixtures

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// CLIRunner wraps cli.Run and counts manual runtime transition invocations
// (FR-024 acceptance: automatic scenarios must not call transition CLI).
type CLIRunner struct {
	ManualTransitionCalls int
}

func (r *CLIRunner) Run(t *testing.T, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	t.Helper()
	if isManualTransitionCLI(args) {
		r.ManualTransitionCalls++
	}
	return cli.Run(args, stdin, stdout, stderr)
}

func isManualTransitionCLI(args []string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "runtime" && args[i+1] == "transition" {
			return true
		}
	}
	return false
}

// RepoRoot resolves the module root from the caller's source location.
func RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not resolve repository root")
	return ""
}

// FreshRoot copies loop-definition.json and hook-policy.json into a temp repo.
func FreshRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	repo := RepoRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"docs/loop-definition.json",
		"docs/hook-policy.json",
		// RC-06 (S10-3): the protected-release policy rule loads the
		// data-driven protected-commands table from the runtime root; the
		// fixture must ship the real table so Bash classification sees the
		// production surface instead of failing closed on a missing file.
		"docs/release_audits/protected_commands.json",
	} {
		data, err := os.ReadFile(filepath.Join(repo, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		target := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// definitionRefs returns sha256 fingerprints for on-disk docs in root.
func definitionRefs(t *testing.T, root string) (defSHA, policySHA string) {
	t.Helper()
	defBytes, err := os.ReadFile(filepath.Join(root, "docs", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	polBytes, err := os.ReadFile(filepath.Join(root, "docs", "hook-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	return Sha256Hex(defBytes), Sha256Hex(polBytes)
}

// WriteState persists loop-state.json under root/.claude/.
func WriteState(t *testing.T, root string, state map[string]any) {
	t.Helper()
	path := filepath.Join(root, ".claude", "loop-state.json")
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// ReadState loads loop-state.json from root/.claude/.
func ReadState(t *testing.T, root string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

// Sha256Hex returns the lowercase hex digest of data.
func Sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// RuntimeIDFromState returns state.runtime_id or the CT fixture default.
func RuntimeIDFromState(state map[string]any) string {
	if id, _ := state["runtime_id"].(string); id != "" {
		return id
	}
	return "loop-req039-ct"
}

func runtimeIDFromState(state map[string]any) string {
	return RuntimeIDFromState(state)
}

// EvidenceEnvelope builds a schema-valid on-disk evidence envelope using the
// current state runtime_id (never hardcoded — avoids drift vs system helpers).
func EvidenceEnvelope(state map[string]any, id, kind, agent, responsibility, conclusion string, extra map[string]any) map[string]any {
	envelope := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             id,
		"kind":                    kind,
		"runtime_id":              RuntimeIDFromState(state),
		"baseline_generation":     1,
		"review_round":            1,
		"producer_agent_id":       agent,
		"producer_responsibility": responsibility,
		"conclusion":              conclusion,
		"created_at":              "2026-07-30T00:00:00Z",
	}
	for k, v := range extra {
		envelope[k] = v
	}
	return envelope
}

// WriteEvidenceEnvelope persists envelope JSON and returns a runtime evidence index entry.
func WriteEvidenceEnvelope(t *testing.T, root string, state map[string]any, id, wireKind, agent, responsibility string, envelope map[string]any, scope []any) map[string]any {
	t.Helper()
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	rel := writeEvidenceFile(t, root, id+".json", data)
	return evidenceIndexEntry(id, wireKind, rel, Sha256Hex(data), envelopeReviewRound(envelope), agent, responsibility, scope)
}

func envelopeReviewRound(envelope map[string]any) int {
	if rr, ok := envelope["review_round"].(int); ok {
		return rr
	}
	if rr, ok := envelope["review_round"].(float64); ok {
		return int(rr)
	}
	return 1
}

// BaseState returns a schema-valid loop-state skeleton for Hook-driven tests.
func BaseState(t *testing.T, root, lifecycleState, phase string, revision int) map[string]any {
	t.Helper()
	// req_baseline_unchanged is a real fingerprint guard (L3-S6 P0-4): the
	// bound REQ file must exist with bytes matching the pinned sha. Write
	// a stub REQ when the fixture root has none so every seeded state is
	// fingerprint-consistent.
	reqRel := "docs/requirements/REQ-039-loop-control-plane.md"
	reqBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(reqRel)))
	if err != nil {
		reqBytes = []byte("# REQ-039\n> Status: locked\n> Version: v2.0.0\n")
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.FromSlash(reqRel))), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(reqRel)), reqBytes, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	defSHA, policySHA := definitionRefs(t, root)
	stage := stageFor(lifecycleState, phase)
	return map[string]any{
		"schema_version": "1.1.0",
		"runtime_id":     "loop-req039-ct",
		"definition": map[string]any{
			"path":    "docs/loop-definition.json",
			"version": "1.1.0",
			"sha256":  defSHA,
		},
		"revision": float64(revision),
		"lifecycle": map[string]any{
			"state":          lifecycleState,
			"phase":          phaseOrNil(phase),
			"phase_revision": 0,
		},
		"milestone": map[string]any{
			"stage":           stage,
			"lifecycle_state": lifecycleState,
			"lifecycle_phase": phaseOrNil(phase),
			"objective":       "CT fixture at " + lifecycleState,
			"action":          "advance via Hook",
			"protocol_ref":    "docs/agent-protocol.md",
			"manual_ref":      ".claude/bin/loop-harness.md",
			"primary_skill":   "loop-orchestration",
			"read":            []any{".claude/loop-state.json"},
			"read_order":      []any{"LOOP RECOVERY packet (this message)", ".claude/loop-state.json"},
			"missing":         []any{},
			"done_when":       []any{},
			"questions":       []any{},
			"automation":      []any{"do not call loop-harness for normal continuation"},
			"integration":     []any{},
			"human_required":  false,
			"blocked":         false,
			"blocker":         nil,
			"event":           "PreToolUse",
			"instruction":     "LOOP RECOVERY: you are at " + stage + ".",
			"recovery":        []any{"read .claude/loop-state.json"},
			"source_revision": float64(revision),
			"updated_at":      time.Now().UTC().Format(time.RFC3339Nano),
		},
		"authorization": map[string]any{
			"mode":        "binding",
			"command":     "loop-harness req bind",
			"actor":       "user",
			"occurred_at": "2026-07-30T00:00:00Z",
		},
		"bound_req": map[string]any{
			"id":          "REQ-039",
			"path":        reqRel,
			"version":     "v2.0.0",
			"sha256":      Sha256Hex(reqBytes),
			"status":      "locked",
			"approved_by": "user",
			"approved_at": "2026-07-30T00:00:00Z",
			"metadata":    map[string]any{"ui_impact": "none"},
		},
		"baseline": map[string]any{"generation": 1, "captured_at": "2026-07-30T00:00:00Z"},
		"review":   map[string]any{"round": 1, "clean_round": nil},
		"configuration": map[string]any{
			"repair": map[string]any{
				"max_attempts_per_bug":       3,
				"max_same_contract_failures": 2,
				"max_full_review_rounds":     5,
			},
		},
		"entities":        map[string]any{"agents": []any{}, "tasks": []any{}, "bugs": []any{}, "teams": []any{}},
		"documents":       []any{},
		"evidence":        []any{},
		"blockers":        []any{},
		"pause":           nil,
		"last_transition": nil,
		"journal": map[string]any{
			"path":          ".claude/loop-events.jsonl",
			"last_sequence": 0,
			"last_event_id": nil,
		},
		"hook_control": map[string]any{
			"policy_ref": map[string]any{
				"path": "docs/hook-policy.json", "version": "v2.0.0",
				"sha256": policySHA,
			},
			"mode": "enforce", "health": "healthy", "consecutive_failures": 0, "last_checked_at": nil,
		},
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func phaseOrNil(phase string) any {
	if phase == "" {
		return nil
	}
	return phase
}

func stageFor(state, phase string) string {
	switch state {
	case "planning":
		switch phase {
		case "design":
			return "S2"
		case "contracts":
			return "S3"
		case "tasks":
			return "S4"
		}
	case "document_verification":
		return "S5"
	case "building":
		return "S6"
	case "verification":
		return "S7"
	case "bug_resolution":
		return "S8"
	case "acceptance":
		return "S10"
	case "release_audit":
		return "S10"
	case "awaiting_human_release":
		return "S11"
	}
	return "S2"
}

// Lifecycle extracts lifecycle.state and lifecycle.phase from a state map.
func Lifecycle(state map[string]any) (string, string) {
	lc, _ := state["lifecycle"].(map[string]any)
	st, _ := lc["state"].(string)
	ph, _ := lc["phase"].(string)
	if lc["phase"] == nil {
		ph = ""
	}
	return st, ph
}

// LastTransitionID returns the transition_id from last_transition when present.
func LastTransitionID(state map[string]any) string {
	lt, _ := state["last_transition"].(map[string]any)
	if lt == nil {
		return ""
	}
	id, _ := lt["transition_id"].(string)
	return id
}

// Revision returns the numeric revision from state.
func Revision(state map[string]any) float64 {
	rev, _ := state["revision"].(float64)
	return rev
}

// RunHook invokes the hook CLI entry for event with JSON body.
func RunHook(t *testing.T, runner *CLIRunner, root, event, body string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runner.Run(t, []string{"hook", "--event", event, "--root", root}, bytes.NewReader([]byte(body)), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// ParseQualityGate decodes hook stdout and returns the envelope and quality_gate block.
func ParseQualityGate(t *testing.T, raw string) (map[string]any, map[string]any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.UseNumber()
	var env map[string]any
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("hook output is not JSON: %v\noutput=%s", err, raw)
	}
	qg, _ := env["quality_gate"].(map[string]any)
	if qg == nil {
		if hsp, ok := env["hookSpecificOutput"].(map[string]any); ok {
			qg, _ = hsp["quality_gate"].(map[string]any)
		}
	}
	return env, qg
}

type docSeed struct {
	id, kind, path, version string
	data                    []byte
}

// ensureS5DocumentBaseline returns the subject list the DV envelopes must
// sign over. On the organic path (documents[] already registered by
// bind/PTR-PLAN-01/PTR-PLAN-02/TR-002) it derives subjects from the current
// registrations and writes nothing — the organic chain owns the files. On a
// compressed precondition (empty documents[]) it writes the four files with
// proper status headers and registers them, as a precondition seed (the
// documented compressed-seed pattern).
func ensureS5DocumentBaseline(t *testing.T, root string, state map[string]any) []any {
	t.Helper()
	if existing, _ := state["documents"].([]any); len(existing) > 0 {
		var subjects []any
		for _, raw := range existing {
			doc, _ := raw.(map[string]any)
			if doc == nil {
				continue
			}
			path, _ := doc["path"].(string)
			version, _ := doc["version"].(string)
			sha, _ := doc["sha256"].(string)
			if path == "" || sha == "" {
				continue
			}
			subjects = append(subjects, map[string]any{"path": path, "version": version, "sha256": sha})
		}
		if len(subjects) == 0 {
			t.Fatal("ensureS5DocumentBaseline: documents[] present but carries no path/sha entries")
		}
		return subjects
	}
	docs := []docSeed{
		{"REQ-039", "req", "docs/requirements/REQ-039-loop-control-plane.md", "v2.0.0", []byte("# REQ-039\n\n> 状态：locked\n> 版本：v2.0.0\n")},
		{"ARCH-039", "design", "docs/design/architecture/ARCHITECTURE-039-loop-control-plane.md", "v2.0.2", []byte("# ARCH\n\n> 状态：locked\n> 版本：v2.0.2\n")},
		{"BE-039", "contract", "docs/contracts/BE-039-loop-controller.md", "v1.0.2", []byte("# BE\n\n> 状态：locked\n> 版本：v1.0.2\n\n### 需求条款映射\n\n| REQ source_ref | Rule / CASE | 本合同条款 | 验收标准 |\n|---|---|---|---|\n| — | — | §1 | — |\n")},
		{"TASK-039-01", "task", "docs/tasks/TASK-039-01-loop-definition.md", "v1.0.2", []byte("# TASK-039-01\n\n> 状态：complete\n> 版本：v1.0.2\n> Primary contract: BE-039-loop-controller\n")},
	}
	var documents, subjects []any
	for _, d := range docs {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, d.path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, d.path), d.data, 0o644); err != nil {
			t.Fatal(err)
		}
		documents = append(documents, map[string]any{
			"id": d.id, "kind": d.kind, "path": d.path, "version": d.version,
			"sha256": Sha256Hex(d.data), "status": "locked", "generation": 1,
		})
		subjects = append(subjects, map[string]any{
			"path": d.path, "version": d.version, "sha256": Sha256Hex(d.data),
		})
	}
	state["documents"] = documents
	return subjects
}

// appendEvidence adds entries to the existing evidence index (organic chain
// evidence stays; the old seeds replaced the whole index).
func appendEvidence(state map[string]any, entries []any) {
	existing, _ := state["evidence"].([]any)
	state["evidence"] = append(existing, entries...)
}

// SeedDocumentPassS5 writes locked documents and dual independent PASS review
// evidence for GATE-DOCUMENT-PASS / TR-003 (CT-039-11).
func SeedDocumentPassS5(t *testing.T, root string, state map[string]any, specAgent, taskAgent string) {
	t.Helper()
	// Derive from the current baseline (organic registrations win; the seed
	// never replaces them) and keep the production round semantics (S5 is
	// round 0 — no forcing to 1, which used to mask the round-0 behavior).
	subjects := ensureS5DocumentBaseline(t, root, state)
	round := reviewRoundFromState(state)
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	var entries []any
	addReview := func(id, responsibility, agent string) {
		envelope := map[string]any{
			"schema_version": "1.0.0", "evidence_id": id, "kind": "document_review",
			"runtime_id": runtimeIDFromState(state), "baseline_generation": 1,
			"producer_agent_id": agent, "producer_responsibility": responsibility,
			"subject_refs": subjects, "conclusion": "pass", "created_at": "2026-07-30T00:00:00Z",
		}
		if round > 0 {
			envelope["review_round"] = round
		}
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		rel := "evidence/" + id + ".json"
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, evidenceIndexEntry(id, "document_review", rel, Sha256Hex(data), round, agent, responsibility, []any{}))
	}
	addReview("ev-dv-spec", "DV-SPEC-CONSISTENCY", specAgent)
	addReview("ev-dv-task", "DV-TASK-EXECUTABILITY", taskAgent)
	appendEvidence(state, entries)
	state["lifecycle"] = map[string]any{"state": "document_verification", "phase": nil, "phase_revision": 0}
	state["milestone"].(map[string]any)["stage"] = "S5"
	state["milestone"].(map[string]any)["lifecycle_state"] = "document_verification"
	state["milestone"].(map[string]any)["lifecycle_phase"] = nil
}

// SeedDocumentFixRequiredS5 seeds document_verification with dual DV evidence
// concluding fix_required + requested_event document_fix_required
// (GATE-DOCUMENT-FIX-REQUIRED / TR-004).
func SeedDocumentFixRequiredS5(t *testing.T, root string, state map[string]any) {
	t.Helper()
	SeedDocumentFixRequired(t, root, state, "dv-spec-fix", "dv-task-fix")
}

// SeedBuilderBatchReady seeds building state with batch completion evidence for TR-006.
func SeedBuilderBatchReady(t *testing.T, root string, state map[string]any) {
	t.Helper()
	taskOne := []byte("# TASK 1\n")
	taskTwo := []byte("# TASK 2\n")
	for _, pair := range []struct {
		path string
		data []byte
	}{
		{"docs/tasks/TASK-039-01-loop-definition.md", taskOne},
		{"docs/tasks/TASK-039-02-controller-cycle.md", taskTwo},
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, pair.path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, pair.path), pair.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	documents := []any{
		map[string]any{
			"id": "TASK-039-01", "kind": "task", "path": "docs/tasks/TASK-039-01-loop-definition.md",
			"version": "v1.0.2", "sha256": Sha256Hex(taskOne), "status": "locked", "generation": 1,
		},
		map[string]any{
			"id": "TASK-039-02", "kind": "task", "path": "docs/tasks/TASK-039-02-controller-cycle.md",
			"version": "v1.0.2", "sha256": Sha256Hex(taskTwo), "status": "locked", "generation": 1,
		},
	}
	subjects := []any{
		map[string]any{"path": "docs/tasks/TASK-039-01-loop-definition.md", "version": "v1.0.2", "sha256": Sha256Hex(taskOne)},
		map[string]any{"path": "docs/tasks/TASK-039-02-controller-cycle.md", "version": "v1.0.2", "sha256": Sha256Hex(taskTwo)},
	}
	var evidence []any
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	round := reviewRoundFromState(state) // TR-006 commits the FIRST round bump — its pre-commit evidence is round 0
	add := func(id, wireKind, responsibility, conclusion, taskID string) {
		envelope := map[string]any{
			"schema_version": "1.0.0", "evidence_id": id, "kind": wireKind,
			"runtime_id": runtimeIDFromState(state), "baseline_generation": 1,
			"producer_agent_id": "builder-1", "producer_responsibility": responsibility,
			"subject_refs": subjects, "conclusion": conclusion, "created_at": "2026-07-30T00:00:00Z",
		}
		if round > 0 {
			envelope["review_round"] = round
		}
		if taskID != "" {
			envelope["task_id"] = taskID
		}
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		rel := writeEvidenceFile(t, root, id+".json", data)
		evidence = append(evidence, evidenceIndexEntry(id, wireKind, rel, Sha256Hex(data), round, "builder-1", responsibility, []any{}))
	}
	add("ev-completion-1", "agent_completion", "BUILD-WORK-PACKAGE", "completed", "TASK-039-01")
	add("ev-completion-2", "agent_completion", "BUILD-WORK-PACKAGE", "completed", "TASK-039-02")
	add("ev-team", "builder_report", "Orchestrator", "complete", "")
	// L3-S6 P0-1: GATE-BUILDER-BATCH-READY verifies a durable integration
	// checkpoint per batch TASK (task_id + state>=verified) — seed both so
	// the satisfied path reflects the real gate contract.
	runtimeID := runtimeIDFromState(state)
	for index, taskID := range []string{"TASK-039-01", "TASK-039-02"} {
		assignmentID := fmt.Sprintf("assignment-batch-%02d", index+1)
		checkpointDir := filepath.Join(root, ".claude", "evidence", runtimeID, "g1", "worktree", assignmentID)
		if err := os.MkdirAll(checkpointDir, 0o755); err != nil {
			t.Fatal(err)
		}
		checkpoint := map[string]any{
			"assignment_id":       assignmentID,
			"task_id":             taskID,
			"source_branch":       "wt/" + assignmentID,
			"target_branch":       "develop",
			"baseline_generation": 1,
			"state":               "verified",
			"revision":            1,
			"worktree_path":       filepath.Join(root, ".worktrees", assignmentID),
			"idempotency_key":     assignmentID + "|src|develop|1",
			"updated_at":          "2026-07-30T00:00:00Z",
		}
		data, err := json.Marshal(checkpoint)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(checkpointDir, "checkpoint.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state["documents"] = documents
	state["evidence"] = evidence
	state["entities"] = map[string]any{
		"agents": []any{},
		"tasks": []any{
			map[string]any{
				"id": "TASK-039-01", "state": "review",
				"path":   "docs/tasks/TASK-039-01-loop-definition.md",
				"sha256": Sha256Hex(taskOne), "owner_agent_ids": []any{"builder-1"},
			},
			map[string]any{
				"id": "TASK-039-02", "state": "review",
				"path":   "docs/tasks/TASK-039-02-controller-cycle.md",
				"sha256": Sha256Hex(taskTwo), "owner_agent_ids": []any{"builder-1"},
			},
		},
		"bugs": []any{}, "teams": []any{},
	}
	state["lifecycle"] = map[string]any{"state": "building", "phase": nil, "phase_revision": 0}
	state["milestone"].(map[string]any)["stage"] = "S6"
	state["milestone"].(map[string]any)["lifecycle_state"] = "building"
}

// SeedDualDVSameAgent seeds document_verification with one agent on both DV labels (CT-039-24).
func SeedDualDVSameAgent(t *testing.T, root string, state map[string]any) {
	SeedDocumentPassS5(t, root, state, "reviewer-1", "reviewer-1")
}

// SeedDocumentFixRequired seeds document_verification with dual DV evidence
// concluding fix_required + requested_event document_fix_required (TR-004 /
// GATE-DOCUMENT-FIX-REQUIRED).
func SeedDocumentFixRequired(t *testing.T, root string, state map[string]any, specAgent, taskAgent string) {
	t.Helper()
	subjects := ensureS5DocumentBaseline(t, root, state)
	round := reviewRoundFromState(state)
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	var entries []any
	addReview := func(id, responsibility, agent string) {
		envelope := map[string]any{
			"schema_version": "1.0.0", "evidence_id": id, "kind": "document_review",
			"runtime_id": runtimeIDFromState(state), "baseline_generation": 1,
			"producer_agent_id": agent, "producer_responsibility": responsibility,
			"subject_refs": subjects, "conclusion": "fix_required",
			"requested_event": "document_fix_required", "created_at": "2026-07-30T00:00:00Z",
		}
		if round > 0 {
			envelope["review_round"] = round
		}
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		rel := "evidence/" + id + ".json"
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, evidenceIndexEntry(id, "document_review", rel, Sha256Hex(data), round, agent, responsibility, []any{}))
	}
	addReview("ev-dv-fix-spec", "DV-SPEC-CONSISTENCY", specAgent)
	addReview("ev-dv-fix-task", "DV-TASK-EXECUTABILITY", taskAgent)
	appendEvidence(state, entries)
	state["lifecycle"] = map[string]any{"state": "document_verification", "phase": nil, "phase_revision": 0}
}

func SeedVerificationDelivery(t *testing.T, root string, state map[string]any) {
	t.Helper()
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "delivery", "phase_revision": 0}
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	state["milestone"].(map[string]any)["stage"] = "S7"
	state["milestone"].(map[string]any)["lifecycle_state"] = "verification"
	state["milestone"].(map[string]any)["lifecycle_phase"] = "delivery"
	EnsureVerificationWorkgroups(state)
}

// SeedConflictingDeliveryEvents adds two qualified requested_event facts at
// verification.delivery for CT-039-16 selector conflict coverage.
func SeedConflictingDeliveryEvents(t *testing.T, root string, state map[string]any) {
	t.Helper()
	SeedVerificationDelivery(t, root, state)
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	taskData := []byte("# TASK\n")
	taskPath := "docs/tasks/TASK-039-01-loop-definition.md"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, taskPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, taskPath), taskData, 0o644); err != nil {
		t.Fatal(err)
	}
	subject := map[string]any{
		"path": taskPath, "version": "v1.0.2", "sha256": Sha256Hex(taskData),
	}
	state["documents"] = []any{
		map[string]any{
			"id": "TASK-039-01", "kind": "task", "path": taskPath, "version": "v1.0.2",
			"sha256": Sha256Hex(taskData), "status": "locked", "generation": 1,
		},
	}
	var evidence []any
	add := func(id, wireKind, responsibility, conclusion, event string) {
		envelope := map[string]any{
			"schema_version": "1.0.0", "evidence_id": id, "kind": wireKind,
			"runtime_id": runtimeIDFromState(state), "baseline_generation": 1, "review_round": 1,
			"producer_agent_id": "delivery-1", "producer_responsibility": responsibility,
			"subject_refs": []any{subject}, "conclusion": conclusion,
			"requested_event": event, "created_at": "2026-07-30T00:00:00Z",
		}
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		rel := writeEvidenceFile(t, root, id+".json", data)
		evidence = append(evidence, evidenceIndexEntry(id, wireKind, rel, Sha256Hex(data), 1, "delivery-1", responsibility, []any{taskPath}))
	}
	add("ev-delivery-pass", "delivery_review", "Delivery Verifier", "pass", "delivery_pass")
	add("ev-blocking", "bug", "Delivery Verifier", "blocking", "blocking_findings_reported")
	state["evidence"] = evidence
}

// evidenceIndexEntry returns a schema-valid runtime evidence index record.
func evidenceIndexEntry(id, wireKind, path, sha string, round int, producer, responsibility string, scope []any) map[string]any {
	if scope == nil {
		scope = []any{}
	}
	// The schema requires review_round >= 1 when present; round 0 (the S5
	// production semantics) is expressed as nil, matching RecordEvidence.
	var roundValue any
	if round > 0 {
		roundValue = round
	}
	return map[string]any{
		"id": id, "kind": wireKind, "path": path, "sha256": sha,
		"status": "valid", "baseline_generation": 1, "review_round": roundValue,
		"produced_by": []any{producer}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil,
		"responsibility_id": responsibility, "scope_refs": scope,
	}
}

func writeEvidenceFile(t *testing.T, root, name string, data []byte) string {
	t.Helper()
	rel := filepath.Join("evidence", name)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

// SeedCleanRoundIncomplete seeds clean_round_evaluation with incomplete evidence (CT-039-21).
func SeedCleanRoundIncomplete(t *testing.T, root string, state map[string]any) {
	t.Helper()
	SeedVerificationDelivery(t, root, state)
	state["lifecycle"] = map[string]any{"state": "verification", "phase": "clean_round_evaluation", "phase_revision": 0}
	state["milestone"].(map[string]any)["lifecycle_phase"] = "clean_round_evaluation"
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-clean-incomplete", "kind": "clean_round",
		"runtime_id": runtimeIDFromState(state), "baseline_generation": 1, "review_round": 1,
		"producer_agent_id": "orchestrator-1", "producer_responsibility": "Orchestrator",
		"conclusion": "incomplete", "created_at": "2026-07-30T00:00:00Z",
	}
	data, _ := json.Marshal(envelope)
	rel := writeEvidenceFile(t, root, "ev-clean-incomplete.json", data)
	state["evidence"] = []any{
		evidenceIndexEntry("ev-clean-incomplete", "clean_round", rel, Sha256Hex(data), 1, "orchestrator-1", "Orchestrator", []any{}),
	}
}

// SeedBugReportsRejected seeds bug_report_review with rejected batch (CT-039-22).
func SeedBugReportsRejected(t *testing.T, root string, state map[string]any) {
	t.Helper()
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-bug-batch", "kind": "bug",
		"runtime_id": runtimeIDFromState(state), "baseline_generation": 1, "review_round": 1,
		"producer_agent_id": "orchestrator-1", "producer_responsibility": "Orchestrator",
		"conclusion": "rejected", "created_at": "2026-07-30T00:00:00Z",
	}
	data, _ := json.Marshal(envelope)
	rel := writeEvidenceFile(t, root, "ev-bug-batch.json", data)
	state["lifecycle"] = map[string]any{"state": "bug_resolution", "phase": "bug_report_review", "phase_revision": 0}
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	state["evidence"] = []any{
		evidenceIndexEntry("ev-bug-batch", "bug", rel, Sha256Hex(data), 1, "orchestrator-1", "Orchestrator", []any{}),
	}
	state["milestone"].(map[string]any)["stage"] = "S8"
	state["milestone"].(map[string]any)["lifecycle_state"] = "bug_resolution"
	state["milestone"].(map[string]any)["lifecycle_phase"] = "bug_report_review"
}

// SeedTargetedReverificationFail seeds targeted_reverification fail evidence (CT-039-23).
func SeedTargetedReverificationFail(t *testing.T, root string, state map[string]any) {
	t.Helper()
	taskData := []byte("# TASK\n")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, "docs/tasks/TASK-039-01-loop-definition.md")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/tasks/TASK-039-01-loop-definition.md"), taskData, 0o644); err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-tgt-fail", "kind": "targeted_reverification",
		"runtime_id": runtimeIDFromState(state), "baseline_generation": 1, "review_round": 1,
		"producer_agent_id": "finder-1", "producer_responsibility": "Original Finder",
		"subject_refs": []any{
			map[string]any{"path": "docs/tasks/TASK-039-01-loop-definition.md", "version": "v1.0.2", "sha256": Sha256Hex(taskData)},
		},
		"conclusion": "fail", "created_at": "2026-07-30T00:00:00Z",
	}
	data, _ := json.Marshal(envelope)
	rel := writeEvidenceFile(t, root, "ev-tgt-fail.json", data)
	state["lifecycle"] = map[string]any{"state": "bug_resolution", "phase": "targeted_reverification", "phase_revision": 0}
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	state["documents"] = []any{
		map[string]any{
			"id": "TASK-039-01", "kind": "task", "path": "docs/tasks/TASK-039-01-loop-definition.md",
			"version": "v1.0.2", "sha256": Sha256Hex(taskData), "status": "locked", "generation": 1,
		},
	}
	state["evidence"] = []any{
		evidenceIndexEntry("ev-tgt-fail", "targeted_reverification", rel, Sha256Hex(data), 1, "finder-1", "Original Finder", []any{"docs/tasks/TASK-039-01-loop-definition.md"}),
	}
	state["milestone"].(map[string]any)["stage"] = "S8"
	state["milestone"].(map[string]any)["lifecycle_state"] = "bug_resolution"
	state["milestone"].(map[string]any)["lifecycle_phase"] = "targeted_reverification"
}

// SeedAwaitingHumanRelease seeds terminal S11 cursor (CT-039-15 / FR-025).
func SeedAwaitingHumanRelease(t *testing.T, root string, state map[string]any) {
	t.Helper()
	state["lifecycle"] = map[string]any{"state": "awaiting_human_release", "phase": nil, "phase_revision": 0}
	state["milestone"].(map[string]any)["stage"] = "S11"
	state["milestone"].(map[string]any)["lifecycle_state"] = "awaiting_human_release"
	state["milestone"].(map[string]any)["lifecycle_phase"] = nil
	state["milestone"].(map[string]any)["human_required"] = true
}

// SeedG2Rework seeds baseline generation 2 with g1 locked path and g2 active manifest (CT-039-18).
func SeedG2Rework(t *testing.T, root string, state map[string]any) {
	t.Helper()
	g1Path := "docs/design/versions/REQ-039/g1/ARCHITECTURE-039-loop-control-plane.md"
	g2Path := "docs/design/versions/REQ-039/g2/ARCHITECTURE-039-loop-control-plane.md"
	g1Data := []byte("# ARCH g1 immutable\n")
	g2Data := []byte("# ARCH g2 active\n")
	for _, pair := range []struct {
		path string
		data []byte
	}{
		{g1Path, g1Data}, {g2Path, g2Data},
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, pair.path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, pair.path), pair.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state["baseline"] = map[string]any{"generation": 2, "captured_at": "2026-07-30T00:00:00Z"}
	state["documents"] = []any{
		map[string]any{
			"id": "ARCH-039", "kind": "design", "path": g2Path, "version": "v2.0.3",
			"sha256": Sha256Hex(g2Data), "status": "locked", "generation": 2,
		},
		map[string]any{
			"id": "ARCH-039-g1", "kind": "design", "path": g1Path, "version": "v2.0.2",
			"sha256": Sha256Hex(g1Data), "status": "superseded", "generation": 1,
		},
	}
}

// SeedPlanningDesignComplete seeds S2 planning.design with architecture evidence.
func SeedPlanningDesignComplete(t *testing.T, root string, state map[string]any) {
	t.Helper()
	reqData := []byte("# REQ-039\n\n> 状态：locked\n> 版本：v2.0.0\n\n" +
		"| 编号 | 模块 | 需求 | 服务于 | 优先级 |\n|:--|:--|:--|:--|:--|\n| FR-001 | controller | 控制平面 | A1 | Must |\n")
	archData := []byte("# ARCHITECTURE-039\n\n> 状态：locked\n> 版本：v2.0.2\n")
	for _, pair := range []struct {
		path string
		data []byte
	}{
		{"docs/requirements/REQ-039-loop-control-plane.md", reqData},
		{"docs/design/architecture/ARCHITECTURE-039-loop-control-plane.md", archData},
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, pair.path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, pair.path), pair.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-design", "kind": "planning_design",
		"runtime_id": runtimeIDFromState(state), "baseline_generation": 1, "review_round": 1,
		"producer_agent_id": "architect-1", "producer_responsibility": "Architect",
		"subject_refs": []any{
			map[string]any{"path": "docs/design/architecture/ARCHITECTURE-039-loop-control-plane.md", "version": "v2.0.2", "sha256": Sha256Hex(archData)},
		},
		"conclusion": "pass", "created_at": "2026-07-30T00:00:00Z",
	}
	evData, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	evPath := writeEvidenceFile(t, root, "ev-design.json", evData)
	// Do not hand-seed documents[] — the disk declarations plus
	// the gate's disk fallback (pre-commit) and PTR-PLAN-01's
	// register_design_documents (at commit) carry the chain.
	state["evidence"] = []any{
		evidenceIndexEntry("ev-design", "planning_design", evPath, Sha256Hex(evData), 1, "architect-1", "Architect", []any{"docs/design/architecture/ARCHITECTURE-039-loop-control-plane.md"}),
	}
	state["lifecycle"] = map[string]any{"state": "planning", "phase": "design", "phase_revision": 0}
	state["milestone"].(map[string]any)["stage"] = "S2"
	state["milestone"].(map[string]any)["lifecycle_state"] = "planning"
	state["milestone"].(map[string]any)["lifecycle_phase"] = "design"
}

// SeedAcceptanceReady seeds acceptance with ACC + clean round evidence (CT-039-15 path).
func SeedAcceptanceReady(t *testing.T, root string, state map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	runtimeID := runtimeIDFromState(state)
	write := func(id, wireKind, agent, responsibility, conclusion string) map[string]any {
		envelope := map[string]any{
			"schema_version": "1.0.0", "evidence_id": id, "kind": wireKind,
			"runtime_id": runtimeID, "baseline_generation": 1, "review_round": 1,
			"producer_agent_id": agent, "producer_responsibility": responsibility,
			"conclusion": conclusion, "created_at": "2026-07-30T00:00:00Z",
		}
		if wireKind == "acceptance" || wireKind == "release_audit" {
			manifestPath := "s10/" + wireKind + "-manifest.json"
			manifestData := s10ManifestData(t, state, wireKind, 1)
			if err := os.MkdirAll(filepath.Join(root, "s10"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(manifestPath)), manifestData, 0o644); err != nil {
				t.Fatal(err)
			}
			envelope["audit_manifest_path"] = manifestPath
			envelope["audit_manifest_sha256"] = Sha256Hex(manifestData)
		}
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		rel := writeEvidenceFile(t, root, id+".json", data)
		return evidenceIndexEntry(id, wireKind, rel, Sha256Hex(data), 1, agent, responsibility, []any{})
	}
	teams := []struct {
		id, platformID, kind, manifestRel string
		responsibility, agent             string
	}{
		{"team-delivery", "platform-delivery", "delivery_verifier", ".claude/workgroups/delivery/manifest.json", "VER-REQ", "delivery-1"},
		{"team-qa", "platform-qa", "qa", ".claude/workgroups/qa/manifest.json", "QA-QUALITY", "qa-1"},
		{"team-e2e", "platform-e2e", "e2e_browser", ".claude/workgroups/e2e/manifest.json", "E2E-USER-FLOW", "e2e-1"},
	}
	var teamEntries []any
	for _, team := range teams {
		writeWorkgroupManifest(t, root, state, team.manifestRel, team.kind, team.responsibility, team.agent)
		teamEntries = append(teamEntries, map[string]any{
			"id": team.id, "platform_team_id": team.platformID,
			"kind": team.kind, "status": "complete",
			"manifest_ref":       team.manifestRel,
			"responsibility_ids": []any{team.responsibility},
			"agent_ids":          []any{team.agent}, "review_round": 1,
		})
	}
	state["evidence"] = []any{
		write("ev-acc", "acceptance", "orchestrator-1", "Orchestrator", "pass"),
		write("ev-clean-pass", "clean_round", "orchestrator-1", "Orchestrator", "pass"),
		write("ev-delivery-pass", "delivery_review", "delivery-1", "VER-REQ", "pass"),
		write("ev-qa-pass", "qa_review", "qa-1", "QA-QUALITY", "pass"),
		write("ev-e2e-pass", "e2e_review", "e2e-1", "E2E-USER-FLOW", "pass"),
	}
	state["entities"] = map[string]any{
		"agents": []any{},
		"tasks":  []any{},
		"bugs":   []any{},
		"teams":  teamEntries,
	}
	state["lifecycle"] = map[string]any{"state": "acceptance", "phase": nil, "phase_revision": 0}
	// L3-S7: the clean_round_still_valid guard (TR-015/TR-017) recomputes the
	// machine CleanRound from the ReviewPlan projection.
	SeedCleanRoundProjection(t, root, state)
	state["milestone"].(map[string]any)["stage"] = "S10"
	state["milestone"].(map[string]any)["lifecycle_state"] = "acceptance"
}

// SeedReleaseAuditReady seeds release_audit with audit approval evidence.
func SeedReleaseAuditReady(t *testing.T, root string, state map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	runtimeID := runtimeIDFromState(state)
	write := func(id, wireKind, agent, responsibility, conclusion string) map[string]any {
		envelope := map[string]any{
			"schema_version": "1.0.0", "evidence_id": id, "kind": wireKind,
			"runtime_id": runtimeID, "baseline_generation": 1, "review_round": 1,
			"producer_agent_id": agent, "producer_responsibility": responsibility,
			"conclusion": conclusion, "created_at": "2026-07-30T00:00:00Z",
		}
		if wireKind == "acceptance" || wireKind == "release_audit" {
			manifestPath := "s10/" + wireKind + "-manifest.json"
			manifestData := s10ManifestData(t, state, wireKind, 1)
			if err := os.MkdirAll(filepath.Join(root, "s10"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(manifestPath)), manifestData, 0o644); err != nil {
				t.Fatal(err)
			}
			envelope["audit_manifest_path"] = manifestPath
			envelope["audit_manifest_sha256"] = Sha256Hex(manifestData)
		}
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		rel := writeEvidenceFile(t, root, id+".json", data)
		return evidenceIndexEntry(id, wireKind, rel, Sha256Hex(data), 1, agent, responsibility, []any{})
	}
	teams := []struct {
		id, platformID, kind, manifestRel string
		responsibility, agent             string
	}{
		{"team-delivery", "platform-delivery", "delivery_verifier", ".claude/workgroups/delivery/manifest.json", "VER-REQ", "delivery-1"},
		{"team-qa", "platform-qa", "qa", ".claude/workgroups/qa/manifest.json", "QA-QUALITY", "qa-1"},
		{"team-e2e", "platform-e2e", "e2e_browser", ".claude/workgroups/e2e/manifest.json", "E2E-USER-FLOW", "e2e-1"},
	}
	var teamEntries []any
	for _, team := range teams {
		writeWorkgroupManifest(t, root, state, team.manifestRel, team.kind, team.responsibility, team.agent)
		teamEntries = append(teamEntries, map[string]any{
			"id": team.id, "platform_team_id": team.platformID,
			"kind": team.kind, "status": "complete",
			"manifest_ref":       team.manifestRel,
			"responsibility_ids": []any{team.responsibility},
			"agent_ids":          []any{team.agent}, "review_round": 1,
		})
	}
	state["evidence"] = []any{
		write("ev-audit", "release_audit", "auditor-1", "Release Auditor", "approved"),
		write("ev-acc", "acceptance", "orchestrator-1", "Orchestrator", "pass"),
		write("ev-clean-pass", "clean_round", "orchestrator-1", "Orchestrator", "pass"),
		write("ev-delivery-pass", "delivery_review", "delivery-1", "VER-REQ", "pass"),
		write("ev-qa-pass", "qa_review", "qa-1", "QA-QUALITY", "pass"),
		write("ev-e2e-pass", "e2e_review", "e2e-1", "E2E-USER-FLOW", "pass"),
	}
	state["entities"] = map[string]any{
		"agents": []any{},
		"tasks":  []any{},
		"bugs":   []any{},
		"teams":  teamEntries,
	}
	state["lifecycle"] = map[string]any{"state": "release_audit", "phase": nil, "phase_revision": 0}
	// L3-S7: the clean_round_still_valid guard (TR-015/TR-017) recomputes the
	// machine CleanRound from the ReviewPlan projection.
	SeedCleanRoundProjection(t, root, state)
	state["milestone"].(map[string]any)["stage"] = "S10"
	state["milestone"].(map[string]any)["lifecycle_state"] = "release_audit"
}

func s10ManifestData(t *testing.T, state map[string]any, manifestType string, reviewRound int) []byte {
	t.Helper()
	items := []any{}
	counterevidence := []any{}
	for _, item := range []struct {
		id, category string
	}{
		// The S10 transition guard now consumes the same authoritative
		// denominator as Runtime/Quality Gate. Keep this fixture aligned with
		// its bound REQ and pinned ReviewPlan rather than using invented rows.
		{"REQ-039", "requirement"},
		{"CONTRACT-001", "contract"},
		{"PATH-001", "changed_path"},
		{"claim-qa-1", "claim"},
		{"AUDIT-001", "audit_area"},
	} {
		items = append(items, map[string]any{
			"id": item.id, "category": item.category, "source_refs": []string{"fixture:" + item.id},
			"expected": "fixture expected " + item.id, "oracle": "fixture oracle " + item.id,
			"owner": "S10 fixture reviewer", "evidence_refs": []string{"ev-clean-pass"},
			"disposition": "pass",
		})
		counterevidence = append(counterevidence, map[string]any{
			"id": "CE-" + item.id, "inventory_id": item.id,
			"question": "what disproves " + item.id + "?", "evidence_refs": []string{"ev-clean-pass"},
			"outcome": "pass",
		})
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_type": manifestType,
		"runtime_id": runtimeIDFromState(state), "baseline_generation": 1, "review_round": reviewRound,
		"coverage_inventory": items, "counterevidence": counterevidence,
		"risks": []any{}, "technical_debt": []any{}, "blocking_findings": []any{},
		"metrics": map[string]any{
			"requirement_coverage": 1, "contract_coverage": 1, "changed_path_coverage": 1,
			"audit_area_coverage": 1, "unknown_count": 0, "unsupported_pass_count": 0,
			"unowned_risk_count": 0, "untracked_debt_count": 0, "blocking_finding_count": 0,
		},
	}
	if manifestType == "release_audit" {
		areas := []any{}
		for _, id := range []string{"state_machine", "transaction_uow", "concurrency_idempotency", "data_migration", "call_sites_topology", "observability_errors", "verification_evidence", "docs_release_scope"} {
			areas = append(areas, map[string]any{"id": id, "conclusion": "pass", "owner": "Release Auditor", "evidence_refs": []string{"ev-clean-pass"}})
		}
		manifest["audit_areas"] = areas
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// writeWorkgroupManifest writes a readable team manifest for clean_round_still_valid.
func writeWorkgroupManifest(t *testing.T, root string, state map[string]any, rel, kind, responsibility, agent string) {
	t.Helper()
	runtimeID := RuntimeIDFromState(state)
	manifest := map[string]any{
		"schema_version":      "1.0.0",
		"manifest_id":         "team-manifest-" + kind,
		"version":             "v1.0.0",
		"runtime_id":          runtimeID,
		"req_id":              "REQ-039",
		"baseline_generation": 1,
		"review_round":        1,
		"platform_team_id":    "platform-" + kind,
		"workgroup_id":        "workgroup-" + kind,
		"workgroup_kind":      kind,
		"status":              "complete",
		"documents":           []any{},
		"risk_tags":           []any{},
		"responsibility_dispositions": []any{
			map[string]any{
				"responsibility_id": responsibility,
				"disposition":       "assigned",
				"trigger":           "fixture",
				"assignment_ids":    []any{"assignment-" + kind},
				"na_rationale":      nil,
				"evidence_ref":      nil,
			},
		},
		"assignments": []any{
			map[string]any{
				"assignment_id":        "assignment-" + kind,
				"responsibility_id":    responsibility,
				"role_family":          kind,
				"scope":                []any{"verification"},
				"agent_id":             agent,
				"agent_definition_ref": "agents/" + kind + ".md",
				"skill_refs":           []any{},
				"read_paths":           []any{},
				"write_paths":          []any{},
				"output_paths":         []any{},
				"depends_on":           []any{},
				"reuse_decision":       "create",
				"grouping_rationale":   "fixture",
				"status":               "complete",
			},
		},
		"separation_edges":    []any{},
		"planned_agent_count": 1,
		"max_parallel_agents": 1,
		"quantity_rationale":  "fixture",
		"validation": map[string]any{
			"result":                   "pass",
			"missing_responsibilities": []any{},
			"unresolved_conflicts":     []any{},
			"warnings":                 []any{},
			"validated_at":             "2026-07-30T00:00:00Z",
		},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// PreToolUseBody returns a minimal PreToolUse hook JSON body.
func PreToolUseBody(sessionID, toolName string, toolInput map[string]any) string {
	payload := map[string]any{
		"session_id": sessionID, "hook_event_name": "PreToolUse",
		"agent_id": "agent-1", "tool_name": toolName, "tool_input": toolInput,
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}
