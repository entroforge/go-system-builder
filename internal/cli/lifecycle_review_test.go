package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// Review-driven coverage: the scenarios the E2E chain and unit tests left
// open (third-party review findings, 2026-08-15).

func bindForReview(t *testing.T, root string) {
	bindForReviewReq(t, root, "")
}

func bindForReviewReq(t *testing.T, root, req string) {
	t.Helper()
	args := []string{"req", "bind", "--root", root, "--approved-by", "alice"}
	if req != "" {
		args = append(args, "--req", req)
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("bind failed: %s", stderr.String())
	}
}

// TestUnbindAllowedFromPaused pins the L2 ruling "any non-terminal state":
// revoking from a paused checkpoint is a legitimate abandonment, and the
// archive keeps the checkpoint as part of the audit trail.
func TestUnbindAllowedFromPaused(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-200.md": "# REQ-200\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n"})
	bindForReview(t, root)
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"runtime", "pause", "--root", root, "--reason", "reconsidering", "--approved-by", "alice"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("pause failed: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code := cli.Run([]string{"req", "unbind", "--root", root, "--approved-by", "alice", "--reason", "abandon from checkpoint"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unbind from paused must be allowed (L2: any non-terminal state): %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "unbound REQ-200") {
		t.Fatalf("unbind output: %s", stdout.String())
	}
	// The archived runtime keeps the pause checkpoint for audit.
	entries, _ := os.ReadDir(filepath.Join(root, ".claude", "runtime-archive"))
	if len(entries) == 0 {
		t.Fatal("unbound archive missing")
	}
	data, err := os.ReadFile(filepath.Join(root, ".claude", "runtime-archive", entries[0].Name(), "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var archived map[string]any
	if err := json.Unmarshal(data, &archived); err != nil {
		t.Fatal(err)
	}
	if archived["pause"] == nil {
		t.Fatal("archive must retain the pause checkpoint of the abandoned period")
	}
}

// TestUnbindForceRecordsInFlight pins the soft gate: open entities block,
// --force proceeds and the archive carries the visible abandonment.
func TestUnbindForceRecordsInFlight(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-201.md": "# REQ-201\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n"})
	bindForReview(t, root)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readJSONMap(t, statePath)
	state["entities"] = map[string]any{
		"agents": []any{},
		"bugs":   []any{},
		"teams":  []any{},
		"tasks": []any{map[string]any{
			"id":              "TASK-001",
			"state":           "in_progress",
			"path":            "docs/tasks/TASK-001.md",
			"sha256":          "0000000000000000000000000000000000000000000000000000000000000000",
			"owner_agent_ids": []any{},
		}},
	}
	writeJSONMap(t, statePath, state)

	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"req", "unbind", "--root", root, "--approved-by", "alice", "--reason", "abandon"}, strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatal("in-flight tasks must block unbind without --force")
	}
	if !strings.Contains(stderr.String(), "TASK-001") || !strings.Contains(stderr.String(), "--force") {
		t.Fatalf("soft gate must list entities and the override: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{"req", "unbind", "--root", root, "--approved-by", "alice", "--reason", "visible abandonment", "--force"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("forced unbind failed: %s", stderr.String())
	}
	entries, _ := os.ReadDir(filepath.Join(root, ".claude", "runtime-archive"))
	data, err := os.ReadFile(filepath.Join(root, ".claude", "runtime-archive", entries[0].Name(), "rollover.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["forced"] != true {
		t.Fatalf("manifest must record forced=true, got %v", manifest["forced"])
	}
	inFlight, _ := manifest["in_flight_entities"].([]any)
	if len(inFlight) == 0 || !strings.Contains(strings.Join(flattenAny(inFlight), " "), "TASK-001") {
		t.Fatalf("manifest must record the abandoned in-flight entities, got %v", manifest["in_flight_entities"])
	}
}

func flattenAny(items []any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, strings.TrimSpace(item.(string)))
	}
	return out
}

