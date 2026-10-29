package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestS7CaptureShellQuoteRoundTrip(t *testing.T) {
	for _, value := range []string{"/tmp/review workspace/.claude/bin/loop-harness", "/tmp/reviewer's workspace/.claude/bin/loop-harness"} {
		t.Run(value, func(t *testing.T) {
			output, err := exec.Command("sh", "-c", "printf '%s' "+s7CaptureShellQuote(value)).Output()
			if err != nil {
				t.Fatal(err)
			}
			if string(output) != value {
				t.Fatalf("shell argument roundtrip: got %q, want %q", output, value)
			}
		})
	}
}

func TestS7MainManifestHintUsesExistingWorkgroupDirectoryWithoutOverwrite(t *testing.T) {
	for _, scenario := range []string{"missing-directory", "prepare", "existing-manifest"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, ".claude/workgroups")
			manifest := filepath.Join(directory, "manifest-g3-r2-assignment-qa-static.json")
			if scenario != "missing-directory" {
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "existing-manifest" {
				if err := os.WriteFile(manifest, []byte("preserved manifest"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			state := map[string]any{"baseline": map[string]any{"generation": 3}, "review": map[string]any{
				"round":       2,
				"plan":        map[string]any{"plan_id": "review-plan-current", "path": "original-plan.json"},
				"assignments": map[string]any{"assignment-qa-static": map[string]any{"status": "planned"}},
			}}
			output := s7ManifestPreparationHint(root, state, "assignment-qa-static")
			switch scenario {
			case "missing-directory", "prepare":
				command := " s7 manifest-draft --root " + s7CaptureShellQuote(root) + " --assignment 'assignment-qa-static' --out " + s7CaptureShellQuote(manifest)
				if !strings.Contains(output, command) || !strings.Contains(output, "stdout also contains guidance") {
					t.Fatalf("missing exact file-output command: %s", output)
				}
			case "existing-manifest":
				if !strings.Contains(output, manifest) || !strings.Contains(output, "preserve existing manifest") || !strings.Contains(output, "restore its original source") || strings.Contains(output, "--out") {
					t.Fatalf("existing manifest was not preserved: %s", output)
				}
				data, err := os.ReadFile(manifest)
				if err != nil || string(data) != "preserved manifest" {
					t.Fatalf("manifest changed: %q %v", data, err)
				}
			}
			if scenario != "existing-manifest" {
				if _, err := os.Stat(manifest); !os.IsNotExist(err) {
					t.Fatalf("reminder created a manifest: %v", err)
				}
			}
		})
	}
}

// This checks bounded reminder selection and file preservation, not successful
// multi-Reviewer issuance: cancellation stops the original issuer at entry.
func TestS7MainPreparationSelectsOneMissingOwnerAndPreservesReadyQuestionnaire(t *testing.T) {
	root := t.TempDir()
	binding := workpacket.ReviewResultBinding{
		RuntimeID: "loop-REQ-041", BaselineGeneration: 1, ReviewPlanID: "review-plan-current",
		ReviewPlanRef: ".claude/review/plan.json", ReviewPlanSHA256: strings.Repeat("a", 64), ReviewPlanRevision: 1, ReviewRound: 1,
		AssignmentID: "assignment-qa-ready", AssignmentSHA256: strings.Repeat("b", 64), OwnerAgentID: "reviewer-ready", ClaimIDs: []string{"claim-ready"}, SubjectDigest: strings.Repeat("c", 64),
	}
	packet, err := workpacket.BuildReviewResult(binding, workpacket.ReviewResultInputs{
		Claims:         json.RawMessage(`[{"claim_id":"claim-ready","lens":"qa","target":"service","assertion":"errors propagate","oracle":"error response","method":"inspection","applicability":"required","source_refs":["REQ-041"]}]`),
		FrozenSubjects: json.RawMessage(`[{"path":"internal/service.go","sha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(packet)
	var packetRow map[string]any
	if err := json.Unmarshal(encoded, &packetRow); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{packet.ViewPath, packet.AnswerPath} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("preserved existing questionnaire"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state := map[string]any{
		"schema_version": "1.2.0",
		"runtime_id":     binding.RuntimeID, "baseline": map[string]any{"generation": 1},
		"configuration": map[string]any{"optimization": map[string]any{"packet": true}},
		"entities":      map[string]any{"work_packets": []any{packetRow}},
		"review": map[string]any{
			"round": 1, "plan": map[string]any{"plan_id": binding.ReviewPlanID, "path": binding.ReviewPlanRef, "sha256": binding.ReviewPlanSHA256, "revision": 1, "review_round": 1},
			"assignments": map[string]any{
				binding.AssignmentID:   map[string]any{"status": "dispatched", "agent_id": binding.OwnerAgentID, "claim_ids": []any{"claim-ready"}},
				"assignment-missing-a": map[string]any{"status": "dispatched", "agent_id": "reviewer-missing-a"},
				"assignment-missing-b": map[string]any{"status": "dispatched", "agent_id": "reviewer-missing-b"},
			},
		},
	}
	for _, format := range []string{"inline", "index-with-missing-body"} {
		t.Run(format, func(t *testing.T) {
			if format == "index-with-missing-body" {
				index, err := packetbody.Publish(context.Background(), root, packetRow)
				if err != nil {
					t.Fatal(err)
				}
				ref, _, err := packetbody.ReferenceFromIndex(index)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(root, ref.Path)); err != nil {
					t.Fatal(err)
				}
				state["entities"].(map[string]any)["work_packets"] = []any{index}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			output := stageReviewResultLifecycleContext(ctx, root, policy.Input{Event: "PreToolUse", CWD: root}, &submission.Service{}, runtime.Snapshot{State: state}, "Main")
			for _, want := range []string{"assignment-missing-a", "assignment-missing-b", filepath.Join(root, filepath.FromSlash(packet.AnswerPath))} {
				if !strings.Contains(output, want) {
					t.Fatalf("missing current responsibility %q: %s", want, output)
				}
			}
			if strings.Count(output, "questionnaire preparation deferred for reviewer-missing-") != 1 || strings.Contains(output, "deferred for reviewer-ready") {
				t.Fatalf("preparation was not limited to one missing owner: %s", output)
			}
			for _, rel := range []string{packet.ViewPath, packet.AnswerPath} {
				data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
				if err != nil || string(data) != "preserved existing questionnaire" {
					t.Fatalf("ready questionnaire changed: %q %v", data, err)
				}
			}

			if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json")); !os.IsNotExist(err) {
				t.Fatalf("canceled read-only projection started a Runtime writer: %v", err)
			}
		})
	}
}
