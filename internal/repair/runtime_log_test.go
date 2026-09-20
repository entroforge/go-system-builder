package repair_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// RC-16 (S9-L1) regression coverage. The defect: the S9 file scan classified a
// git-ignored runtime log (server/log/<date>/info.log, appended to by the
// project's long-running service and by ordinary verification runs) as session
// authority. A single appended log line then staled the authority fingerprint
// of every open RepairSession and blocked result submission, and the same file
// appeared in the Session diff as an unreported change that no contract scope
// could ever claim.
//
// The classification that replaces it is deliberately narrow: a path is runtime
// log output only when its *name* is a log name (or an allowlisted log-output
// name inside a log directory) AND Git reports it ignored, which also means
// untracked. Ignored-and-untracked alone never decides — every counterexample
// below is ignored and untracked and still stays in the baseline.

// legacySessionPointer opens a normal RepairSession and then rewrites its
// stored baseline to include runtime log output — the on-disk shape of every
// session opened before the capture-side classification. The Runtime pointer is
// re-aimed at the rewritten session so the exported gate can be probed without
// touching the Runtime state machine.
func legacySessionPointer(t *testing.T, root, statePath, logRel, logSHA string) map[string]any {
	t.Helper()
	stateRaw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateRaw, &state); err != nil {
		t.Fatal(err)
	}
	pointer := state["review"].(map[string]any)["repair"].(map[string]any)
	sessionPath := filepath.Join(root, filepath.FromSlash(pointer["path"].(string)))
	sessionRaw, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	var session map[string]any
	if err := json.Unmarshal(sessionRaw, &session); err != nil {
		t.Fatal(err)
	}
	artifacts, ok := session["baseline_artifacts"].([]any)
	if !ok {
		t.Fatalf("session baseline_artifacts = %#v", session["baseline_artifacts"])
	}
	session["baseline_artifacts"] = append(artifacts, map[string]any{
		"id": "baseline-" + strings.NewReplacer("/", "-", ".", "-").Replace(logRel), "path": logRel, "sha256": logSHA, "status": "modified",
	})
	rewritten, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	rewritten = append(rewritten, '\n')
	if err := os.WriteFile(sessionPath, rewritten, 0o644); err != nil {
		t.Fatal(err)
	}
	pointer["sha256"] = fileHash(rewritten)
	return pointer
}