// TestUnbindReceiptCannotAuthorizeRollover pins approval-scope isolation at
// the CLI level: a terminal runtime rejects an unbind-scoped approval.
func TestUnbindReceiptCannotAuthorizeRollover(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-202.md": "# REQ-202\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n"})
	bindForReview(t, root)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readJSONMap(t, statePath)
	state["lifecycle"] = map[string]any{"state": "aborted", "phase": nil, "phase_revision": float64(3)}
	state["revision"] = float64(4)
	decision := filepath.Join(root, ".claude", "decisions", "unbind.json")
	os.MkdirAll(filepath.Dir(decision), 0o755)
	os.WriteFile(decision, []byte(`{"decision":"req_unbind_requested","approved_by":"alice"}`), 0o644)
	state["evidence"] = []any{map[string]any{
		"id": "ev-unbind", "kind": "human_decision", "status": "valid",
		"baseline_generation": float64(1), "review_round": nil,
		"path": ".claude/decisions/unbind.json", "sha256": "placeholder",
		"produced_by": []any{"alice"}, "scope_refs": []any{"runtime_unbind:loop-REQ-202@4"},
		"invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil,
		"responsibility_id": nil,
	}}
	// Give the evidence a real fingerprint so validation reaches the scope check.
	data, _ := os.ReadFile(decision)
	state["evidence"].([]any)[0].(map[string]any)["sha256"] = fmt.Sprintf("%x", sha256.Sum256(data))
	writeJSONMap(t, statePath, state)

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "rollover", "--root", root,
		"--approved-by", "alice", "--approval-evidence", "ev-unbind"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("an unbind-scoped receipt must not authorize a rollover")
	}
	if !strings.Contains(stderr.String(), "runtime_rollover") {
		t.Fatalf("rejection must name the required scope, got: %s", stderr.String())
	}
}

// TestResumeRefusedOnBaselineDriftRoutesToAmend pins the drift branch.
func TestResumeRefusedOnBaselineDriftRoutesToAmend(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-203.md": "# REQ-203\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n"})
	bindForReview(t, root)
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"runtime", "pause", "--root", root, "--reason", "x", "--approved-by", "alice"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("pause failed: code=%d stderr=%s", code, stderr.String())
	}
	decisionFiles, err := filepath.Glob(filepath.Join(root, ".claude", "decisions", "*.json"))
	if err != nil || len(decisionFiles) != 1 {
		t.Fatalf("pause decision artifact = %v, err=%v; want one artifact", decisionFiles, err)
	}
	var decision map[string]any
	decisionData, err := os.ReadFile(decisionFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(decisionData, &decision); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"revision", "authorization_revision"} {
		if _, present := decision[field]; present {
			t.Fatalf("human decision artifact must not carry Runtime %s: %#v", field, decision)
		}
	}
	// Drift: modify the locked REQ while paused (no hook in this test harness).
	os.WriteFile(filepath.Join(root, "docs", "requirements", "REQ-203.md"),
		[]byte("# REQ-203\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n\ndrifted\n"), 0o644)
	stdout.Reset()
	stderr.Reset()
	code := cli.Run([]string{"runtime", "resume", "--root", root, "--approved-by", "alice"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("resume must refuse on baseline drift")
	}
	if !strings.Contains(stderr.String(), "req amend") {
		t.Fatalf("drift refusal must route to amendment, got: %s", stderr.String())
	}
}

// TestREQListJSONShape pins the --json contract for scripts.
func TestREQListJSONShape(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{
		"REQ-204.md": "# REQ-204\n\n> 状态：locked\n> 版本：v2.0.0\n> UI impact：none\n",
	})
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"req", "list", "--root", root, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("req list --json failed: %s", stderr.String())
	}
	var summaries []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &summaries); err != nil {
		t.Fatalf("json output invalid: %v", err)
	}
	if len(summaries) != 1 || summaries[0]["id"] != "REQ-204" || summaries[0]["bindable"] != true {
		t.Fatalf("unexpected summaries: %v", summaries)
	}
}

// TestAmendRejectsDifferentREQ pins the same-identity rule: changing the
// target is an unbind+rebind, not an amendment.
func TestAmendRejectsDifferentREQ(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{
		"REQ-205.md": "# REQ-205\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n",
		"REQ-206.md": "# REQ-206\n\n> 状态：locked\n> 版本：v9.0.0\n> UI impact：none\n",
	})
	bindForReviewReq(t, root, "docs/requirements/REQ-205.md")
	var stdout, stderr bytes.Buffer
	cli.Run([]string{"runtime", "pause", "--root", root, "--reason", "x", "--approved-by", "alice"}, strings.NewReader(""), &stdout, &stderr)
	stdout.Reset()
	stderr.Reset()
	code := cli.Run([]string{"req", "amend", "--root", root, "--req", "docs/requirements/REQ-206.md", "--approved-by", "alice"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("amending to a different REQ id must be rejected")
	}
	if !strings.Contains(stderr.String(), "unbind") {
		t.Fatalf("rejection must route to unbind+rebind, got: %s", stderr.String())
	}
}

