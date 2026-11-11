// s2_to_s11_hook_driven_test.go — SPINE-S2-S11 L4 intent: organic Hook-driven path (FR-024/025).

package req039_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/catalog"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestS2ToS11_HookDrivenCleanPath advances S2→S11 with one Hook per stage;
// compressed seeds establish preconditions only — transitions under test come
// from Hook + evidence in-scenario (BUG-039-25 / TASK-039-09 OrganicSpine).
//
// S6 is the one stage the Hook does not own. The PreToolUse Hook evaluates
// the batch gate and names the missing canonical responsibility per TASK; the
// original TR-006 itself is committed by Main's integration exit over the
// canonical Assignment Record path.
// blueprint/l3-stages/L3-S6-build.md:417 —
// 「原 `GATE-BUILDER-BATCH-READY` 及 TR-006 使用 TR-003 的精确非空任务集，不扫描当前几份成功报告来反推分母。」
// blueprint/l3-stages/L3-S6-build.md:436 —
// 「最后一次有效集成的完成接续应检查原整批 Gate，通过原 TR-006 进入 S7。」
// blueprint/l4-mechanisms/L4-workflow-convergence.md:293 —
// 「删除 frozen／legacy 的启动、观察、接收及恢复接线，而非增加兼容层。」
func TestS2ToS11_HookDrivenCleanPath(t *testing.T) {
	root := freshRoot(t)
	runner := &req039fixtures.CLIRunner{}
	installCanonicalS6Materials(t, root)

	state := systemPlanningState(t, root, "design", 1)
	// The canonical S6 assignment path is the current Runtime contract; the
	// organic spine therefore runs on it instead of the retired legacy one.
	state["schema_version"] = "1.2.0"
	req039fixtures.SeedPlanningDesignComplete(t, root, state)
	req039fixtures.EnsureStateRoot(state, root)
	writeSystemState(t, root, state)

	const bugID = "SPINE-S2-S11"
	archEdit := map[string]any{"file_path": "docs/architecture/ARCHITECTURE-039-loop-control-plane.md"}
	bash := map[string]any{"command": "go test ./..."}

	// S2 → S3 (PTR-PLAN-01)
	req039fixtures.RequireStrictLifecycleTransition(t, runner, root, "spine-s2", "Edit", archEdit,
		"PTR-PLAN-01", "planning", "contracts", bugID)

	// S3 → S4 (PTR-PLAN-02)
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WritePlanningContractPass(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireStrictLifecycleTransition(t, runner, root, "spine-s3", "Edit",
		map[string]any{"file_path": "docs/dev/contracts/BE-039.md"},
		"PTR-PLAN-02", "planning", "tasks", bugID)

	// S4 → S5 (TR-002). The reviewed TASK also carries the waves-v1 dispatch
	// scope and the closing contract the canonical S6 preparation reads.
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WritePlanningTaskPass(t, root, state)
	writeCanonicalDispatchTask(t, root)
	writeSystemState(t, root, state)
	req039fixtures.RequireStrictLifecycleTransition(t, runner, root, "spine-s4", "Edit",
		map[string]any{"file_path": "docs/dev/tasks/TASK-039-01.md"},
		"TR-002", "document_verification", "", bugID)

	// S5 → S6 (TR-003)
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteDocumentVerificationPassEvidence(t, root, state, "dv-spec", "dv-task")
	writeSystemState(t, root, state)
	req039fixtures.RequireStrictLifecycleTransition(t, runner, root, "spine-s5", "Edit",
		map[string]any{"file_path": "docs/dev/contracts/BE-039.md"},
		"TR-003", "building", "", bugID)

	// S6, first half: the PreToolUse Hook evaluates the S6 batch gate and names
	// the missing canonical responsibility for the exact TR-003 task set. It
	// commits nothing — the Hook is the gate evaluator, not the TR-006 outlet.
	state = req039fixtures.ReadState(t, root)
	beforeLast := req039fixtures.LastTransitionID(state)
	beforeEvents := transitionJournalEvents(t, root)
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse",
		req039fixtures.MainPreToolUseBody(root, "spine-s6", "Bash", bash))
	if code != 0 && code != 2 {
		t.Fatalf("S6 gate Hook failed: code=%d stderr=%s", code, stderr)
	}
	_, qg := parseEnv(t, stdout)
	if gate, _ := qg["gate_id"].(string); gate != "GATE-BUILDER-BATCH-READY" {
		t.Fatalf("S6 must evaluate the batch gate, got %q qg=%v", gate, qg)
	}
	if cand := req039fixtures.CandidateTransition(qg); cand != "TR-006" {
		t.Fatalf("S6 candidate want TR-006, got %q qg=%v", cand, qg)
	}
	if req039fixtures.TransitionCommitted(qg) {
		t.Fatalf("S6 gate Hook must not commit TR-006, qg=%v", qg)
	}
	missing := fmt.Sprint(qg["missing"])
	if !strings.Contains(missing, "TASK-039-01") {
		t.Fatalf("S6 must name the missing canonical responsibility per TASK, got missing=%v", qg["missing"])
	}
	afterGate := req039fixtures.ReadState(t, root)
	lc, ph := req039fixtures.Lifecycle(afterGate)
	if lc != "building" || ph != "" {
		t.Fatalf("S6 must stay at building while the batch is incomplete, got %q/%q", lc, ph)
	}
	if last := req039fixtures.LastTransitionID(afterGate); last != beforeLast {
		t.Fatalf("S6 gate Hook must not record a transition, got %q want %q", last, beforeLast)
	}
	if events := transitionJournalEvents(t, root); events != beforeEvents {
		t.Fatalf("S6 gate Hook must not write a transition event, got %q want %q", events, beforeEvents)
	}

	// S6, second half: the canonical Assignment is prepared, the native Worker
	// delivers it, and Main's integration exit commits the original TR-006.
	runRecheckCLI(t, root, "s6", "prepare", "--task", "TASK-039-01", "--role", "backend-builder")
	record, found, err := assignmentrecord.ForOwner(req039fixtures.ReadState(t, root), "native-TASK-039-01")
	if err != nil || !found {
		t.Fatalf("canonical S6 preparation must register the TASK Assignment: found=%t err=%v", found, err)
	}
	worker := filepath.Join(t.TempDir(), "native-builder")
	runGitIn(t, root, "worktree", "add", "-b", "native/system-spine", worker, "HEAD")
	receiveSystemNativeBuilder(t, root, worker, record)
	code, stdout, stderr = runTaskIntegrate(t, root, record.AssignmentID)
	if code != 0 {
		t.Fatalf("Main integration exit failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	state = req039fixtures.ReadState(t, root)
	req039fixtures.AssertLifecycle(t, state, "verification", "planned")
	req039fixtures.AssertLastTransition(t, state, "TR-006")

	// S7: the ReviewPlan is registered, every required Claim consumed pass,
	// and the round consumer generated the machine CleanRound (the
	// claim-level flow is driven by the runtime verbs; the exit is one
	// hook-committed TR-009).
	state = req039fixtures.ReadState(t, root)
	req039fixtures.SeedCleanRoundReady(t, root, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireStrictLifecycleTransition(t, runner, root, "spine-s7-tr009", "Bash", bash,
		"TR-009", "acceptance", "", bugID)

	// S10 acceptance → release_audit (TR-015)
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteAcceptancePassEvidence(t, root, state)
	// The seeded S10 report is bound to the active ReviewPlan by its plan
	// identity, not by a numeric review round, so the current gate accepts it
	// as the current professional report.
	stampCurrentS10ReviewIdentity(t, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireStrictLifecycleTransition(t, runner, root, "spine-s10-acc", "Bash", bash,
		"TR-015", "release_audit", "", bugID)

	// S10 release_audit → S11 (TR-017)
	state = req039fixtures.ReadState(t, root)
	req039fixtures.WriteReleaseAuditPassEvidence(t, root, state)
	stampCurrentS10ReviewIdentity(t, state)
	writeSystemState(t, root, state)
	req039fixtures.RequireStrictLifecycleTransition(t, runner, root, "spine-s10-audit", "Bash", bash,
		"TR-017", "awaiting_human_release", "", bugID)

	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("FR-024: manual transition CLI calls = %d, want 0", runner.ManualTransitionCalls)
	}

	final := req039fixtures.ReadState(t, root)
	finalLC, finalPh := req039fixtures.Lifecycle(final)
	ms, _ := final["milestone"].(map[string]any)
	stage, _ := ms["stage"].(string)
	if finalLC != "awaiting_human_release" || stage != "S11" {
		t.Fatalf("FR-024/FR-025: did not reach S11 (lifecycle=%s/%s stage=%s)", finalLC, finalPh, stage)
	}
	assertS11TerminalStop(t, runner, root, final)
}

// installCanonicalS6Materials installs the runtime authorities and the small
// Go module the canonical S6 Worker delivery checks in its own worktree. The
// spine needs the current S6 contract, not the retired frozen one.
func installCanonicalS6Materials(t *testing.T, root string) {
	t.Helper()
	repo := repoRoot(t)
	copyTree := func(source, target string) {
		t.Helper()
		if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			dest := filepath.Join(target, rel)
			if entry.IsDir() {
				return os.MkdirAll(dest, 0755)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return err
			}
			return os.WriteFile(dest, data, 0644)
		}); err != nil {
			t.Fatal(err)
		}
	}
	copyTree(filepath.Join(repo, "docs/control/agent-protocol.md"), filepath.Join(root, "docs/control/agent-protocol.md"))
	copyTree(filepath.Join(repo, "docs/rules"), filepath.Join(root, "docs/rules"))
	copyTree(filepath.Join(repo, "agents/backend-builder.md"), filepath.Join(root, ".claude/agents/backend-builder.md"))
	for _, skill := range catalog.Skills {
		copyTree(filepath.Join(repo, "skills", skill.Name, "SKILL.md"), filepath.Join(root, ".claude/skills", skill.Name, "SKILL.md"))
	}
	writeAuditFile(t, root, "go.mod", "module system.handoff.fixture\n\ngo 1.26\n")
	writeAuditFile(t, root, "internal/input.go", "package internal\n")
	writeAuditFile(t, root, "internal/proof_test.go", "package internal\nimport (\"os\"; \"testing\")\nfunc TestReviewedDelivery(t *testing.T) { got, err := os.ReadFile(\"proof.txt\"); if err != nil || string(got) != \"actual reviewed delivery\\n\" { t.Fatalf(\"delivery: %q %v\", got, err) } }\n")
}

// writeCanonicalDispatchTask gives the reviewed TASK the waves-v1 dispatch
// scope and the reviewed dispatch plan the S4 registration records, so the
// canonical S6 preparation can read the exact registered sources.
func writeCanonicalDispatchTask(t *testing.T, root string) {
	t.Helper()
	writeAuditFile(t, root, "docs/dev/tasks/index-REQ-039.md", "# REQ-039 dispatch plan\n> REQ: REQ-039\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1\n\n- [ ] [reviewed delivery](TASK-039-01.md)\n")
	writeAuditFile(t, root, "docs/dev/tasks/TASK-039-01.md", "# TASK-039-01\n\n> Status: complete\n> Version: v1.0.2\n> Source REQ refs: REQ-039\n> Dispatch policy: waves-v1\n> Primary contract: BE-039\n\n## 3. Delivered Clauses\n\n| Contract | Delivered clauses |\n|:--|:--|\n| BE-039 | §1 |\n\n## Scope\n\n| Type | Paths |\n|---|---|\n| prospective write paths | internal |\n\n## 7. Closing Contract\n\n```text\nassert go test ./internal == pass\n```\n")
}

func assertS11TerminalStop(t *testing.T, runner *req039fixtures.CLIRunner, root string, before map[string]any) {
	t.Helper()
	beforeLC, beforePh := req039fixtures.Lifecycle(before)

	body := req039fixtures.MainPreToolUseBody(root, "s11-terminal-check", "Bash", map[string]any{"command": "ls"})
	code, stdout, stderr := runHookWithRunner(t, runner, root, "PreToolUse", body)
	if code != 0 {
		t.Fatalf("S11 terminal hook failed: code=%d stderr=%s", code, stderr)
	}
	if runner.ManualTransitionCalls != 0 {
		t.Fatalf("FR-024 at S11: manual transition calls = %d", runner.ManualTransitionCalls)
	}

	after := req039fixtures.ReadState(t, root)
	afterLC, afterPh := req039fixtures.Lifecycle(after)
	if afterLC != beforeLC || afterPh != beforePh {
		t.Fatalf("FR-025: lifecycle changed at terminal: %s/%s -> %s/%s", beforeLC, beforePh, afterLC, afterPh)
	}
	_, qg := parseEnv(t, stdout)
	if committed, _ := qg["transition_committed"].(bool); committed {
		t.Fatalf("FR-025: terminal state committed transition, qg=%v", qg)
	}
	if !strings.Contains(stdout, "release") && !strings.Contains(stdout, "human") && !strings.Contains(stdout, "Gateway") {
		t.Fatalf("FR-025: must surface human Gateway guidance at S11, got %s", stdout)
	}
}
