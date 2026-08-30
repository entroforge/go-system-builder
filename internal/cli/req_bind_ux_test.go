package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
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
	for _, rel := range []string{"docs/loop-definition.json", "docs/hook-policy.json"} {
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
	code := cli.Run([]string{"req", "bind", "--root", root, "--approved-by", "ux-owner"}, strings.NewReader(""), &stdout, &stderr)
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
	code := cli.Run([]string{"req", "bind", "--root", root, "--approved-by", "ux-owner"}, strings.NewReader(""), &stdout, &stderr)
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
	code := cli.Run([]string{"req", "bind", "--root", root, "--approved-by", "ux-owner"}, strings.NewReader(""), &stdout, &stderr)
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
	code := cli.Run([]string{"req", "bind", "--root", root}, strings.NewReader(""), &stdout, &stderr)
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
	code := cli.Run([]string{"req", "bind", "--root", root, "--approved-by", "ux-owner", "--json"}, strings.NewReader(""), &stdout, &stderr)
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
	if code := cli.Run([]string{"req", "bind", "--root", root, "--req", "docs/requirements/REQ-098.md", "--approved-by", "ux-owner"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("first bind failed: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code := cli.Run([]string{"req", "bind", "--root", root, "--req", "docs/requirements/REQ-099.md", "--approved-by", "ux-owner"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "already bound") || !strings.Contains(stderr.String(), "req amend") || !strings.Contains(stderr.String(), "req unbind") {
		t.Fatalf("rebind must route to amend/unbind, got: %s", stderr.String())
	}
}
