// Package s10 supplies explicit authority for S10 consumer fixtures.
package s10

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Bind installs a real Git root and an explicit disk file view for small
// consumer tests. Tests of mixed Git/disk reads supply their own source rules.
func Bind(t *testing.T, root string, state map[string]any, reqID string, round int) {
	t.Helper()
	write := func(path string, data []byte) string {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:])
	}
	for _, args := range [][]string{{"init", "-b", "s10-fixture"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "fixture"}} {
		if data, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v: %s", err, data)
		}
	}
	commit, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	definition, err := os.ReadFile(filepath.Join(filepath.Dir(source), "../../../docs/control/loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract map[string]any
	if err := json.Unmarshal(definition, &contract); err != nil {
		t.Fatal(err)
	}
	contract["file_sources"] = []any{map[string]any{"path": ".", "source": "disk"}}
	definition, _ = json.Marshal(contract)
	write("docs/control/loop-definition.json", definition)
	sha := write("authority/REQ.md", []byte("# Required behavior\n"))
	state["bound_req"] = map[string]any{"id": reqID, "version": "v1", "status": "locked", "approved_by": "fixture-human", "approved_at": "2026-10-04T00:00:00Z", "path": "authority/REQ.md", "sha256": sha, "workspace": map[string]any{"project_root": root, "dev_branch": "s10-fixture", "release_upstream": "fixture/release", "bound_commit": strings.TrimSpace(string(commit))}}
	plan, _ := json.Marshal(map[string]any{"review_round": round, "baseline_generation": 1, "claims": []any{map[string]any{"claim_id": "claim-1"}}})
	sha = write("authority/plan.json", plan)
	state["review"].(map[string]any)["plan"] = map[string]any{"plan_id": "plan-fixture", "revision": 1, "review_round": round, "status": "clean", "e2e_coverage_state": "not_applicable", "submitted_at": "2026-10-04T00:00:00Z", "path": "authority/plan.json", "sha256": sha}
	state["review"].(map[string]any)["round"] = round
}
