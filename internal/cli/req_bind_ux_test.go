package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// newUXTestRoot prepares a repository root with definition/policy assets and
// the given REQ files, but does NOT initialize the runtime — the bind UX
// under test performs auto-init itself.
func newUXTestRoot(t *testing.T, reqs map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs/control"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"docs/control/loop-definition.json", "docs/control/hook-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range reqs {
		if err := os.WriteFile(filepath.Join(root, "docs", "requirements", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-qb", "feature/req"}, {"add", "docs"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "stage output"}} {
		if out, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); e != nil {
			t.Fatalf("git: %s %v", out, e)
		}
	}
	return root
}

const lockedReqBody = "# REQ-098\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n"

func TestREQListClassifiesStatusesAndSkipsTemplate(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{
		"REQ-097.md":      "# REQ-097\n\n> 状态：draft\n> 版本：v0.1.0\n> UI impact：unknown\n",
		"REQ-098.md":      lockedReqBody,
		"REQ-template.md": "# template\n\n> 状态：draft\n> 版本：v0.1.0\n> UI impact：none\n",
	})
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"req", "list", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("req list failed: code=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "REQ-template") {
		t.Fatalf("req list must skip templates, got %q", out)
	}
	if !strings.Contains(out, "REQ-097") || !strings.Contains(out, "not bindable: status is draft") {
		t.Fatalf("req list missing draft classification: %q", out)
	}
	if !strings.Contains(out, "REQ-098") || !strings.Contains(out, "bindable") {
		t.Fatalf("req list missing locked classification: %q", out)
	}
	if !strings.Contains(out, "loop-harness req bind --req docs/requirements/REQ-098.md") {
		t.Fatalf("req list must print the ready-to-bind command: %q", out)
	}
}

func TestREQBindAutoInitsAndDiscoversSoleLockedREQ(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-098.md": lockedReqBody})
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"req", "bind", "--root", root, "--dev-branch", "feature/req", "--release-upstream", "origin/main", "--approved-by", "ux-owner"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("req bind failed: code=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"initialized fresh runtime at .claude/loop-state.json",
		"discovered sole bindable REQ: docs/requirements/REQ-098.md",
		"bound REQ-098 v1.0.0 (ui_impact=none)",
		"approved-by ux-owner",
		"cursor planning.design",
		"generation 1",
		"next: S2 design",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("bind output missing %q, got:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "loop-state.json")); err != nil {
		t.Fatalf("runtime not created: %v", err)
	}
}

func TestREQBindMultipleCandidatesRequireExplicitReq(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{
		"REQ-098.md": lockedReqBody,
		"REQ-099.md": "# REQ-099\n\n> 状态：locked\n> 版本：v1.2.0\n> UI impact：none\n",
	})
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"req", "bind", "--root", root, "--dev-branch", "feature/req", "--release-upstream", "origin/main", "--approved-by", "ux-owner"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("multiple candidates must exit 2, got %d", code)
	}
	errOut := stderr.String()
	if !strings.Contains(errOut, "uniqueness is a human decision") ||
		!strings.Contains(errOut, "docs/requirements/REQ-098.md") ||
		!strings.Contains(errOut, "docs/requirements/REQ-099.md") {
		t.Fatalf("ambiguous-candidate guidance incomplete: %q", errOut)
	}
}

func TestREQBindNoBindableREQGuidesBackToS0(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{
		"REQ-097.md": "# REQ-097\n\n> 状态：draft\n> 版本：v0.1.0\n> UI impact：unknown\n",
	})
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"req", "bind", "--root", root, "--dev-branch", "feature/req", "--release-upstream", "origin/main", "--approved-by", "ux-owner"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("no bindable REQ must exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "no bindable REQ") || !strings.Contains(stderr.String(), "REQ-097") {
		t.Fatalf("missing guidance: %q", stderr.String())
	}
}

func TestREQBindApprovedByHint(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-098.md": lockedReqBody})
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"req", "bind", "--root", root, "--dev-branch", "feature/req", "--release-upstream", "origin/main"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("missing --approved-by must exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "requires --approved-by") {
		t.Fatalf("missing identity guidance: %q", stderr.String())
	}
}