// TestAmendRejectsNonNumericVersion pins the version format gate.
func TestAmendRejectsNonNumericVersion(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-207.md": "# REQ-207\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n"})
	bindForReview(t, root)
	var stdout, stderr bytes.Buffer
	cli.Run([]string{"runtime", "pause", "--root", root, "--reason", "x", "--approved-by", "alice"}, strings.NewReader(""), &stdout, &stderr)
	os.WriteFile(filepath.Join(root, "docs", "requirements", "REQ-207.md"),
		[]byte("# REQ-207\n\n> 状态：locked\n> 版本：draft2\n> UI impact：none\n"), 0o644)
	stdout.Reset()
	stderr.Reset()
	code := cli.Run([]string{"req", "amend", "--root", root, "--req", "docs/requirements/REQ-207.md", "--approved-by", "alice"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("non-numeric version must be rejected")
	}
	if !strings.Contains(stderr.String(), "dotted numeric") {
		t.Fatalf("rejection must name the format, got: %s", stderr.String())
	}
}

// TestRolloverArchivesCRLFREQ pins the CRLF handling of the archival flip
// through the real rollover path: the trailing CR must survive so the
// file's line endings stay exactly as the author left them.
func TestRolloverArchivesCRLFREQ(t *testing.T) {
	reqBody := "# REQ-208\r\n\r\n> 状态：locked\r\n> 版本：v1.0.0\r\n> UI impact：none\r\n"
	root := newUXTestRoot(t, map[string]string{"REQ-208.md": reqBody})
	bindForReviewReq(t, root, "docs/requirements/REQ-208.md")
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	state := readJSONMap(t, statePath)
	state["lifecycle"] = map[string]any{"state": "release_authorized", "phase": nil, "phase_revision": float64(3)}
	decision := filepath.Join(root, ".claude", "decisions", "rollover.json")
	os.MkdirAll(filepath.Dir(decision), 0o755)
	decisionBody := []byte(`{"decision":"runtime_rollover","approved_by":"alice"}`)
	os.WriteFile(decision, decisionBody, 0o644)
	state["evidence"] = []any{map[string]any{
		"id": "hd-rollover", "kind": "human_decision", "status": "valid",
		"baseline_generation": float64(1), "review_round": nil,
		"path":           ".claude/decisions/rollover.json",
		"sha256":         fmt.Sprintf("%x", sha256.Sum256(decisionBody)),
		"produced_by":    []any{"alice"},
		"scope_refs":     []any{"runtime_rollover:loop-REQ-208@0"},
		"invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil,
		"responsibility_id": nil,
	}}
	writeJSONMap(t, statePath, state)
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"runtime", "rollover", "--root", root,
		"--approved-by", "alice", "--approval-evidence", "hd-rollover"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("rollover failed: %s", stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(root, "docs", "requirements", "REQ-208.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "# REQ-208\r\n\r\n> 状态：archived\r\n> 版本：v1.0.0\r\n> UI impact：none\r\n"
	if string(data) != want {
		t.Fatalf("archived REQ = %q, want %q", string(data), want)
	}
}

// TestBindPreflightsControlPlaneDrift pins the preflight: a valid-but-
// changed loop-definition must be refused instead of silently burning a
// stale fingerprint into a fresh baseline.
func TestBindPreflightsControlPlaneDrift(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-209.md": "# REQ-209\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n"})
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"init", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init failed: %s", stderr.String())
	}
	defPath := filepath.Join(root, "docs", "loop-definition.json")
	data, err := os.ReadFile(defPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defPath, append(data, []byte("\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code := cli.Run([]string{"req", "bind", "--root", root, "--approved-by", "alice"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("bind must refuse a drifted control plane")
	}
	if !strings.Contains(stderr.String(), "control plane drifted") || !strings.Contains(stderr.String(), "doctor") {
		t.Fatalf("drift refusal must route to doctor, got: %s", stderr.String())
	}
}
