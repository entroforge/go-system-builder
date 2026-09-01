package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestREQBindArchivesControllerTouchedInactiveRuntimeAndStartsAtRevisionZero(t *testing.T) {
	root := newBindTestRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	state := readJSONMap(t, statePath)
	state["revision"] = float64(8)
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": float64(8),
		"last_event_id": "evt-milestone-r8",
	}
	writeJSONMap(t, statePath, state)
	if err := os.WriteFile(journalPath, validArchiveJournalForCLI("loop-inactive", 8, "evt-milestone-r8"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"req", "bind", "--root", root,
		"--req", "docs/requirements/REQ-099.md",
		"--approved-by", "release-owner",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("req bind must accept a controller-touched inactive runtime: code=%d stderr=%s", code, stderr.String())
	}
	bound := readJSONMap(t, statePath)
	if got := bound["revision"]; got != float64(0) {
		t.Fatalf("bound runtime revision = %v, want 0", got)
	}
	if got := bound["runtime_id"]; got != "loop-REQ-099" {
		t.Fatalf("bound runtime id = %v, want loop-REQ-099", got)
	}
	receipt, ok := bound["binding_receipt"].(map[string]any)
	if !ok {
		t.Fatalf("binding receipt = %#v, want object", bound["binding_receipt"])
	}
	if receipt["event"] != "req_bound" || receipt["source_runtime_id"] != "loop-inactive" || receipt["target_runtime_id"] != "loop-REQ-099" {
		t.Fatalf("binding receipt identity = %#v", receipt)
	}
	if receipt["source_revision"] != float64(8) || receipt["target_revision"] != float64(0) {
		t.Fatalf("binding receipt revisions = %#v, want source 8 target 0", receipt)
	}
	if _, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: "PTR-PLAN-01", ExpectedRevision: 8, ExpectedRuntimeID: "loop-inactive", Actor: "hook_controller",
	}); !errors.Is(err, runtime.ErrStaleRuntimeIdentity) {
		t.Fatalf("stale pre-bind snapshot error = %v, want stale runtime identity", err)
	}
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal) != 0 {
		t.Fatalf("bound runtime journal must start empty, got %q", journal)
	}
	archives, err := filepath.Glob(filepath.Join(root, ".claude", "runtime-archive", "loop-inactive-r8-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("bind archive directories = %v, want one", archives)
	}
	archivedJournal, err := os.ReadFile(filepath.Join(archives[0], "loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(archivedJournal), "evt-milestone-r8") {
		t.Fatalf("bind archive did not preserve pre-bind journal: %s", archivedJournal)
	}
}

func TestREQBindRejectsMissingFreshJournal(t *testing.T) {
	root := newBindTestRoot(t)
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"req", "bind", "--root", root,
		"--req", "docs/requirements/REQ-099.md",
		"--approved-by", "release-owner",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "fresh runtime journal is missing") {
		t.Fatalf("bind error = %q, want missing journal rejection", stderr.String())
	}
}

func TestREQBindRejectsDirtyInactiveRuntime(t *testing.T) {
	root := newBindTestRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readJSONMap(t, statePath)
	oldREQ := []byte("# REQ-037\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n")
	if err := os.WriteFile(filepath.Join(root, "docs", "requirements", "REQ-037.md"), oldREQ, 0o644); err != nil {
		t.Fatal(err)
	}
	state["revision"] = float64(8)
	state["runtime_id"] = "loop-REQ-037"
	state["bound_req"] = map[string]any{
		"id":          "REQ-037",
		"path":        "docs/requirements/REQ-037.md",
		"version":     "v1.0.0",
		"sha256":      sha256Hex(oldREQ),
		"status":      "locked",
		"approved_by": "release-owner",
		"approved_at": "2026-07-26T00:00:00Z",
		"metadata":    map[string]any{"ui_impact": "none"},
	}
	state["evidence"] = []any{rolloverApprovalEvidence("release-owner", "loop-REQ-037", 8)}
	writeJSONMap(t, statePath, state)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"req", "bind", "--root", root,
		"--req", "docs/requirements/REQ-099.md",
		"--approved-by", "release-owner",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("req bind must reject an inactive runtime containing a previous REQ")
	}
	if !strings.Contains(stderr.String(), "fresh inactive runtime") {
		t.Fatalf("dirty runtime error = %q, want fresh inactive runtime guidance", stderr.String())
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("rejected bind mutated the dirty runtime")
	}
}

