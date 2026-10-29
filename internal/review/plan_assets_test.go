package review

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestPlanAssetsReadPinnedDeclaredSources(t *testing.T) {
	root, raw := planAssetFixture(t)
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "source")
	commit := git("rev-parse", "HEAD")
	declaration := []byte(`{"file_sources":[{"path":".","source":"git_tree"}]}`)
	if err := os.WriteFile(filepath.Join(root, "definition.json"), declaration, 0600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"definition": map[string]any{"path": "definition.json", "sha256": sha256Of(declaration)}}
	raw["product_baseline"] = map[string]any{"final_commit": commit}
	if err := os.WriteFile(filepath.Join(root, "browser.spec.ts"), []byte("unapproved workspace bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(raw)
	prepared, err := preparePlanAssets(root, body, state)
	if err != nil {
		t.Fatal(err)
	}
	var p Plan
	if err := json.Unmarshal(prepared, &p); err != nil {
		t.Fatal(err)
	}
	if p.E2EAssets[0].SHA256 != sha256Of([]byte("actual preexisting script")) {
		t.Fatal("machine completion certified workspace bytes instead of the pinned declared source")
	}
	if err := os.WriteFile(filepath.Join(root, "definition.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := preparePlanAssets(root, body, state); err == nil {
		t.Fatal("changed source authority accepted")
	}
}

func planAssetFixture(t *testing.T) (string, map[string]any) {
	t.Helper()
	root := t.TempDir()
	for _, m := range []string{"alpha", "beta"} {
		dir := filepath.Join(root, "docs/design/prototypes", m)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cases.json"), []byte(`{"cases":[{"id":"CASE-001","required":true,"browser_required":true}]}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "browser.spec.ts"), []byte("actual preexisting script"), 0600); err != nil {
		t.Fatal(err)
	}
	body, err := schema.ReadAsset("review-plan.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	raw["e2e_assets"] = []any{map[string]any{"case_ref": "docs/design/prototypes/alpha/cases.json#CASE-001", "path": "browser.spec.ts", "selector_ref": "actual-selector", "route_ref": "/actual-alpha", "environment": "existing-browser"}}
	claims := raw["claims"].([]any)
	claims = append(claims, map[string]any{"claim_id": "claim-exact-case", "lens": "e2e", "source_refs": []any{"docs/design/prototypes/alpha/cases.json#CASE-001", "docs/design/prototypes/beta/cases.json#CASE-001"}})
	raw["claims"] = claims
	return root, raw
}
func TestPlanAssetsFillMachineFactsAndReplayWithoutRefreshingSources(t *testing.T) {
	root, raw := planAssetFixture(t)
	authored, _ := json.Marshal(raw)
	accepted, err := preparePlanAssets(root, authored, nil)
	if err != nil {
		t.Fatal(err)
	}
	var plan Plan
	if err := json.Unmarshal(accepted, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.E2EAssets[0].AssetID == "" || plan.E2EAssets[0].SHA256 != sha256Of([]byte("actual preexisting script")) {
		t.Fatalf("wrong original source: %+v", plan.E2EAssets)
	}
	if err := os.WriteFile(filepath.Join(root, "browser.spec.ts"), []byte("later drift"), 0600); err != nil {
		t.Fatal(err)
	}
	if !SamePlanAnswer(authored, accepted) {
		t.Fatal("original answer replay depended on today's source")
	}
	raw["e2e_assets"].([]any)[0].(map[string]any)["route_ref"] = "/changed-professional-choice"
	changed, _ := json.Marshal(raw)
	if SamePlanAnswer(changed, accepted) {
		t.Fatal("changed professional choice was accepted as replay")
	}
	if _, err := preparePlanAssets(root, accepted, nil); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("explicit old digest was silently refreshed: %v", err)
	}
}
func TestPlanAssetsRejectAmbiguousSourcesAndPreserveUnknownFields(t *testing.T) {
	root, raw := planAssetFixture(t)
	asset := raw["e2e_assets"].([]any)[0].(map[string]any)
	asset["case_ref"] = "CASE-001"
	data, _ := json.Marshal(raw)
	if _, err := preparePlanAssets(root, data, nil); err == nil || !strings.Contains(err.Error(), "2 matches") {
		t.Fatalf("ambiguous bare CASE was guessed: %v", err)
	}
	asset["case_ref"] = "docs/design/prototypes/alpha/cases.json#CASE-001"
	asset["unrecognized_author_field"] = true
	data, _ = json.Marshal(raw)
	prepared, err := preparePlanAssets(root, data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prepared), "unrecognized_author_field") {
		t.Fatal("preparation discarded a field before schema rejection")
	}
	if _, err := preparePlanAssets(root, []byte(`{"e2e_assets":[],"e2e_assets":[]}`), nil); err == nil {
		t.Fatal("duplicate JSON field accepted")
	}
	asset["case_ref"] = "../../outside/cases.json#CASE-001"
	data, _ = json.Marshal(raw)
	if _, err := preparePlanAssets(root, data, nil); err == nil {
		t.Fatal("escaping scenario source accepted")
	}
}