func TestREQBindJSONFlagReturnsState(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-098.md": lockedReqBody})
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"req", "bind", "--root", root, "--dev-branch", "feature/req", "--release-upstream", "origin/main", "--approved-by", "ux-owner", "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("json bind failed: code=%d stderr=%s", code, stderr.String())
	}
	var state map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &state); err != nil {
		t.Fatalf("--json output not valid JSON: %v", err)
	}
	bound, _ := state["bound_req"].(map[string]any)
	if bound["id"] != "REQ-098" {
		t.Fatalf("json state missing bound_req id: %v", bound)
	}
}

func TestREQListExcludesTerminatedArchiveAndAutoDiscoverySkipsIt(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{
		"REQ-097.md": lockedReqBody, // rename body id mismatch is fine: filename drives ID
	})
	// Reuse REQ-097 as the closed one and add a fresh open one.
	if err := os.WriteFile(filepath.Join(root, "docs", "requirements", "REQ-096.md"),
		[]byte("# REQ-096\n\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	archived := map[string]any{
		"lifecycle": map[string]any{"state": "release_authorized", "phase": nil},
		"bound_req": map[string]any{"id": "REQ-097"},
	}
	data, err := json.Marshal(archived)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".claude", "runtime-archive", "loop-REQ-097-r9-20260815T0000Z")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loop-state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"req", "list", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("req list failed: %s", stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "lifecycle closed (archived runtime)") {
		t.Fatalf("archived REQ not annotated: %q", out)
	}
	if !strings.Contains(out, "loop-harness req bind --req docs/requirements/REQ-096.md") {
		t.Fatalf("ready-to-bind must point at the open REQ only: %q", out)
	}
}

// TestREQBindAlreadyBoundRoutesToAmendOrUnbind verifies that re-binding
// while a REQ is actively bound must name the two legal routes instead of
// the raw TR-001 source-state rejection.
func TestREQBindAlreadyBoundRoutesToAmendOrUnbind(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-098.md": lockedReqBody, "REQ-099.md": lockedReqBody})
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"req", "bind", "--root", root, "--dev-branch", "feature/req", "--release-upstream", "origin/main", "--req", "docs/requirements/REQ-098.md", "--approved-by", "ux-owner"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("first bind failed: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code := cli.Run([]string{"req", "bind", "--root", root, "--dev-branch", "feature/req", "--release-upstream", "origin/main", "--req", "docs/requirements/REQ-099.md", "--approved-by", "ux-owner"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "already bound") || !strings.Contains(stderr.String(), "req amend") || !strings.Contains(stderr.String(), "req unbind") {
		t.Fatalf("rebind must route to amend/unbind, got: %s", stderr.String())
	}
}