// TestRuntimeLogAppendDoesNotStaleAuthorityAndRealDriftStillBlocks covers: a log
// append never blocks an open session (even one whose baseline already captured
// the log, i.e. without a rebuild), while a genuine out-of-scope code change in
// the same session still fails closed.
func TestRuntimeLogAppendDoesNotStaleAuthorityAndRealDriftStillBlocks(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	runGit(t, root, "init")
	writeFile(t, root, ".gitignore", "log/\nlogs/\n*.log\n")
	logRel := "server/log/2026-09-17/info.log"
	logPath := writeFile(t, root, logRel, "2026-09-17 00:00:00 info server/task/alert_check.go:19 boot\n")
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRef, contractSHA := writeRuntimeContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRef.Path, "repair_contract_sha256": contractSHA}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	if _, _, _, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "main"}, SessionID: "repair-session-runtime-log", CreatedBy: "main"}); err != nil {
		t.Fatal(err)
	}
	captured, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	pointer := legacySessionPointer(t, root, statePath, logRel, fileHash(captured))

	// The long-running service appends its cron heartbeat to a log the session's
	// baseline already lists. This is runtime exhaust, not an implementation
	// change, so the gate stays green without re-baselining.
	if err := os.WriteFile(logPath, append(captured, []byte("2026-09-17 08:00:00 info server/task/alert_check.go:19 Alert检查任务完成 {\"triggered\": 0, \"failed\": 0}\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repair.ValidateAuthorityFreshness(root, pointer, nil); err != nil {
		t.Fatalf("a runtime log append must not stale the authority fingerprint: %v", err)
	}

	// An unclaimed, out-of-scope code change in the same session is still
	// authority drift — the exemption above must not have widened the gate.
	definitionPath := filepath.Join(root, "docs", "loop-definition.json")
	definition, err := os.ReadFile(definitionPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(definitionPath, append(definition, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(other, []byte("log/\nlogs/\n*.log\n# changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	err = repair.ValidateAuthorityFreshness(root, pointer, nil)
	if err == nil || !strings.Contains(err.Error(), "authority fingerprint is stale") {
		t.Fatalf("out-of-scope code drift must still block, got %v", err)
	}
	if !strings.Contains(err.Error(), "docs/loop-definition.json") || !strings.Contains(err.Error(), ".gitignore") {
		t.Fatalf("stale-fingerprint error must name the drifted code path, got %v", err)
	}
}

// TestSessionChangesetExcludesRuntimeLogsAndKeepsRealChanges covers the diff
// side against a legacy session object: the diff must report the repair's own
// artifact with its current digest, drop the appended runtime log (which would
// otherwise be an unclaimable "unreported change"), and keep a tracked log
// fixture that the repository deliberately version-controls.
func TestSessionChangesetExcludesRuntimeLogsAndKeepsRealChanges(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	gitignorePath := writeFile(t, root, ".gitignore", "log/\n*.log\n")
	fixtureRel := "testdata/golden.log"
	fixturePath := writeFile(t, root, fixtureRel, "expected fixture output\n")
	runGit(t, root, "add", "-f", fixtureRel)
	logRel := "server/log/2026-09-17/info.log"
	logPath := writeFile(t, root, logRel, "boot\n")
	productRel := "internal/api/payload.go"
	productPath := writeFile(t, root, productRel, "package api\n\nfunc Fixed() {}\n")

	baseline := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return fileHash(data)
	}
	session := repair.RepairSession{SessionID: "repair-session-legacy-changeset", BaselineArtifacts: []repair.ArtifactRef{
		{Path: ".gitignore", SHA256: baseline(gitignorePath), Status: "modified"},
		{Path: productRel, SHA256: baseline(productPath), Status: "modified"},
		{Path: logRel, SHA256: baseline(logPath), Status: "modified"},
		{Path: fixtureRel, SHA256: baseline(fixturePath), Status: "modified"},
	}}
	// The repair edits the product file, the service appends to the log, and the
	// versioned fixture is deliberately updated by the repair.
	if err := os.WriteFile(productPath, []byte("package api\n\nfunc Fixed() { /* v2 */ }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("boot\n2026-09-17 08:00:00 heartbeat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, []byte("expected fixture output v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changeset, err := repair.ComputeSessionChangeset(root, session)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, artifact := range changeset {
		byPath[artifact.Path] = artifact.SHA256
	}
	if len(changeset) != 2 {
		t.Fatalf("session diff = %#v, want exactly the product change and the versioned fixture", changeset)
	}
	if _, exists := byPath[logRel]; exists {
		t.Fatalf("runtime log %s must not appear in the session diff: %#v", logRel, changeset)
	}
	productData, err := os.ReadFile(productPath)
	if err != nil {
		t.Fatal(err)
	}
	if byPath[productRel] != fileHash(productData) {
		t.Fatalf("product change digest = %s, want current bytes %s", byPath[productRel], fileHash(productData))
	}
	fixtureData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if byPath[fixtureRel] != fileHash(fixtureData) {
		t.Fatalf("tracked log fixture digest = %s, want current bytes %s — a versioned log must stay authority-relevant", byPath[fixtureRel], fileHash(fixtureData))
	}
}

// TestCaptureBaselineClassifiesOnlyNamedLogOutput is the classification matrix.
// Every path below is git-ignored and untracked (or tracked on purpose); the
// ignored-and-untracked ones that are not *named* like log output must survive
// into the baseline, so that a local source copy, a Go file whose name merely
// contains ".log", or a stray config file can never be silenced as exhaust.
func TestCaptureBaselineClassifiesOnlyNamedLogOutput(t *testing.T) {
	cases := []struct {
		rel      string
		contents string
		tracked  bool
		want     bool // want = must appear in the captured baseline
		reason   string
	}{
		{"internal/api/payload.go", "package api\n", true, true, "ordinary product source"},
		{"server/log/2026-09-17/info.log", "boot\n", false, false, "named log output in a log directory"},
		{"server/log/2026-09-17/info.log.1", "boot\n", false, false, "rotated log"},
		{"server/log/2026-09-17/info.log.2026-09-16.gz", "boot\n", false, false, "date-rotated, compressed log"},
		{"logs/app.out", "boot\n", false, false, "allowlisted log-output name in a log directory"},
		{"logs/stdout", "boot\n", false, false, "extension-free log-output name in a log directory"},
		{"server/log/audit.logic.go", "package audit\n", false, true, "non-log file whose name resembles .log"},
		{"server/log/debug_helper.go", "package log\n", false, true, "untracked source copy inside a log directory"},
		{"server/log/pipeline.json", "{\"stage\": 1}\n", false, true, "configuration file inside a log directory"},
		{"server/log/README.md", "# notes\n", false, true, "documentation inside a log directory"},
		{"testdata/golden.log", "expected output\n", true, true, "tracked log fixture — Git never reports an indexed path as ignored"},
		{"internal/log/logger.go", "package log\n", true, true, "tracked source in a directory named log"},
	}
	capture := func(t *testing.T, gitBacked bool) map[string]bool {
		t.Helper()
		root := t.TempDir()
		if gitBacked {
			runGit(t, root, "init")
			writeFile(t, root, ".gitignore", "log/\nlogs/\n*.log\n")
		}
		for _, testCase := range cases {
			writeFile(t, root, testCase.rel, testCase.contents)
		}
		if gitBacked {
			for _, testCase := range cases {
				if testCase.tracked {
					runGit(t, root, "add", "-f", testCase.rel)
				}
			}
		}
		contractRef, _ := writeRuntimeContract(t, root)
		session, _, err := repair.CreateRepairSession(root, repair.SessionRequest{
			Contract: contractRef, SessionID: "repair-session-capture", RuntimeID: "loop-REQ-039",
			ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
		})
		if err != nil {
			t.Fatal(err)
		}
		paths := map[string]bool{}
		for _, artifact := range session.BaselineArtifacts {
			paths[artifact.Path] = true
		}
		return paths
	}

	tracked := capture(t, true)
	for _, testCase := range cases {
		if tracked[testCase.rel] != testCase.want {
			t.Fatalf("baseline contains %s = %v, want %v (%s)", testCase.rel, tracked[testCase.rel], testCase.want, testCase.reason)
		}
	}
	// A repository Git cannot classify keeps the strict default: nothing is
	// presumed to be exhaust on shape alone.
	withoutGit := capture(t, false)
	for _, testCase := range cases {
		if !withoutGit[testCase.rel] {
			t.Fatalf("without Git classification %s must stay in the baseline (%s)", testCase.rel, testCase.reason)
		}
	}
}
