package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// The upstream Plan/dispatch are fixtures. Both professional answers go through
// the public native Stop, original Result consumer and real TR-009; no test
// writes a clean label or snapshot to stand in for the final received result.
func TestS7LastNativeResultHandsMainS10WithoutAnotherTool(t *testing.T) {
	for _, version := range []string{"1.2.0", "1.3.0"} {
		t.Run(version, func(t *testing.T) {
			root := fixtures.FreshRoot(t)
			state := fixtures.BaseState(t, root, "verification", "planned", 1)
			state["schema_version"] = version
			state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
			plan := minimalReviewPlan(t, root, "review-plan-native-clean-outlet")
			fixtures.WriteState(t, root, state)
			stageCLI(t, "runtime", "workspace", "bind", "--root", root)
			stageCLI(t, "runtime", "review-plan", "--root", root, "--file", plan)
			for _, id := range []string{"assignment-dv-1", "assignment-qa-1"} {
				markAssignmentDispatched(t, root, id, "reviewer-"+id)
			}
			current := stageSnapshot(t, root)
			for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
				a := raw.(map[string]any)
				a["dispatch_mode"] = "plan_checkpoint"
				if version == "1.3.0" {
					if err := workstate.RecordAgentActivity(current.State, a, "reading", "workgroup_registered"); err != nil {
						t.Fatal(err)
					}
				}
			}
			if version == "1.3.0" {
				for _, raw := range current.State["review"].(map[string]any)["assignments"].(map[string]any) {
					if err := workstate.RecordReviewAssignmentActivity(current.State, raw.(map[string]any), "dispatched", "workgroup_registered"); err != nil {
						t.Fatal(err)
					}
				}
			}
			stageJSON(t, root, ".claude/loop-state.json", current.State)
			var packets []workpacket.ReviewResultPacket
			if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")), &packets); err != nil || len(packets) != 2 {
				t.Fatalf("packets: %v %d", err, len(packets))
			}
			for i, packet := range packets {
				var claims []any
				for _, id := range packet.Binding.ClaimIDs {
					claims = append(claims, map[string]any{"claim_id": id, "conclusion": "pass", "method": "read actual frozen fixture", "observed": "the frozen fixture subject satisfies this assigned check", "evidence_refs": []string{}})
				}
				packet.Draft.Answer, _ = json.Marshal(map[string]any{"claim_results": claims, "blocked_claims": []any{}, "checks": []any{}, "findings": []any{}, "deviations": []string{}, "site_lost": []any{}, "verification_artifact_digest": nil, "verdict": "pass"})
				stageJSON(t, root, packet.AnswerPath, packet.Draft)
				ref := filepath.Join(root, packet.ViewPath)
				code, _, diagnostic := nativeReviewEvent(t, root, "SubagentStop", "opaque-"+packet.Binding.OwnerAgentID, ref)
				if code != 0 || !strings.Contains(diagnostic, "original consumer") {
					t.Fatalf("native reception: %d %s", code, diagnostic)
				}
				after := stageSnapshot(t, root)
				if i < len(packets)-1 {
					if after.State["lifecycle"].(map[string]any)["state"] != "verification" {
						t.Fatal("partial coverage advanced the round")
					}
					// The sibling answer is private and unreceived. Its contents
					// must not affect Main's read-only responsibility projection.
					sibling := packets[i+1]
					answer := filepath.Join(root, sibling.AnswerPath)
					original, err := os.ReadFile(answer)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(answer, []byte("PRIVATE-UNRECEIVED-ANSWER invalid JSON"), 0600); err != nil {
						t.Fatal(err)
					}
					text := mainNativeReturn(t, root, ref)
					for _, want := range []string{`"accepted":true`, `"work_complete":true`, `"waiting_on":[]`, sibling.Binding.AssignmentID, sibling.Binding.OwnerAgentID, filepath.Join(root, sibling.ViewPath), answer, "does not prove a native Reviewer is running"} {
						if !strings.Contains(text, want) {
							t.Fatalf("partial Main return missing %q: %s", want, text)
						}
					}
					if strings.Contains(text, "PRIVATE-UNRECEIVED-ANSWER") || stageSnapshot(t, root).Revision != after.Revision {
						t.Fatalf("Main projection read private contents or changed Runtime: %s", text)
					}
					if err := os.WriteFile(answer, original, 0600); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if after.State["lifecycle"].(map[string]any)["state"] != "acceptance" {
					t.Fatalf("last Result did not enter S10: %s", diagnostic)
				}
				path := filepath.Join(root, "s10", after.State["runtime_id"].(string), "g1", "r1", "acceptance-manifest.json")
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("same outlet did not prepare S10: %v; %s", err, diagnostic)
				}
				beforeMain := after.Revision
				text := mainNativeReturn(t, root, ref)
				for _, want := range []string{`"accepted":true`, "TR-009", path} {
					if !strings.Contains(text, want) {
						t.Fatalf("Main missing %q: %s", want, text)
					}
				}
				if stageSnapshot(t, root).Revision != beforeMain {
					t.Fatal("Main return projection changed Runtime")
				}
				if code, _, diagnostic := nativeReviewEvent(t, root, "SubagentStop", "opaque-replay", ref); code != 0 {
					t.Fatalf("replay: %d %s", code, diagnostic)
				}
				if stageSnapshot(t, root).Revision != beforeMain {
					t.Fatal("late return repeated transition")
				}
			}
		})
	}
}