func TestREQBindingUsesCommittedBytesAndExplicitDestinations(t *testing.T) {
	root := newUXTestRoot(t, map[string]string{"REQ-098.md": lockedReqBody})
	if e := os.WriteFile(filepath.Join(root, "docs/requirements/REQ-098.md"), []byte(strings.Replace(lockedReqBody, "locked", "draft", 1)), 0644); e != nil {
		t.Fatal(e)
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"req", "bind", "--root", root, "--approved-by", "owner"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("implicit branch accepted: %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{"req", "bind", "--root", root, "--approved-by", "owner", "--dev-branch", "feature/req", "--release-upstream", "origin/release", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("bind: %d %s", code, stderr.String())
	}
	var state map[string]any
	if e := json.Unmarshal(stdout.Bytes(), &state); e != nil {
		t.Fatal(e)
	}
	binding := state["bound_req"].(map[string]any)["workspace"].(map[string]any)
	if binding["dev_branch"] != "feature/req" || binding["release_upstream"] != "origin/release" || binding["bound_commit"] == "" {
		t.Fatalf("lost binding: %v", binding)
	}
}

func TestExplicitLegacyStateCannotBeReconciled(t *testing.T) {
	for _, legacy := range []string{"definition", "bound_req"} {
		t.Run(legacy, func(t *testing.T) {
			root := newUXTestRoot(t, nil)
			var out, errOut bytes.Buffer
			if code := cli.Run([]string{"init", "--root", root}, strings.NewReader(""), &out, &errOut); code != 0 {
				t.Fatal(errOut.String())
			}
			original := filepath.Join(root, ".claude/loop-state.json")
			data, err := os.ReadFile(original)
			if err != nil {
				t.Fatal(err)
			}
			var state map[string]any
			if err := json.Unmarshal(data, &state); err != nil {
				t.Fatal(err)
			}
			if legacy == "definition" {
				state["definition"].(map[string]any)["path"] = "docs/loop-definition.json"
			} else {
				state["bound_req"] = map[string]any{"path": "docs/product/requirements/REQ-001.md"}
			}
			state["hook_control"].(map[string]any)["policy_ref"].(map[string]any)["sha256"] = strings.Repeat("0", 64)
			data, err = json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			external := t.TempDir()
			statePath := filepath.Join(external, "old-state.json")
			journalPath := filepath.Join(external, "old-events.jsonl")
			if err := os.WriteFile(statePath, data, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(root, ".claude/loop-events.jsonl"), journalPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(original); err != nil {
				t.Fatal(err)
			}
			beforeJournal, _ := os.ReadFile(journalPath)
			out.Reset()
			errOut.Reset()
			code := cli.Run([]string{"runtime", "reconcile-policy-ref", "--root", root, "--state", statePath, "--journal", journalPath}, strings.NewReader(""), &out, &errOut)
			if code == 0 || !strings.Contains(errOut.String(), "layout migration required") {
				t.Fatalf("old explicit Runtime accepted: %d %s %s", code, out.String(), errOut.String())
			}
			after, err := os.ReadFile(statePath)
			if err != nil || !bytes.Equal(after, data) {
				t.Fatal("legacy state changed", err)
			}
			afterJournal, err := os.ReadFile(journalPath)
			if err != nil || !bytes.Equal(afterJournal, beforeJournal) {
				t.Fatal("legacy journal changed", err)
			}
		})
	}
}

func TestReadOnlyCommandsValidateExplicitRuntimeLayout(t *testing.T) {
	root := newUXTestRoot(t, nil)
	for _, legacy := range []bool{false, true} {
		reqPath := "docs/requirements/REQ-001.md"
		if legacy {
			reqPath = "docs/product/requirements/REQ-001.md"
		}
		data := []byte(`{"bound_req":{"path":"` + reqPath + `"},"evidence":[]}`)
		for _, absolute := range []bool{false, true} {
			statePath := "external-state.json"
			actual := filepath.Join(root, statePath)
			if absolute {
				actual = filepath.Join(t.TempDir(), "external-state.json")
				statePath = actual
			}
			if err := os.WriteFile(actual, data, 0644); err != nil {
				t.Fatal(err)
			}
			for _, command := range [][]string{{"impact", "analyze", "--changed", "docs/requirements/REQ-001.md"}, {"verification", "clean-round"}, {"explain", "TR-001"}} {
				var out, errOut bytes.Buffer
				args := append(append([]string{}, command...), "--root", root, "--state", statePath)
				code := cli.Run(args, strings.NewReader(""), &out, &errOut)
				if legacy {
					if code == 0 || !strings.Contains(errOut.String(), "layout migration required") {
						t.Fatalf("legacy state accepted by %v: %d %s %s", args, code, out.String(), errOut.String())
					}
				} else {
					if errOut.Len() != 0 || out.Len() == 0 {
						t.Fatalf("current state unreadable by %v: %d %s %s", args, code, out.String(), errOut.String())
					}
					if command[0] != "verification" && code != 0 {
						t.Fatalf("current diagnostic failed: %v %d", args, code)
					}
				}
				after, err := os.ReadFile(actual)
				if err != nil || !bytes.Equal(after, data) {
					t.Fatalf("read-only command changed state: %v", args)
				}
			}
		}
	}
}
