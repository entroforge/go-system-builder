package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS9QuestionnaireUsesSavedOriginalPathWithoutDigest(t *testing.T) {
	root := t.TempDir()
	sessionID := "repair-session-custom-" + strings.Repeat("long", 100)
	original := repair.QuestionnaireRoot("runtime-A", sessionID)
	state := map[string]any{"review": map[string]any{"repair": map[string]any{"session_id": sessionID, "questionnaire_root": original, "plan_ref": "plan-A.json"}}}
	name := repairTargetedQuestionnaireName("assignment-" + strings.Repeat("long", 100))
	path, err := repairLeadQuestionnairePath(root, state, name)
	if err != nil {
		t.Fatal(err)
	}
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if len(component) > 200 {
			t.Fatalf("unbounded path component %d", len(component))
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"result":"pass"}`), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := repairLeadQuestionnairePath(root, state, name)
	if err != nil || second != path {
		t.Fatalf("original location changed: %s %v", second, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	state["review"].(map[string]any)["repair"].(map[string]any)["plan_ref"] = "plan-B.json"
	next, err := repairLeadQuestionnairePath(root, state, name)
	if err != nil || next == path {
		t.Fatalf("new plan reused original answer: %s %v", next, err)
	}
}

func TestS9LegacyQuestionnaireWithoutSavedPathRemainsUnknown(t *testing.T) {
	root := t.TempDir()
	for _, historical := range []string{"", strings.Repeat("a", 64)} {
		state := map[string]any{"review": map[string]any{"repair": map[string]any{"session_id": "repair-session-old", "plan_sha256": historical}}}
		if _, err := repairLeadQuestionnairePath(root, state, "change-impact.json"); err == nil || !strings.Contains(err.Error(), "not retained") {
			t.Fatalf("missing path should remain unknown independently of old hash: %v", err)
		}
	}
}

func TestS9ReadbackIdentityUsesOriginalResponsibilityNotDigest(t *testing.T) {
	first, err := repairReadbackIDSuffix("runtime-A", 1, "case-A", "contract-A", repair.ContractRef{Path: "contract.json"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := repairReadbackIDSuffix("runtime-A", 1, "case-A", "contract-A", repair.ContractRef{Path: "contract.json"})
	if err != nil || first != again {
		t.Fatalf("historical digest changed responsibility: %s %s %v", first, again, err)
	}
	different, err := repairReadbackIDSuffix("runtime-a", 1, "case-A", "contract-A", repair.ContractRef{Path: "contract.json"})
	if err != nil || first == different {
		t.Fatalf("distinct identities collided: %s %s %v", first, different, err)
	}
}

func TestS9DispatchIdentityAndArtifactUseOriginalFacts(t *testing.T) {
	state := map[string]any{"entities": map[string]any{"tasks": []any{map[string]any{"id": "TASK-900-001"}, map[string]any{"id": "TASK-900-003"}}}}
	if got := nextS9RepairTaskID(state); got != "TASK-900-002" {
		t.Fatalf("next unallocated task = %q", got)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "original.json"), []byte(`{"note":"sha256 is a product feature"}`), 0600); err != nil {
		t.Fatal(err)
	}
	ref, err := artifactRef(root, "original.json", "original")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Path != "original.json" || ref.ID != "original" {
		t.Fatalf("unexpected reference: %+v", ref)
	}
}

func TestS9NativeLongPacketPathsRemainConsistent(t *testing.T) {
	id := "packet-" + strings.Repeat("LongIdentity-", 40)
	p := workpacket.RepairPlanReportPacket{ID: id, ViewPath: ".claude/work-packets/p/current.json"}
	brief := nativeS9BriefPath(p)
	// The route carries the registered brief coordinate; it is no longer
	// re-derived from the packet ID and view path
	// (L4-workflow-convergence §"S9 Main 集成与影响").
	route := runtime.RepairPlanReportRoute{ID: id, ViewPath: p.ViewPath, BriefPath: brief}
	if got := nativeS9RouteBrief(route); got != brief {
		t.Fatalf("route=%q brief=%q", got, brief)
	}
	if route.BriefPath != nativeS9BriefPath(p) {
		t.Fatalf("registered route brief %q differs from the packet brief %q", route.BriefPath, nativeS9BriefPath(p))
	}
	plan, privateResult, err := repair.NativeS9PrivatePaths(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{brief, plan, privateResult} {
		for _, part := range strings.Split(value, "/") {
			if len(part) > 200 {
				t.Fatalf("unbounded path component %d in %q", len(part), value)
			}
		}
	}
	legacy := "packet-" + strings.Repeat("a", 64)
	legacyPlan, _, err := repair.NativeS9PrivatePaths(legacy)
	if err != nil || legacyPlan != ".loop-submissions/native-s9/"+legacy+"/plan.json" {
		t.Fatalf("old path changed: %q %v", legacyPlan, err)
	}
}
