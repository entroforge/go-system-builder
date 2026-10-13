package repair_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// RC-17 (S9-L2) regression coverage. The defect: .claude/multi.json, the local
// session launcher's environment/profile file (model and connection settings),
// was captured into session authority baselines. The launcher rewrites it on
// every session start or profile switch, out-of-band and by design; the harness
// never reads it. A single profile switch then staled the authority fingerprint
// of every open RepairSession and, worse, surfaced in the Session diff as an
// unclaimable phantom change once capture stopped recording it.
//
// The classification is a single exact path added to the control-plane set —
// the same class as .claude/loop-state.json — covering capture, the authority
// gate, and the Session diff uniformly. It does not widen to .claude/ as a
// whole: any other .claude file stays authority-relevant, as the negative
// control below proves.

const multiJSONRel = ".claude/multi.json"

// TestMultiJSONProfileSwitchDoesNotStaleAuthority covers the production RC-17
// case: a session whose stored baseline carries .claude/multi.json must survive
// the launcher rewriting that file (a model/profile switch) without
// re-baselining, while drift of any *other* .claude file in the same session
// still fails closed.
func TestMultiJSONProfileSwitchDoesNotStaleAuthority(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	runGit(t, root, "init")
	multiPath := writeFile(t, root, multiJSONRel, "{\n  \"env\": {\n    \"ANTHROPIC_MODEL\": \"profile-a\"\n  }\n}\n")
	otherRel := ".claude/other-local.json"
	otherPath := writeFile(t, root, otherRel, "{\"note\": \"not exempt\"}\n")
	productRel := "internal/api/payload.go"
	writeFile(t, root, productRel, "package api\n\nfunc Fixed() {}\n")
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRef, contractSHA := writeRuntimeContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRef.Path, "repair_contract_sha256": contractSHA}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	if _, _, _, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "main"}, SessionID: "repair-session-multi-json", CreatedBy: "main"}); err != nil {
		t.Fatal(err)
	}
	captured, err := os.ReadFile(multiPath)
	if err != nil {
		t.Fatal(err)
	}
	pointer := legacySessionPointer(t, root, statePath, multiJSONRel, fileHash(captured))

	// The launcher rewrites the profile file at the next session start. This is
	// local control-plane state, not an implementation change, so the gate
	// stays green without re-baselining — for a legacy session whose baseline
	// already lists the file.
	if err := os.WriteFile(multiPath, []byte("{\n  \"env\": {\n    \"ANTHROPIC_MODEL\": \"profile-b\"\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repair.ValidateAuthorityFreshness(root, pointer, nil); err != nil {
		t.Fatalf("a launcher profile switch must not stale the authority fingerprint: %v", err)
	}

	// Drift of any other .claude file is still authority drift — the exemption
	// names exactly one file and must not have widened to the directory.
	otherData, err := os.ReadFile(otherPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, append(otherData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	err = repair.ValidateAuthorityFreshness(root, pointer, nil)
	if err == nil || !strings.Contains(err.Error(), "authority fingerprint is stale") {
		t.Fatalf("drift of a non-exempt .claude file must still block, got %v", err)
	}
	if !strings.Contains(err.Error(), otherRel) {
		t.Fatalf("stale-fingerprint error must name the drifted path %s, got %v", otherRel, err)
	}
}

// TestSessionChangesetExcludesMultiJSON covers the diff side against a legacy
// session object: once capture classifies .claude/multi.json out, the stored
// baseline copy must not reappear in the diff as a phantom "deleted" (or, while
// capture still recorded it, as an unclaimable "modified"). A real product
// change in the same session must still be reported with its current digest.
func TestSessionChangesetExcludesMultiJSON(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	multiPath := writeFile(t, root, multiJSONRel, "{\n  \"env\": {\n    \"ANTHROPIC_MODEL\": \"profile-a\"\n  }\n}\n")
	productRel := "internal/api/payload.go"
	productPath := writeFile(t, root, productRel, "package api\n\nfunc Fixed() {}\n")

	baseline := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return fileHash(data)
	}
	session := repair.RepairSession{SessionID: "repair-session-legacy-multi-json", BaselineArtifacts: []repair.ArtifactRef{
		{Path: multiJSONRel, SHA256: baseline(multiPath), Status: "modified"},
		{Path: productRel, SHA256: baseline(productPath), Status: "modified"},
	}}
	// The launcher switches the profile; the repair edits the product file.
	if err := os.WriteFile(multiPath, []byte("{\n  \"env\": {\n    \"ANTHROPIC_MODEL\": \"profile-b\"\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(productPath, []byte("package api\n\nfunc Fixed() { /* v2 */ }\n"), 0o644); err != nil {
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
	if _, exists := byPath[multiJSONRel]; exists {
		t.Fatalf("launcher profile file %s must not appear in the session diff: %#v", multiJSONRel, changeset)
	}
	productData, err := os.ReadFile(productPath)
	if err != nil {
		t.Fatal(err)
	}
	if byPath[productRel] != fileHash(productData) {
		t.Fatalf("product change digest = %s, want current bytes %s", byPath[productRel], fileHash(productData))
	}
	if len(changeset) != 1 {
		t.Fatalf("session diff = %#v, want exactly the product change", changeset)
	}
}

// TestCaptureBaselineExcludesMultiJSON covers the capture side: a fresh session
// baseline never records the launcher profile file, while ordinary product
// source and non-exempt .claude files still enter it.
func TestCaptureBaselineExcludesMultiJSON(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	writeFile(t, root, multiJSONRel, "{\n  \"env\": {\n    \"ANTHROPIC_MODEL\": \"profile-a\"\n  }\n}\n")
	writeFile(t, root, "internal/api/payload.go", "package api\n")
	writeFile(t, root, ".claude/other-local.json", "{\"note\": \"not exempt\"}\n")
	contractRef, _ := writeRuntimeContract(t, root)
	session, _, err := repair.CreateRepairSession(root, repair.SessionRequest{
		Contract: contractRef, SessionID: "repair-session-capture-multi-json", RuntimeID: "loop-REQ-039",
		ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, artifact := range session.BaselineArtifacts {
		paths[artifact.Path] = true
	}
	if paths[multiJSONRel] {
		t.Fatalf("fresh baseline must not capture the launcher profile file %s", multiJSONRel)
	}
	if !paths["internal/api/payload.go"] {
		t.Fatal("fresh baseline must still capture ordinary product source")
	}
	if !paths[".claude/other-local.json"] {
		t.Fatal("fresh baseline must still capture non-exempt .claude files — the exemption names exactly one file")
	}
}

// Append to internal/repair/control_plane_config_test.go (framework repo B).
// Regression coverage for the .claude/hook-notices/ authority-fingerprint
// exemption (RC-17 class, directory-scoped).

// TestHookNoticesRewriteDoesNotStaleAuthority covers the S9 execution-phase
// incident class: .claude/hook-notices/ dedup stamps are rewritten by the
// progress hook whenever the gate state moves (gate_id/status transitions
// while builders work). A legacy session whose baseline captured those stamps
// must survive the rewrite without re-baselining, while drift of any other
// .claude file still fails closed.
func TestHookNoticesRewriteDoesNotStaleAuthority(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	runGit(t, root, "init")
	noticeRel := ".claude/hook-notices/0000stale"
	noticeStamp := "1111111111111111111111111111111111111111111111111111111111111111"
	noticePath := writeFile(t, root, noticeRel, noticeStamp)
	otherRel := ".claude/other-local.json"
	otherPath := writeFile(t, root, otherRel, "{\"note\": \"not exempt\"}\n")
	writeFile(t, root, "internal/api/payload.go", "package api\n\nfunc Fixed() {}\n")
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRef, contractSHA := writeRuntimeContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRef.Path, "repair_contract_sha256": contractSHA}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl")
	if _, _, _, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "main"}, SessionID: "repair-session-hook-notices", CreatedBy: "main"}); err != nil {
		t.Fatal(err)
	}
	pointer := legacySessionPointer(t, root, statePath, noticeRel, fileHash([]byte(noticeStamp)))

	// The progress hook rewrites the stamp when the gate state moves. That is
	// harness bookkeeping, not an implementation change: the authority gate
	// must stay green for a legacy baseline that already lists the stamp.
	if err := os.WriteFile(noticePath, []byte("2222222222222222222222222222222222222222222222222222222222222222"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repair.ValidateAuthorityFreshness(root, pointer, nil); err != nil {
		t.Fatalf("a hook-notice stamp rewrite must not stale the authority fingerprint: %v", err)
	}

	// Drift of any other .claude file is still authority drift.
	otherData, err := os.ReadFile(otherPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, append(otherData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	err = repair.ValidateAuthorityFreshness(root, pointer, nil)
	if err == nil || !strings.Contains(err.Error(), "authority fingerprint is stale") {
		t.Fatalf("drift of a non-exempt .claude file must still block, got %v", err)
	}
}

// TestFreshCaptureSkipsHookNotices covers the capture side: new sessions must
// not record hook-notice stamps at all, so later rewrites cannot create
// unclaimable phantom changes in the Session diff.
func TestFreshCaptureSkipsHookNotices(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	runGit(t, root, "init")
	writeFile(t, root, ".claude/hook-notices/0000fresh", "abcd")
	writeFile(t, root, ".claude/other-local.json", "{\"note\": \"captured\"}\n")
	writeFile(t, root, "internal/api/payload.go", "package api\n\nfunc Fixed() {}\n")
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRef, contractSHA := writeRuntimeContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRef.Path, "repair_contract_sha256": contractSHA}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	session, _, err := repair.CreateRepairSession(root, repair.SessionRequest{Contract: contractRef, SessionID: "repair-session-capture-hook-notices", RuntimeID: "loop-REQ-039", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, artifact := range session.BaselineArtifacts {
		paths[artifact.Path] = true
	}
	if paths[".claude/hook-notices/0000fresh"] {
		t.Fatal("fresh baseline must not capture hook-notice stamps")
	}
	if !paths["internal/api/payload.go"] {
		t.Fatal("fresh baseline must still capture ordinary product source")
	}
	if !paths[".claude/other-local.json"] {
		t.Fatal("fresh baseline must still capture non-exempt .claude files")
	}
}

// Append to internal/repair/control_plane_config_test.go (framework repo B).
// Regression coverage for the .claude/hook-metrics/ authority-fingerprint
// exemption (RC-17 class, directory-scoped), including the compaction
// (deletion) incident shape observed in the field.

// TestHookMetricsCompactionDoesNotStaleAuthority covers the S9 execution-phase
// incident class: .claude/hook-metrics/.pending-*.json scratch buffers are
// rewritten on every hook evaluation and later removed by metrics compaction
// once the owning sessions end. A legacy session whose baseline captured those
// buffers must survive both the rewrite and the compaction removal without
// re-baselining, while drift of any other .claude file still fails closed.
func TestHookMetricsCompactionDoesNotStaleAuthority(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	runGit(t, root, "init")
	pendingRel := ".claude/hook-metrics/.pending-9433f38da020.json"
	pendingPath := writeFile(t, root, pendingRel, "{\"events\": 1}\n")
	summaryRel := ".claude/hook-metrics/summary.json"
	otherRel := ".claude/other-local.json"
	otherPath := writeFile(t, root, otherRel, "{\"note\": \"not exempt\"}\n")
	writeFile(t, root, "internal/api/payload.go", "package api\n\nfunc Fixed() {}\n")
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRef, contractSHA := writeRuntimeContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRef.Path, "repair_contract_sha256": contractSHA}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
	if _, _, _, err := repair.OpenRepairSession(root, statePath, journalPath, repair.OpenSessionRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 0, Actor: "main"}, SessionID: "repair-session-hook-metrics", CreatedBy: "main"}); err != nil {
		t.Fatal(err)
	}
	pointer := legacySessionPointer(t, root, statePath, pendingRel, fileHash([]byte("{\"events\": 1}\n")))

	// A hook event rewrites the pending buffer. That is harness bookkeeping,
	// not an implementation change: the authority gate must stay green for a
	// legacy baseline that already lists the buffer.
	if err := os.WriteFile(pendingPath, []byte("{\"events\": 2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repair.ValidateAuthorityFreshness(root, pointer, nil); err != nil {
		t.Fatalf("a hook-metrics buffer rewrite must not stale the authority fingerprint: %v", err)
	}

	// Compaction removes the pending buffers and leaves a summary. Deleting a
	// captured scratch buffer is the field incident (753 drifted artifacts
	// blocking every RepairResult submission) and must not stale authority.
	if err := os.Remove(pendingPath); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, summaryRel, "{\"compacted\": true}\n")
	if err := repair.ValidateAuthorityFreshness(root, pointer, nil); err != nil {
		t.Fatalf("hook-metrics compaction must not stale the authority fingerprint: %v", err)
	}

	// Drift of any other .claude file is still authority drift.
	otherData, err := os.ReadFile(otherPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, append(otherData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	err = repair.ValidateAuthorityFreshness(root, pointer, nil)
	if err == nil || !strings.Contains(err.Error(), "authority fingerprint is stale") {
		t.Fatalf("drift of a non-exempt .claude file must still block, got %v", err)
	}
}

// TestFreshCaptureSkipsHookMetrics covers the capture side: new sessions must
// not record hook-metrics scratch (pending buffers or the compaction summary)
// at all, so later rewrites and compaction cannot create unclaimable phantom
// changes in the Session diff.
func TestFreshCaptureSkipsHookMetrics(t *testing.T) {
	root := req039fixtures.FreshRoot(t)
	runGit(t, root, "init")
	writeFile(t, root, ".claude/hook-metrics/.pending-0001fresh", "{}\n")
	writeFile(t, root, ".claude/hook-metrics/summary.json", "{\"compacted\": true}\n")
	writeFile(t, root, ".claude/other-local.json", "{\"note\": \"captured\"}\n")
	writeFile(t, root, "internal/api/payload.go", "package api\n\nfunc Fixed() {}\n")
	state := req039fixtures.BaseState(t, root, "bug_resolution", "repair_readback", 0)
	contractRef, contractSHA := writeRuntimeContract(t, root)
	state["review"].(map[string]any)["investigation"] = map[string]any{"case_id": "investigation-case-1", "path": ".claude/review/investigation/cases/investigation-case-1-r2.json", "sha256": repeatHex("b", 64), "revision": 2, "status": "contract_approved", "source_finding_ids": []any{"finding-1"}, "observation_batch_id": "observation-batch-1", "updated_at": "2026-08-25T00:00:00Z", "repair_contract_ref": contractRef.Path, "repair_contract_sha256": contractSHA}
	req039fixtures.WriteState(t, root, state)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	session, _, err := repair.CreateRepairSession(root, repair.SessionRequest{Contract: contractRef, SessionID: "repair-session-capture-hook-metrics", RuntimeID: "loop-REQ-039", ReqID: "REQ-039", BaselineGeneration: 1, CreatedBy: "main"})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, artifact := range session.BaselineArtifacts {
		paths[artifact.Path] = true
	}
	if paths[".claude/hook-metrics/.pending-0001fresh"] {
		t.Fatal("fresh baseline must not capture hook-metrics pending buffers")
	}
	if paths[".claude/hook-metrics/summary.json"] {
		t.Fatal("fresh baseline must not capture the hook-metrics compaction summary")
	}
	if !paths["internal/api/payload.go"] {
		t.Fatal("fresh baseline must still capture ordinary product source")
	}
	if !paths[".claude/other-local.json"] {
		t.Fatal("fresh baseline must still capture non-exempt .claude files")
	}
}
