package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// TestLifecycleVerbChainE2E walks the complete REQ authorization lifecycle
// through the CLI surface: enter → pause → resume → pause → amend →
// unbind → rebind → pause → abort → rollover (REQ archived) → next period.
// Each verb's human output, the durable state, and the archive receipts are
// asserted. This is the L2「REQ 授权生命周期」seven-verb table's executable
// counterpart.
func TestLifecycleVerbChainE2E(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{
		"REQ-100.md": "# REQ-100\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n",
	})
	run := func(args ...string) (string, string, int) {
		var stdout, stderr bytes.Buffer
		code := cli.Run(args, strings.NewReader(""), &stdout, &stderr)
		return stdout.String(), stderr.String(), code
	}
	must := func(what string, args ...string) string {
		stdout, stderr, code := run(args...)
		if code != 0 {
			t.Fatalf("%s failed: code=%d stderr=%s stdout=%s", what, code, stderr, stdout)
		}
		return stdout
	}
	stateOf := func() map[string]any {
		data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
		if err != nil {
			t.Fatal(err)
		}
		var state map[string]any
		if err := json.Unmarshal(data, &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	lifecycle := func() (string, any) {
		lc, _ := stateOf()["lifecycle"].(map[string]any)
		state, _ := lc["state"].(string)
		return state, lc["phase"]
	}

	// --- enter: projection hint, then bind (auto-init + discovery) ---
	must("init", "init", "--root", root)
	next := must("next", "next", "--root", root)
	if !strings.Contains(next, "bind the human-locked REQ") ||
		!strings.Contains(next, "req bind --req docs/requirements/REQ-100.md") {
		t.Fatalf("S0 projection must carry the ready-to-run bind command: %s", next)
	}
	out := must("bind", "req", "bind", "--root", root, "--approved-by", "alice")
	if !strings.Contains(out, "bound REQ-100 v1.0.0") || !strings.Contains(out, "event req_bound") {
		t.Fatalf("bind output missing facts: %s", out)
	}
	if state, phase := lifecycle(); state != "planning" || phase != "design" {
		t.Fatalf("post-bind lifecycle = %s.%v, want planning.design", state, phase)
	}

	// --- pause → resume (checkpoint verified and cleared) ---
	out = must("pause", "runtime", "pause", "--root", root, "--reason", "external dependency", "--approved-by", "alice")
	if !strings.Contains(out, "paused from planning.design") {
		t.Fatalf("pause output: %s", out)
	}
	if state, _ := lifecycle(); state != "paused" {
		t.Fatalf("lifecycle after pause = %s", state)
	}
	if _, _, code := run("req", "bind", "--root", root, "--approved-by", "alice"); code == 0 {
		t.Fatal("bind must refuse on a paused runtime")
	}
	out = must("resume", "runtime", "resume", "--root", root, "--approved-by", "alice")
	if !strings.Contains(out, "resumed to planning.design") {
		t.Fatalf("resume output: %s", out)
	}
	if pause := stateOf()["pause"]; pause != nil {
		t.Fatalf("pause checkpoint must be cleared after resume, got %v", pause)
	}

	// --- pause again (residue fix: second pause after a resume works) → amend ---
	must("pause-again", "runtime", "pause", "--root", root, "--reason", "requirement change", "--approved-by", "alice")
	amended := "# REQ-100\n\n> 状态：locked\n> 版本：v1.1.0\n> UI impact：none\n\n> Added AC-3.\n"
	if err := os.WriteFile(filepath.Join(root, "docs", "requirements", "REQ-100.md"), []byte(amended), 0o644); err != nil {
		t.Fatal(err)
	}
	out = must("amend", "req", "amend", "--root", root, "--req", "docs/requirements/REQ-100.md", "--approved-by", "alice")
	if !strings.Contains(out, "REQ-100 v1.1.0 (baseline generation 2") {
		t.Fatalf("amend output: %s", out)
	}
	state := stateOf()
	bound, _ := state["bound_req"].(map[string]any)
	if bound["version"] != "v1.1.0" {
		t.Fatalf("bound version after amend = %v, want v1.1.0", bound["version"])
	}
	if pause := state["pause"]; pause != nil {
		t.Fatalf("amend must clear the pause checkpoint, got %v", pause)
	}
	if state["lifecycle"].(map[string]any)["state"] != "planning" {
		t.Fatal("amend must land the runtime back in planning")
	}

	// --- unbind: archive disposition=unbound, REQ returns to the pool ---
	out = must("unbind", "req", "unbind", "--root", root, "--approved-by", "alice", "--reason", "wrong target, doing REQ-101 first")
	if !strings.Contains(out, "unbound REQ-100") || !strings.Contains(out, "disposition=unbound") {
		t.Fatalf("unbind output: %s", out)
	}
	listing := must("list-after-unbind", "req", "list", "--root", root)
	if !strings.Contains(listing, "REQ-100") || !strings.Contains(listing, "bindable") {
		t.Fatalf("unbound REQ must return to the bindable pool: %s", listing)
	}

	// --- rebind the same REQ (fresh runtime) ---
	out = must("rebind", "req", "bind", "--root", root, "--approved-by", "alice")
	if !strings.Contains(out, "bound REQ-100 v1.1.0") {
		t.Fatalf("rebind output: %s", out)
	}

	// --- abandon via pause → abort (TR-021) ---
	must("pause-for-abort", "runtime", "pause", "--root", root, "--reason", "abandoning this requirement", "--approved-by", "alice")
	abortDecision := filepath.Join(root, ".claude", "decisions", "abort.json")
	if err := os.MkdirAll(filepath.Dir(abortDecision), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abortDecision, []byte(`{"decision":"human_abort_approved","approved_by":"alice"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := stateOf()
	revision := int(snapshot["revision"].(float64))
	must("abort-evidence", "runtime", "evidence", "add", "--root", root,
		"--expected-revision", itoa(revision), "--id", "hd-abort",
		"--kind", "human_decision", "--path", ".claude/decisions/abort.json",
		"--produced-by", "alice",
		"--scope-ref", fmt.Sprintf("runtime_abort:%s@%d", snapshot["runtime_id"], revision+1))
	must("abort", "runtime", "transition", "--root", root,
		"--id", "TR-021", "--expected-revision", itoa(revision+1), "--actor", "user",
		"--evidence", "human_decision_record=hd-abort")
	if state, _ := lifecycle(); state != "aborted" {
		t.Fatalf("lifecycle after TR-021 = %s, want aborted", state)
	}

	// --- rollover: REQ archived with dual fingerprints, fresh runtime ---
	snapshot = stateOf()
	revision = int(snapshot["revision"].(float64))
	rolloverDecision := filepath.Join(root, ".claude", "decisions", "rollover.json")
	if err := os.WriteFile(rolloverDecision, []byte(`{"decision":"runtime_rollover","approved_by":"alice"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	must("rollover-evidence", "runtime", "evidence", "add", "--root", root,
		"--expected-revision", itoa(revision), "--id", "hd-rollover",
		"--kind", "human_decision", "--path", ".claude/decisions/rollover.json",
		"--produced-by", "alice", "--scope-ref", "runtime_rollover:current")
	out = must("rollover", "runtime", "rollover", "--root", root,
		"--approved-by", "alice", "--approval-evidence", "hd-rollover")
	if !strings.Contains(out, "runtime rolled over") || !strings.Contains(out, "REQ-100 status locked → archived") {
		t.Fatalf("rollover output: %s", out)
	}
	reqData, err := os.ReadFile(filepath.Join(root, "docs", "requirements", "REQ-100.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(reqData), "> 状态：archived") {
		t.Fatalf("REQ file must flip to archived, got:\n%s", string(reqData))
	}
	archiveDirs, err := os.ReadDir(filepath.Join(root, ".claude", "runtime-archive"))
	if err != nil || len(archiveDirs) == 0 {
		t.Fatalf("runtime archive missing: %v", err)
	}
	var receipt []byte
	for _, entry := range archiveDirs {
		data, err := os.ReadFile(filepath.Join(root, ".claude", "runtime-archive", entry.Name(), "req-archive.json"))
		if err == nil {
			receipt = data
			break
		}
	}
	if receipt == nil {
		t.Fatal("req-archive receipt missing in every archive dir")
	}
	var receiptMap map[string]any
	if err := json.Unmarshal(receipt, &receiptMap); err != nil {
		t.Fatal(err)
	}
	reqRecord, _ := receiptMap["req"].(map[string]any)
	if reqRecord["sha256_before"] == reqRecord["sha256_after"] {
		t.Fatal("receipt must carry distinct before/after fingerprints")
	}
	if state, _ := lifecycle(); state != "inactive" {
		t.Fatalf("post-rollover lifecycle = %s, want inactive", state)
	}
	listing = must("list-after-rollover", "req", "list", "--root", root)
	if !strings.Contains(listing, "lifecycle closed") {
		t.Fatalf("closed REQ must be excluded from bindable: %s", listing)
	}

	// --- next period: bind REQ-101 ---
	if err := os.WriteFile(filepath.Join(root, "docs", "requirements", "REQ-101.md"),
		[]byte("# REQ-101\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = must("bind-next-period", "req", "bind", "--root", root, "--approved-by", "alice")
	if !strings.Contains(out, "bound REQ-101 v1.0.0") {
		t.Fatalf("next-period bind output: %s", out)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