func TestRuntimeRolloverArchivesTerminalRuntimeAndSeedsCleanRuntime(t *testing.T) {
	root := newBindTestRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	state := readJSONMap(t, statePath)
	state["revision"] = float64(8)
	state["runtime_id"] = "loop-REQ-037"
	state["lifecycle"] = map[string]any{"state": "release_authorized", "phase": nil, "phase_revision": float64(7)}
	state["bound_req"] = map[string]any{
		"id":          "REQ-037",
		"path":        "docs/requirements/REQ-099.md",
		"version":     "v1.0.0",
		"sha256":      sha256Hex(mustReadFile(t, filepath.Join(root, "docs", "requirements", "REQ-099.md"))),
		"status":      "locked",
		"approved_by": "release-owner",
		"approved_at": "2026-07-26T00:00:00Z",
		"metadata":    map[string]any{"ui_impact": "none"},
	}
	state["evidence"] = []any{rolloverApprovalEvidence("release-owner", "loop-REQ-037", 8)}
	state["journal"] = map[string]any{
		"path": ".claude/loop-events.jsonl", "last_sequence": float64(8), "last_event_id": "evt-old-r8",
	}
	writeJSONMap(t, statePath, state)
	if err := os.WriteFile(journalPath, validArchiveJournalForCLI("loop-REQ-037", 8, "evt-old-r8"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "rollover", "--root", root, "--approved-by", "release-owner", "--approval-evidence", "ev-rollover-approval",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runtime rollover failed: code=%d stderr=%s", code, stderr.String())
	}
	clean := readJSONMap(t, statePath)
	if got := clean["revision"]; got != float64(0) {
		t.Fatalf("fresh runtime revision = %v, want 0", got)
	}
	if got := clean["runtime_id"]; got != "loop-inactive" {
		t.Fatalf("fresh runtime id = %v, want loop-inactive", got)
	}
	if got := clean["bound_req"]; got != nil {
		t.Fatalf("fresh runtime bound_req = %#v, want nil", got)
	}
	lifecycle := clean["lifecycle"].(map[string]any)
	if lifecycle["state"] != "inactive" || lifecycle["phase"] != nil {
		t.Fatalf("fresh lifecycle = %#v, want inactive without phase", lifecycle)
	}
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal) != 0 {
		t.Fatalf("fresh journal must be empty, got %q", journal)
	}
	archives, err := filepath.Glob(filepath.Join(root, ".claude", "runtime-archive", "loop-REQ-037-r8-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("archive directories = %v, want one", archives)
	}
	archivedState, err := os.ReadFile(filepath.Join(archives[0], "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(archivedState), "loop-REQ-037") {
		t.Fatalf("archive did not preserve prior runtime: %s", archivedState)
	}
	archivedJournal, err := os.ReadFile(filepath.Join(archives[0], "loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(archivedJournal), "evt-old-r8") {
		t.Fatalf("archive did not preserve prior journal: %s", archivedJournal)
	}
}

func TestRuntimeRolloverRequiresRecordedHumanDecisionEvidence(t *testing.T) {
	root := newBindTestRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readJSONMap(t, statePath)
	state["revision"] = float64(1)
	state["runtime_id"] = "loop-REQ-037"
	state["lifecycle"] = map[string]any{"state": "aborted", "phase": nil, "phase_revision": float64(1)}
	writeJSONMap(t, statePath, state)
	before := mustReadFile(t, statePath)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "rollover", "--root", root,
		"--approved-by", "release-owner", "--approval-evidence", "ev-not-recorded",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("rollover must require recorded human decision evidence")
	}
	if !strings.Contains(stderr.String(), "must be valid human_decision evidence") {
		t.Fatalf("rollover error = %q, want approval evidence rejection", stderr.String())
	}
	if after := mustReadFile(t, statePath); string(after) != string(before) {
		t.Fatal("rejected rollover changed terminal runtime")
	}
}

func TestRuntimeRolloverRejectsApprovalForDifferentRuntime(t *testing.T) {
	root := newBindTestRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readJSONMap(t, statePath)
	state["revision"] = float64(2)
	state["runtime_id"] = "loop-REQ-037"
	state["lifecycle"] = map[string]any{"state": "aborted", "phase": nil, "phase_revision": float64(1)}
	state["evidence"] = []any{rolloverApprovalEvidence("release-owner", "loop-REQ-038", 1)}
	writeJSONMap(t, statePath, state)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "rollover", "--root", root,
		"--approved-by", "release-owner", "--approval-evidence", "ev-rollover-approval",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "runtime_rollover:loop-REQ-037") {
		t.Fatalf("rollover error = %q, want semantic runtime-scoped approval rejection", stderr.String())
	}
}

func TestRuntimeEvidenceAddExpandsCurrentRolloverScope(t *testing.T) {
	root := newBindTestRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readJSONMap(t, statePath)
	state["revision"] = float64(8)
	state["runtime_id"] = "loop-REQ-037"
	state["lifecycle"] = map[string]any{"state": "aborted", "phase": nil, "phase_revision": float64(7)}
	state["journal"] = map[string]any{
		"path": ".claude/loop-events.jsonl", "last_sequence": float64(8), "last_event_id": "evt-old-r8",
	}
	writeJSONMap(t, statePath, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), validArchiveJournalForCLI("loop-REQ-037", 8, "evt-old-r8"), 0o644); err != nil {
		t.Fatal(err)
	}
	evidencePath := "docs/reports/human/rollover-approval.md"
	if err := os.MkdirAll(filepath.Join(root, "docs", "reports", "human"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, evidencePath), []byte("# Rollover approved\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "evidence", "add", "--root", root,
		"--expected-revision", "8", "--id", "ev-rollover-approval",
		"--kind", "human_decision", "--path", evidencePath,
		"--produced-by", "release-owner", "--scope-ref", "runtime_rollover:current",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runtime evidence add failed: code=%d stderr=%s", code, stderr.String())
	}
	state = readJSONMap(t, statePath)
	evidence := state["evidence"].([]any)[0].(map[string]any)
	if got := evidence["scope_refs"].([]any); len(got) != 1 || got[0] != "runtime_rollover:loop-REQ-037" {
		t.Fatalf("rollover scope refs = %#v, want semantic runtime scope", got)
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{
		"runtime", "rollover", "--root", root,
		"--approved-by", "release-owner", "--approval-evidence", "ev-rollover-approval",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("rollover must accept the recorded approval: code=%d stderr=%s", code, stderr.String())
	}
}

func TestInitRefusesExistingRuntimeFilesWithoutChangingThem(t *testing.T) {
	root := newBindTestRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	stateBefore := mustReadFile(t, statePath)
	for _, rel := range []string{".claude/loop-events.jsonl", ".claude/hook-decisions.jsonl"} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("previous-runtime-event\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"init", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("init must refuse existing runtime files")
	}
	if !strings.Contains(stderr.String(), "refusing to initialize") {
		t.Fatalf("init error = %q, want overwrite refusal", stderr.String())
	}
	if got := mustReadFile(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("init changed existing state")
	}
	for _, rel := range []string{".claude/loop-events.jsonl", ".claude/hook-decisions.jsonl"} {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "previous-runtime-event\n" {
			t.Fatalf("%s = %q, want preserved journal", rel, data)
		}
	}
}

func TestInitCompletesPendingBootstrap(t *testing.T) {
	root := newBindTestRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	fresh := readJSONMap(t, statePath)
	for _, rel := range []string{
		".claude/loop-state.json",
		".claude/loop-events.jsonl",
		".claude/hook-decisions.jsonl",
	} {
		if err := os.Remove(filepath.Join(root, rel)); err != nil {
			t.Fatal(err)
		}
	}
	writeJSONMap(t, filepath.Join(root, ".claude", "loop-init-pending.json"), map[string]any{
		"schema_version": "1.0.0",
		"fresh_state":    fresh,
	})

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"init", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init must complete pending bootstrap: code=%d stderr=%s", code, stderr.String())
	}
	for _, rel := range []string{
		".claude/loop-state.json",
		".claude/loop-events.jsonl",
		".claude/hook-decisions.jsonl",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("pending bootstrap did not restore %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "loop-init-pending.json")); !os.IsNotExist(err) {
		t.Fatalf("pending bootstrap marker still exists: %v", err)
	}
}

func TestREQBindRecoversInterruptedRolloverBeforeBinding(t *testing.T) {
	root := newBindTestRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	fresh := readJSONMap(t, statePath)
	archiveDir := filepath.Join(root, ".claude", "runtime-archive", "loop-old-r8-test")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	archivedStateMap := cloneJSONMapForCLI(fresh)
	archivedStateMap["runtime_id"] = "loop-old"
	archivedStateMap["revision"] = float64(8)
	archivedStateMap["lifecycle"] = map[string]any{"state": "awaiting_human_release", "phase": nil, "phase_revision": float64(7)}
	archivedStateMap["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": float64(8), "last_event_id": "evt-terminal"}
	archivedStateMap["evidence"] = []any{rolloverApprovalEvidence("release-owner", "loop-old", 8)}
	archivedState := mustJSONForCLI(archivedStateMap)
	archivedJournal := validArchiveJournalForCLI("loop-old", 8, "evt-terminal")
	if err := os.WriteFile(filepath.Join(archiveDir, "loop-state.json"), archivedState, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "loop-events.jsonl"), archivedJournal, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, archivedState, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, archivedJournal, 0o644); err != nil {
		t.Fatal(err)
	}
	pending := map[string]any{
		"schema_version": "1.0.0",
		"fresh_state":    fresh,
		"record": map[string]any{
			"archive_dir":            archiveDir,
			"runtime_id":             "loop-old",
			"revision":               8,
			"archive_state_sha256":   sha256Hex(archivedState),
			"archive_journal_sha256": sha256Hex(archivedJournal),
		},
		"approval":              map[string]any{"approved_by": "release-owner", "evidence_id": "ev-rollover-approval"},
		"occurred_at":           "2026-07-26T00:00:00Z",
		"source_state_sha256":   sha256Hex(archivedState),
		"source_journal_sha256": sha256Hex(archivedJournal),
		"source_runtime_id":     "loop-old",
		"source_revision":       8,
	}
	writeJSONMap(t, statePath+".rollover-pending.json", pending)
	stateBefore := mustReadFile(t, statePath)
	journalBefore := mustReadFile(t, journalPath)
	var statusOut, statusErr bytes.Buffer
	if code := cli.Run([]string{"status", "--root", root}, strings.NewReader(""), &statusOut, &statusErr); code == 0 {
		t.Fatal("read-only status must report pending rollover instead of repairing it")
	}
	if !strings.Contains(statusErr.String(), "pending") {
		t.Fatalf("status error = %s, want pending-operation diagnostic", statusErr.String())
	}
	if got := mustReadFile(t, statePath); string(got) != string(stateBefore) {
		t.Fatal("read-only status changed state")
	}
	if got := mustReadFile(t, journalPath); string(got) != string(journalBefore) {
		t.Fatal("read-only status changed journal")
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"req", "bind", "--root", root,
		"--req", "docs/requirements/REQ-099.md",
		"--approved-by", "release-owner",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("req bind must recover pending rollover: code=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(statePath + ".rollover-pending.json"); !os.IsNotExist(err) {
		t.Fatalf("pending rollover marker still exists: %v", err)
	}
	journal := mustReadFile(t, journalPath)
	if len(journal) != 0 {
		t.Fatalf("journal = %q, want empty journal for the new bound runtime", journal)
	}
}

func rolloverApprovalEvidence(approvedBy, runtimeID string, _ int) map[string]any {
	return map[string]any{
		"id":                  "ev-rollover-approval",
		"kind":                "human_decision",
		"path":                "docs/requirements/REQ-099.md",
		"sha256":              strings.Repeat("0", 64),
		"status":              "valid",
		"baseline_generation": 0,
		"review_round":        nil,
		"produced_by":         []any{approvedBy},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   nil,
		"scope_refs":          []any{fmt.Sprintf("runtime_rollover:%s", runtimeID)},
	}
}

func newBindTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	req := "# REQ-099\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n"
	if err := os.WriteFile(filepath.Join(root, "docs", "requirements", "REQ-099.md"), []byte(req), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"init", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, stderr.String())
	}
	return root
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func writeJSONMap(t *testing.T, path string, value map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func validArchiveJournalForCLI(runtimeID string, revision int, lastEventID string) []byte {
	var lines []byte
	for sequence := 1; sequence <= revision; sequence++ {
		eventID := fmt.Sprintf("%s-r%d", lastEventID, sequence)
		if sequence == revision {
			eventID = lastEventID
		}
		event := map[string]any{
			"schema_version": "1.0.0", "runtime_id": runtimeID, "event_id": eventID,
			"sequence": sequence, "event": "milestone_refreshed", "outcome": "refreshed",
			"actor": map[string]any{"type": "system", "id": "archive-test"}, "request_id": "archive-test",
			"baseline_generation": 1, "before_revision": sequence - 1, "after_revision": sequence,
			"from": map[string]any{"state": "inactive", "phase": nil}, "to": map[string]any{"state": "inactive", "phase": nil},
			"evidence_ids": []any{}, "message": "Archived test event.", "occurred_at": "2026-08-13T00:00:00Z",
		}
		lines = append(lines, mustJSONLineForCLI(event)...)
	}
	return lines
}

func mustJSONLineForCLI(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}

func cloneJSONMapForCLI(value map[string]any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(data, &clone); err != nil {
		panic(err)
	}
	return clone
}

func mustJSONForCLI(value any) []byte {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}
