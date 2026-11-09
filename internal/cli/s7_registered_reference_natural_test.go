package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// These fixtures model platform dispatch records, then issue the actual
// original ReviewResult packets through the current Writer. The first child
// transcript row has the actual Claude sidechain/actor shape; acceptance is
// exercised only through public Hook events and the original consumer.
func s7RegisteredReferenceTask(p workpacket.ReviewResultPacket) string {
	return fmt.Sprintf("You are responsible for exactly one S7 verification responsibility: (%s, Claim `%s`).", p.Binding.AssignmentID, p.Binding.ClaimIDs[0])
}

func s7RegisteredReferencePayload(t *testing.T, actor, task, event string) map[string]any {
	t.Helper()
	payload := s7ListenerPayload(t, actor, task)
	if event == "SubagentStop" {
		payload["agent_transcript_path"] = filepath.Join(strings.TrimSuffix(payload["transcript_path"].(string), ".jsonl"), "subagents", "agent-"+actor+".jsonl")
		payload["last_assistant_message"] = "My professional work has finished."
	}
	return payload
}

func TestS7RegisteredRelativeReferencesReachOriginalConsumer(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		for _, which := range []string{"view", "answer", "absolute"} {
			t.Run(event+"/"+which, func(t *testing.T) {
				root, p := s7ListenerFixture(t)
				s7ListenerAnswer(t, root, p)
				ref := p.ViewPath
				if which == "answer" {
					ref = p.AnswerPath
				}
				if which == "absolute" {
					ref = filepath.Join(root, p.ViewPath)
				}
				task := "Source root: `" + root + "`. Original questionnaire: `" + ref + "`."
				payload := s7RegisteredReferencePayload(t, "platform-original", task, event)
				out, diagnostic := s7ListenerEvent(t, root, event, payload)
				r := minimalAnswerReviewReceipt(t, root, p)
				if !r.Accepted || !r.WorkComplete {
					t.Fatalf("%s %s did not receive original: %+v; %s %s", event, which, r, out, diagnostic)
				}
				before := stageSnapshot(t, root).Revision
				s7ListenerEvent(t, root, event, payload)
				if after := stageSnapshot(t, root).Revision; after != before {
					t.Fatalf("replay duplicated original receipt: %d -> %d", before, after)
				}
			})
		}
	}
}

func TestS7OriginalDispatchIdentityRepairsPathTranscription(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop"} {
		t.Run(event, func(t *testing.T) {
			root, p := s7ListenerFixture(t)
			s7ListenerAnswer(t, root, p)
			task := s7RegisteredReferenceTask(p) + " Original view: `.claude/wrong/view.json`; answer: `.claude/wrong/answer.json`."
			payload := s7RegisteredReferencePayload(t, "platform-original", task, event)
			out, diagnostic := s7ListenerEvent(t, root, event, payload)
			if r := minimalAnswerReviewReceipt(t, root, p); !r.Accepted {
				t.Fatalf("explicit original responsibility was not received: %+v; %s %s", r, out, diagnostic)
			}
		})
	}
}

func TestS7MainReturnUsesSameOriginalReferenceSelection(t *testing.T) {
	for _, mode := range []string{"relative", "transcribed-path"} {
		t.Run(mode, func(t *testing.T) {
			root, p := s7ListenerFixture(t)
			s7ListenerAnswer(t, root, p)
			task := "Review the original `" + p.ViewPath + "`."
			if mode == "transcribed-path" {
				task = s7RegisteredReferenceTask(p) + " View: `.claude/wrong/view.json`."
			}
			fields := map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "async_launched", "isAsync": true}}
			s7ListenerEvent(t, root, "PostToolUse", fields)
			if r := minimalAnswerReviewReceipt(t, root, p); r.Accepted {
				t.Fatal("background launch received an answer before actual completion")
			}
			fields["tool_response"] = map[string]any{"status": "completed", "content": "My own original work returned."}
			out, diagnostic := s7ListenerEvent(t, root, "PostToolUse", fields)
			if r := minimalAnswerReviewReceipt(t, root, p); !r.Accepted {
				t.Fatalf("actual return did not receive original: %+v; %s %s", r, out, diagnostic)
			}
		})
	}
}

func TestS7OriginalRelativeIdentityNeverBorrowsOtherResponsibility(t *testing.T) {
	for _, mode := range []string{"background", "two-unlabelled", "wrong-actor", "explicit-conflict"} {
		t.Run(mode, func(t *testing.T) {
			root, ps := minimalAnswerReviewResponsibilities(t)
			for _, p := range ps {
				s7ListenerAnswer(t, root, p)
			}
			task := "Original `" + ps[0].ViewPath + "`; compare background `" + ps[1].ViewPath + "`."
			if mode == "background" {
				task = s7RegisteredReferenceTask(ps[0]) + " " + task
			}
			if mode == "explicit-conflict" {
				task = s7RegisteredReferenceTask(ps[0]) + " " + s7RegisteredReferenceTask(ps[1]) + " " + task
			}
			payload := s7RegisteredReferencePayload(t, "platform-first", task, "PostToolUse")
			if mode == "wrong-actor" {
				payload["agent_id"] = "different-platform-actor"
			}
			out, diagnostic := s7ListenerEvent(t, root, "PostToolUse", payload)
			first, second := minimalAnswerReviewReceipt(t, root, ps[0]), minimalAnswerReviewReceipt(t, root, ps[1])
			if first.Accepted != (mode == "background") || second.Accepted {
				t.Fatalf("%s: first=%+v second=%+v; %s %s", mode, first, second, out, diagnostic)
			}
		})
	}
}

// The new-Plan installation itself is an explicit fixture boundary; both
// original and later packets are actually issued, and neither receipt is made
// by the test. An unqualified shared Assignment/Claim cannot choose a Plan.
func TestS7TranscribedPathAcrossPlansStaysUnknown(t *testing.T) {
	root, old := s7ListenerFixture(t)
	// Model the new stage-entry fact before projecting and issuing the next
	// Plan. The old issued packet remains registered and in flight.
	before := stageSnapshot(t, root)
	writer := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{})
	life := before.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": life["state"], "phase": life["phase"]}
	_, err := writer.Update(before.Revision, runtime.Mutation{EventID: "evt-fixture-next-s7-entry", TransitionID: "FIXTURE-ENTRY", Event: "fixture_next_s7_entry", Actor: "orchestrator", RuntimeID: before.State["runtime_id"].(string), BaselineGeneration: old.Binding.BaselineGeneration, From: cursor, To: cursor, RetainLastTransition: true, IdempotencyKey: "fixture:next-s7-entry", OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		state["review"].(map[string]any)["round_entry"].(map[string]any)["plan_id"] = old.Binding.ReviewPlanID + "-next"
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	newer := lateS7NextPlan(t, root, true)
	if len(newer) != 1 || newer[0].Binding.AssignmentID != old.Binding.AssignmentID {
		t.Fatalf("unexpected original issuances: %+v", newer)
	}
	s7ListenerAnswer(t, root, old)
	s7ListenerAnswer(t, root, newer[0])
	task := s7RegisteredReferenceTask(old) + " Original view: `.claude/wrong/view.json`."
	out, diagnostic := s7ListenerEvent(t, root, "PreToolUse", s7RegisteredReferencePayload(t, "platform-ambiguous", task, "PreToolUse"))
	for _, p := range []workpacket.ReviewResultPacket{old, newer[0]} {
		if r := minimalAnswerReviewReceipt(t, root, p); r.Accepted {
			t.Fatalf("ambiguous plan received %s: %+v; %s %s", p.ID, r, out, diagnostic)
		}
	}
}

func TestS7RelativeOriginalFindingAndCleanExit(t *testing.T) {
	t.Run("finding-enters-s8", func(t *testing.T) {
		root, p := s7ListenerFixture(t)
		minimalAnswerFinding(t, root, p, "finding-relative-original")
		task := "Read the original `" + p.ViewPath + "`."
		out, diagnostic := s7ListenerEvent(t, root, "PostToolUse", s7RegisteredReferencePayload(t, "platform-finding", task, "PostToolUse"))
		if r := minimalAnswerReviewReceipt(t, root, p); !r.Accepted {
			t.Fatalf("finding not received: %+v; %s %s", r, out, diagnostic)
		}
		state := stageSnapshot(t, root).State
		life := state["lifecycle"].(map[string]any)
		if life["state"] != "bug_resolution" || life["phase"] != "investigation" {
			t.Fatalf("S8 not reached: %#v", life)
		}
		rows := state["entities"].(map[string]any)["findings"].([]any)
		if len(rows) != 1 {
			t.Fatalf("original Finding missing: %#v", rows)
		}
		finding := rows[0].(map[string]any)
		if finding["original_finder"] != p.Binding.OwnerAgentID || finding["claim_id"] != p.Binding.ClaimIDs[0] {
			t.Fatalf("finding lost responsibility: %#v", finding)
		}
		if _, err := os.ReadFile(filepath.Join(root, finding["path"].(string))); err != nil {
			t.Fatalf("actual Finding path: %v", err)
		}
	})
	t.Run("clean-enters-s10", func(t *testing.T) {
		root, ps := minimalAnswerReviewResponsibilities(t)
		for _, p := range ps {
			p.Draft.Answer, _ = json.Marshal(map[string]any{"verdict": "pass", "findings": []any{}, "checks": []any{}})
			stageJSON(t, root, p.AnswerPath, p.Draft)
			out, diagnostic := s7ListenerEvent(t, root, "PostToolUse", s7RegisteredReferencePayload(t, "platform-clean-"+p.Binding.AssignmentID, "Review `"+p.ViewPath+"`.", "PostToolUse"))
			if r := minimalAnswerReviewReceipt(t, root, p); !r.Accepted {
				t.Fatalf("original result not received: %+v; %s %s", r, out, diagnostic)
			}
		}
		if life := stageSnapshot(t, root).State["lifecycle"].(map[string]any); life["state"] != "acceptance" {
			t.Fatalf("S10 not reached: %#v", life)
		}
	})
}

func TestS7RelativePathBoundariesNeverBorrowAnotherAnswer(t *testing.T) {
	for _, mode := range []string{"suffix", "basename", "different-root", "dotdot"} {
		t.Run(mode, func(t *testing.T) {
			root, p := s7ListenerFixture(t)
			s7ListenerAnswer(t, root, p)
			ref := p.ViewPath
			switch mode {
			case "suffix":
				ref += ".bak"
			case "basename":
				ref = filepath.Base(ref)
			case "different-root":
				ref = filepath.Join(t.TempDir(), ref)
			case "dotdot":
				ref = "../other/" + ref
			}
			before := stageSnapshot(t, root).Revision
			out, diagnostic := s7ListenerEvent(t, root, "PostToolUse", s7RegisteredReferencePayload(t, "platform-path-boundary", "Review `"+ref+"`.", "PostToolUse"))
			if r := minimalAnswerReviewReceipt(t, root, p); r.Accepted {
				t.Fatalf("%s incorrectly received: %+v; %s %s", mode, r, out, diagnostic)
			}
			if after := stageSnapshot(t, root).Revision; before != after {
				t.Fatalf("unknown path changed Runtime: %d -> %d", before, after)
			}
		})
	}
}

func TestS7MainReturnCannotBorrowWorkerReturnText(t *testing.T) {
	root, p := s7ListenerFixture(t)
	s7ListenerAnswer(t, root, p)
	fields := map[string]any{
		"agent_id": "Main", "tool_name": "Agent",
		"tool_input":    map[string]any{"prompt": "Perform unrelated independent work."},
		"tool_response": map[string]any{"status": "completed", "content": "Another original answer is " + p.AnswerPath},
	}
	out, diagnostic := s7ListenerEvent(t, root, "PostToolUse", fields)
	if r := minimalAnswerReviewReceipt(t, root, p); r.Accepted {
		t.Fatalf("return text alone nominated original: %+v; %s %s", r, out, diagnostic)
	}
}

// The S7 workgroup facts in this fixture are simulated, but the isolated
// worktree is materialized by the actual workspace API; a Runtime Writer
// registers it before the real packet issuer publishes and records its paths.
func s7RegisteredIsolatedExecution(t *testing.T) (string, string, workpacket.ReviewResultPacket) {
	t.Helper()
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": "review-plan-isolated-reference", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
	plan := minimalReviewPlan(t, root, "review-plan-isolated-reference")
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	if _, err := review.RegisterPlan(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), review.PlanRequest{ExpectedRevision: -1, PlanPath: plan}); err != nil {
		t.Fatal(err)
	}
	markAssignmentDispatched(t, root, "assignment-dv-1", "reviewer-isolated-reference")
	current := stageSnapshot(t, root)
	for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
		if agent := raw.(map[string]any); agent["id"] == "reviewer-isolated-reference" {
			agent["dispatch_mode"] = "plan_checkpoint"
		}
	}
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	current = stageSnapshot(t, root)
	binding, err := workspace.Decode(current.State)
	if err != nil || binding == nil {
		t.Fatalf("actual workspace: %v", err)
	}
	execution, err := binding.Plan(context.Background(), current.State, "assignment-dv-1", "reviewer-isolated-reference", []string{"internal/example/"}, []string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.Materialize(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	execution.Status = "ready"
	binding.Executions[execution.AssignmentID] = execution
	writer := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{})
	life := current.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": life["state"], "phase": life["phase"]}
	_, err = writer.Update(current.Revision, runtime.Mutation{EventID: "evt-fixture-isolated-review", TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "main", RuntimeID: current.State["runtime_id"].(string), BaselineGeneration: 1, From: cursor, To: cursor, RetainLastTransition: true, IdempotencyKey: "fixture:isolated-review", OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		state["workspace"] = workspace.Encode(binding)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	var packets []workpacket.ReviewResultPacket
	if out := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main"); json.Unmarshal([]byte(out), &packets) != nil || len(packets) != 1 {
		t.Fatalf("original issuer: %s", out)
	}
	p := packets[0]
	if _, err := os.Stat(filepath.Join(execution.Path, p.AnswerPath)); err != nil {
		t.Fatalf("original issued answer not in execution root: %v", err)
	}
	return root, execution.Path, p
}

func TestS7OriginalIsolatedExecutionRegisteredReferenceEvents(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop", "MainReturn"} {
		for _, form := range []string{"relative", "absolute"} {
			t.Run(event+"/"+form, func(t *testing.T) {
				root, source, p := s7RegisteredIsolatedExecution(t)
				s7ListenerAnswer(t, source, p)
				ref := p.AnswerPath
				if form == "absolute" {
					ref = filepath.Join(source, ref)
				}
				task := "Original execution source `" + source + "`; answer `" + ref + "`."
				var out, diagnostic string
				if event == "MainReturn" {
					out, diagnostic = s7ListenerEvent(t, root, "PostToolUse", map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "completed"}})
				} else {
					payload := s7RegisteredReferencePayload(t, "isolated-platform-reviewer", task, event)
					payload["cwd"] = source
					out, diagnostic = s7ListenerEvent(t, root, event, payload)
				}
				if r := minimalAnswerReviewReceipt(t, root, p); !r.Accepted {
					t.Fatalf("%s %s: isolated original not received: %+v; %s %s", event, form, r, out, diagnostic)
				}
			})
		}
	}
}

func TestS7OriginalLatePlanRelativeReferenceReceivesOnlyOriginal(t *testing.T) {
	root, old := s7ListenerFixture(t)
	before := stageSnapshot(t, root)
	writer := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{})
	life := before.State["lifecycle"].(map[string]any)
	cursor := map[string]any{"state": life["state"], "phase": life["phase"]}
	_, err := writer.Update(before.Revision, runtime.Mutation{EventID: "evt-fixture-late-s7-entry", TransitionID: "FIXTURE-ENTRY", Event: "fixture_next_s7_entry", Actor: "orchestrator", RuntimeID: before.State["runtime_id"].(string), BaselineGeneration: old.Binding.BaselineGeneration, From: cursor, To: cursor, RetainLastTransition: true, IdempotencyKey: "fixture:late-s7-entry", OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		state["review"].(map[string]any)["round_entry"].(map[string]any)["plan_id"] = old.Binding.ReviewPlanID + "-next"
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	newer := lateS7NextPlan(t, root, true)
	if len(newer) != 1 {
		t.Fatalf("new original issuance: %+v", newer)
	}
	s7ListenerAnswer(t, root, old)
	out, diagnostic := s7ListenerEvent(t, root, "PostToolUse", s7RegisteredReferencePayload(t, "platform-late-original", "Original `"+old.ViewPath+"`.", "PostToolUse"))
	if r := minimalAnswerReviewReceipt(t, root, old); !r.Accepted {
		t.Fatalf("late original not received: %+v; %s %s", r, out, diagnostic)
	}
	if r := minimalAnswerReviewReceipt(t, root, newer[0]); r.Accepted {
		t.Fatalf("late original claimed new responsibility: %+v", r)
	}
}

func TestS7ExplicitDispatchPairFormsAndSegmentedConflict(t *testing.T) {
	for _, form := range []string{"assigned", "structured", "segmented-conflict", "one-and-background"} {
		t.Run(form, func(t *testing.T) {
			root, ps := minimalAnswerReviewResponsibilities(t)
			for _, p := range ps {
				s7ListenerAnswer(t, root, p)
			}
			first := ps[0]
			var task string
			switch form {
			case "assigned":
				task = fmt.Sprintf("Assigned assignment: `%s`, claim: `%s`. Wrong path: `.claude/wrong.json`.", first.Binding.AssignmentID, first.Binding.ClaimIDs[0])
			case "structured":
				task = fmt.Sprintf("Assigned responsibility: {\"assignment_id\":%q,\"claim_ids\":[%q]}. Wrong path: `.claude/wrong.json`.", first.Binding.AssignmentID, first.Binding.ClaimIDs[0])
			case "segmented-conflict":
				task = s7RegisteredReferenceTask(first) + "\n\n" + s7RegisteredReferenceTask(ps[1])
			case "one-and-background":
				task = s7RegisteredReferenceTask(first) + "\nCompare background " + ps[1].Binding.AssignmentID + " and " + ps[1].Binding.ClaimIDs[0] + "."
			}
			out, diagnostic := s7ListenerEvent(t, root, "PreToolUse", s7RegisteredReferencePayload(t, "platform-form", task, "PreToolUse"))
			one, two := minimalAnswerReviewReceipt(t, root, first), minimalAnswerReviewReceipt(t, root, ps[1])
			if one.Accepted != (form != "segmented-conflict") || two.Accepted {
				t.Fatalf("%s: first=%+v second=%+v; %s %s", form, one, two, out, diagnostic)
			}
		})
	}
}

func TestS7SharedPrefixAndBackgroundPairCannotRedirectOriginalActor(t *testing.T) {
	for _, event := range []string{"PostToolUse", "MainReturn"} {
		for _, form := range []string{"one-prefix-two-pairs", "one-prefix-list", "one-prefix-two-objects", "background-pair", "background-assigned-fields"} {
			t.Run(event+"/"+form, func(t *testing.T) {
				root, ps := minimalAnswerReviewResponsibilities(t)
				for _, p := range ps {
					s7ListenerAnswer(t, root, p)
				}
				a, b := ps[0], ps[1]
				pair := func(p workpacket.ReviewResultPacket) string {
					return fmt.Sprintf("(%s, Claim %s)", p.Binding.AssignmentID, p.Binding.ClaimIDs[0])
				}
				var task string
				switch form {
				case "one-prefix-two-pairs":
					task = "Assigned responsibility: " + pair(a) + ", " + pair(b) + "."
				case "one-prefix-list":
					task = "Assigned responsibility:\n- " + pair(a) + "\n- " + pair(b) + "."
				case "one-prefix-two-objects":
					task = fmt.Sprintf("Assigned responsibility: [{\"assignment_id\":%q,\"claim_id\":%q}, {\"assignment_id\":%q,\"claim_id\":%q}].", a.Binding.AssignmentID, a.Binding.ClaimIDs[0], b.Binding.AssignmentID, b.Binding.ClaimIDs[0])
				case "background-pair":
					task = "S7 responsibility: review original `" + a.ViewPath + "`; compare background " + pair(b) + "."
				case "background-assigned-fields":
					task = fmt.Sprintf("Review original `%s`; compare background assigned assignment: %s, claim: %s.", a.ViewPath, b.Binding.AssignmentID, b.Binding.ClaimIDs[0])
				}
				var out, diagnostic string
				if event == "MainReturn" {
					out, diagnostic = s7ListenerEvent(t, root, "PostToolUse", map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "completed"}})
				} else {
					out, diagnostic = s7ListenerEvent(t, root, event, s7RegisteredReferencePayload(t, "platform-prefix", task, event))
				}
				first, second := minimalAnswerReviewReceipt(t, root, a), minimalAnswerReviewReceipt(t, root, b)
				if first.Accepted != (form == "background-pair" || form == "background-assigned-fields") || second.Accepted {
					t.Fatalf("%s %s: first=%+v second=%+v; %s %s", event, form, first, second, out, diagnostic)
				}
			})
		}
	}
}

// Mirrors the real offline Claude first dispatch structure including the bold
// role label; the actual Assignment/Claim are the original test issuance.
func TestS7OfflineExactOriginalDispatchHandlesTranscribedPath(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop", "MainReturn"} {
		t.Run(event, func(t *testing.T) {
			root, p := s7ListenerFixture(t)
			s7ListenerAnswer(t, root, p)
			task := fmt.Sprintf("You are the native Agent for exactly one S7 verification responsibility: **QA / design-boundary** (%s, Claim `%s`). You own your own professional ReviewResult; Main does not write it.\n\n- questionnaire (view): `.claude/wrong/view.json`\n- private answer (save exactly here): `.claude/wrong/answer.json`", p.Binding.AssignmentID, p.Binding.ClaimIDs[0])
			var out, diagnostic string
			if event == "MainReturn" {
				out, diagnostic = s7ListenerEvent(t, root, "PostToolUse", map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "completed"}})
			} else {
				out, diagnostic = s7ListenerEvent(t, root, event, s7RegisteredReferencePayload(t, "platform-offline-qa", task, event))
			}
			if r := minimalAnswerReviewReceipt(t, root, p); !r.Accepted {
				t.Fatalf("real original dispatch did not receive original: %+v; %s %s", r, out, diagnostic)
			}
		})
	}
}

func TestS7ExplicitSameLineDifferentOriginalPathRemainsUnknown(t *testing.T) {
	for _, event := range []string{"PostToolUse", "MainReturn"} {
		for _, label := range []string{"Original view", "Original answer"} {
			t.Run(event+"/"+label, func(t *testing.T) {
				root, ps := minimalAnswerReviewResponsibilities(t)
				for _, p := range ps {
					s7ListenerAnswer(t, root, p)
				}
				a, b := ps[0], ps[1]
				ref := b.ViewPath
				if label == "Original answer" {
					ref = b.AnswerPath
				}
				task := s7RegisteredReferenceTask(a) + " " + label + ": `" + ref + "`."
				var out, diagnostic string
				if event == "MainReturn" {
					out, diagnostic = s7ListenerEvent(t, root, "PostToolUse", map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "completed"}})
				} else {
					out, diagnostic = s7ListenerEvent(t, root, event, s7RegisteredReferencePayload(t, "platform-conflicting-original", task, event))
				}
				first, second := minimalAnswerReviewReceipt(t, root, a), minimalAnswerReviewReceipt(t, root, b)
				if first.Accepted || second.Accepted {
					t.Fatalf("explicit conflicting origin selected a responsibility: first=%+v second=%+v; %s %s", first, second, out, diagnostic)
				}
			})
		}
	}
}

// Prepare a genuine S7 Finding -> S8 intake/issuance and retain the other
// already-issued Reviewer in flight; no test-created packet or receipt.
func s7s8IssuedPeerFixture(t *testing.T) (string, workpacket.ReviewResultPacket, workpacket.HypothesisResultPacket) {
	t.Helper()
	root, investigation := minimalAnswerInvestigator(t)
	state := stageSnapshot(t, root).State
	indices, err := runtime.ProjectIssuedReviewResultPacketIndex(state, "reviewer-minimal-quality")
	if err != nil || len(indices) != 1 {
		t.Fatalf("original late Reviewer index: %+v %v", indices, err)
	}
	ctx := runtime.WithPacketBodyRoot(context.Background(), root)
	peer, found, err := runtime.FindReviewResultPacketContext(ctx, state, indices[0].ID)
	if err != nil || !found {
		t.Fatalf("original issued peer: %+v found=%v err=%v", peer, found, err)
	}
	s7ListenerAnswer(t, root, peer)
	investigation.Draft.Answer = json.RawMessage(`{"result":"inconclusive","observed":"The original professional observation remains inconclusive."}`)
	stageJSON(t, root, investigation.AnswerPath, investigation.Draft)
	return root, peer, investigation
}

func TestS7S8OriginalTasksIgnoreOtherStageBackground(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop", "MainReturn"} {
		for _, ownStage := range []string{"S7", "S8"} {
			t.Run(event+"/"+ownStage, func(t *testing.T) {
				root, reviewer, investigator := s7s8IssuedPeerFixture(t)
				// Use the real stage guidance shape (owner/assignment + paths),
				// not a required English or structured identity template.
				var task string
				if ownStage == "S7" {
					task = fmt.Sprintf("S7 Reviewer %s assignment=%s: Main carries exact view %s and answer %s into native Agent. Compare background {\"hypothesis_id\":%q,\"owner_agent_id\":%q}.", reviewer.Binding.OwnerAgentID, reviewer.Binding.AssignmentID, reviewer.ViewPath, reviewer.AnswerPath, investigator.Binding.HypothesisID, investigator.Binding.OwnerAgentID)
				} else {
					task = fmt.Sprintf("S8 Investigator %s assignment=%s: Main carries exact question %s and private questionnaire answer %s. Compare background {\"assignment_id\":%q,\"claim_id\":%q}.", investigator.Binding.OwnerAgentID, investigator.Binding.AssignmentID, investigator.ViewPath, investigator.AnswerPath, reviewer.Binding.AssignmentID, reviewer.Binding.ClaimIDs[0])
				}
				var out, diagnostic string
				if event == "MainReturn" {
					out, diagnostic = s7ListenerEvent(t, root, "PostToolUse", map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "completed"}})
				} else {
					out, diagnostic = s7ListenerEvent(t, root, event, s7RegisteredReferencePayload(t, "platform-"+ownStage, task, event))
				}
				reviewReceipt := minimalAnswerReviewReceipt(t, root, reviewer)
				service, err := submission.New(root)
				if err != nil {
					t.Fatal(err)
				}
				investigationReceipt, err := service.HypothesisResultReceipt(investigator.Binding.OwnerAgentID, investigator.ID)
				if err != nil {
					t.Fatal(err)
				}
				if reviewReceipt.Accepted != (ownStage == "S7") || investigationReceipt.Accepted != (ownStage == "S8") {
					t.Fatalf("original %s: S7=%+v S8=%+v; stdout=%s stderr=%s", ownStage, reviewReceipt, investigationReceipt, out, diagnostic)
				}
			})
		}
	}
}

func TestS7ExplicitSingularDispatchWithContradictoryPairStaysUnknown(t *testing.T) {
	for _, event := range []string{"PostToolUse", "MainReturn"} {
		for _, separator := range []string{", and ", "; and ", " and ", ",\n- "} {
			t.Run(event+"/"+fmt.Sprintf("%q", separator), func(t *testing.T) {
				root, ps := minimalAnswerReviewResponsibilities(t)
				for _, p := range ps {
					s7ListenerAnswer(t, root, p)
				}
				pair := func(p workpacket.ReviewResultPacket) string {
					return fmt.Sprintf("(%s, Claim %s)", p.Binding.AssignmentID, p.Binding.ClaimIDs[0])
				}
				// A singular, explicitly exclusive dispatch that itself names two
				// different authors is contradictory; it is not a test of a
				// platform dispatch that genuinely grants multiple responsibilities.
				task := "Exactly one S7 verification responsibility: " + pair(ps[0]) + separator + pair(ps[1]) + "."
				var out, diagnostic string
				if event == "MainReturn" {
					out, diagnostic = s7ListenerEvent(t, root, "PostToolUse", map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "completed"}})
				} else {
					out, diagnostic = s7ListenerEvent(t, root, event, s7RegisteredReferencePayload(t, "platform-singular-contradiction", task, event))
				}
				for _, p := range ps {
					if r := minimalAnswerReviewReceipt(t, root, p); r.Accepted {
						t.Fatalf("contradictory singular dispatch received %s: %+v; %s %s", p.ID, r, out, diagnostic)
					}
				}
			})
		}
	}
}

func TestS7S8ContradictorySingularDispatchNeverClaimsEitherAuthor(t *testing.T) {
	for _, event := range []string{"PostToolUse", "MainReturn"} {
		t.Run(event, func(t *testing.T) {
			root, reviewer, investigator := s7s8IssuedPeerFixture(t)
			// Distinct owners are both presented as the one owner of the same
			// dispatch: this is conflict, not an authorized multi-responsibility
			// platform dispatch (for which no real platform sample is asserted).
			task := s7RegisteredReferenceTask(reviewer) + fmt.Sprintf(" You are assigned exactly one S8 investigation: investigate `%s` (owner `%s`). Original question: `%s`.", investigator.Binding.HypothesisID, investigator.Binding.OwnerAgentID, investigator.ViewPath)
			var out, diagnostic string
			if event == "MainReturn" {
				out, diagnostic = s7ListenerEvent(t, root, "PostToolUse", map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "completed"}})
			} else {
				out, diagnostic = s7ListenerEvent(t, root, event, s7RegisteredReferencePayload(t, "platform-cross-stage-contradiction", task, event))
			}
			r := minimalAnswerReviewReceipt(t, root, reviewer)
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			i, err := service.HypothesisResultReceipt(investigator.Binding.OwnerAgentID, investigator.ID)
			if err != nil {
				t.Fatal(err)
			}
			if r.Accepted || i.Accepted {
				t.Fatalf("conflicting single-author dispatch received S7=%+v S8=%+v; %s %s", r, i, out, diagnostic)
			}
		})
	}
}

// This fixture creates a same-lens QA plan and actually issues both original
// packets. Workgroup registrations and the single platform task are explicitly
// synthetic test fixtures; no real multi-responsibility Claude sample is claimed.
func s7SameLensIssuedPairFixture(t *testing.T) (string, []workpacket.ReviewResultPacket) {
	t.Helper()
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "verification", "planned", 1)
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	state["review"].(map[string]any)["round_entry"] = map[string]any{"plan_id": "review-plan-multi-same-lens", "transition_id": "TR-006", "baseline_generation": 1, "change_impact_ref": nil}
	planPath := minimalReviewPlan(t, root, "review-plan-multi-same-lens")
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	firstClaim := plan["claims"].([]any)[0].(map[string]any)
	firstClaim["lens"] = "qa"
	firstClaim["assertion"] = "the separate response boundary remains observable"
	firstAssignment := plan["assignments"].([]any)[0].(map[string]any)
	firstAssignment["lens"] = "qa"
	firstAssignment["non_overlap_boundary"] = "owns the separate response boundary"
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, append(encoded, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	fixtures.WriteState(t, root, state)
	stageCLI(t, "runtime", "workspace", "bind", "--root", root)
	if _, err := review.RegisterPlan(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), review.PlanRequest{ExpectedRevision: -1, PlanPath: planPath}); err != nil {
		t.Fatal(err)
	}
	markAssignmentDispatched(t, root, "assignment-dv-1", "reviewer-qa-boundary")
	markAssignmentDispatched(t, root, "assignment-qa-1", "reviewer-qa-errors")
	current := stageSnapshot(t, root)
	for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
		raw.(map[string]any)["dispatch_mode"] = "plan_checkpoint"
	}
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	var packets []workpacket.ReviewResultPacket
	if out := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main"); json.Unmarshal([]byte(out), &packets) != nil || len(packets) != 2 {
		t.Fatalf("actual same-lens issuer: %s", out)
	}
	return root, packets
}

func TestS7OneSyntheticDispatchOfExplicitSameLensResponsibilitiesReceivesIndependently(t *testing.T) {
	for _, event := range []string{"PostToolUse", "MainReturn"} {
		for _, mode := range []string{"both-ready", "second-draft", "second-invalid", "second-source-missing"} {
			t.Run(event+"/"+mode, func(t *testing.T) {
				root, ps := s7SameLensIssuedPairFixture(t)
				s7ListenerAnswer(t, root, ps[0])
				if mode == "both-ready" {
					s7ListenerAnswer(t, root, ps[1])
				}
				if mode == "second-invalid" {
					ps[1].Draft.Answer = json.RawMessage(`{"verdict":"not-a-supported-verdict"}`)
					stageJSON(t, root, ps[1].AnswerPath, ps[1].Draft)
				}
				if mode == "second-source-missing" {
					if err := os.Remove(filepath.Join(root, ps[1].AnswerPath)); err != nil {
						t.Fatal(err)
					}
				}
				pair := func(p workpacket.ReviewResultPacket) string {
					return fmt.Sprintf("(%s, Claim %s)", p.Binding.AssignmentID, p.Binding.ClaimIDs[0])
				}
				task := fmt.Sprintf("This single native Agent is explicitly assigned both original QA responsibilities.\nS7 responsibilities: %s, and %s. Original views: `%s`, `%s`. Original answers: `%s`, `%s`.", pair(ps[0]), pair(ps[1]), ps[0].ViewPath, ps[1].ViewPath, ps[0].AnswerPath, ps[1].AnswerPath)
				var out, diagnostic string
				if event == "MainReturn" {
					out, diagnostic = s7ListenerEvent(t, root, "PostToolUse", map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "completed"}})
				} else {
					out, diagnostic = s7ListenerEvent(t, root, event, s7RegisteredReferencePayload(t, "synthetic-platform-qa", task, event))
				}
				first, second := minimalAnswerReviewReceipt(t, root, ps[0]), minimalAnswerReviewReceipt(t, root, ps[1])
				if !first.Accepted || second.Accepted != (mode == "both-ready") {
					t.Fatalf("%s: first=%+v second=%+v; %s %s", mode, first, second, out, diagnostic)
				}
				if mode == "both-ready" {
					if life := stageSnapshot(t, root).State["lifecycle"].(map[string]any); life["state"] != "acceptance" {
						t.Fatalf("completed original responsibilities did not reach S10: %#v", life)
					}
				}
			})
		}
	}
}

// This is an explicitly synthetic single-platform-actor dispatch across two
// already-issued responsibilities. It does not claim a real Claude multi-task
// payload, waive professional independence, or create either packet/receipt.
func TestS7S8ExplicitSyntheticMultiDispatchReceivesOriginalsIndependently(t *testing.T) {
	for _, event := range []string{"PostToolUse", "MainReturn"} {
		for _, mode := range []string{"both-ready", "s8-draft", "s7-invalid", "s7-source-missing"} {
			t.Run(event+"/"+mode, func(t *testing.T) {
				root, reviewer, investigator := s7s8IssuedPeerFixture(t)
				if mode == "s8-draft" {
					investigator.Draft.Answer = json.RawMessage(`{}`)
					stageJSON(t, root, investigator.AnswerPath, investigator.Draft)
				}
				if mode == "s7-invalid" {
					reviewer.Draft.Answer = json.RawMessage(`{"verdict":"not-a-supported-verdict"}`)
					stageJSON(t, root, reviewer.AnswerPath, reviewer.Draft)
				}
				if mode == "s7-source-missing" {
					if err := os.Remove(filepath.Join(root, reviewer.AnswerPath)); err != nil {
						t.Fatal(err)
					}
				}
				task := fmt.Sprintf("This single native Agent is explicitly assigned both original responsibilities.\nS7 responsibilities: (%s, Claim %s).\nS8 responsibilities: investigate `%s` (owner `%s`).\nS7 original view: `%s`. S7 original answer: `%s`.\nS8 original question: `%s`. S8 original answer: `%s`.", reviewer.Binding.AssignmentID, reviewer.Binding.ClaimIDs[0], investigator.Binding.HypothesisID, investigator.Binding.OwnerAgentID, reviewer.ViewPath, reviewer.AnswerPath, investigator.ViewPath, investigator.AnswerPath)
				var out, diagnostic string
				if event == "MainReturn" {
					out, diagnostic = s7ListenerEvent(t, root, "PostToolUse", map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "completed"}})
				} else {
					out, diagnostic = s7ListenerEvent(t, root, event, s7RegisteredReferencePayload(t, "synthetic-platform-cross-stage", task, event))
				}
				r := minimalAnswerReviewReceipt(t, root, reviewer)
				service, err := submission.New(root)
				if err != nil {
					t.Fatal(err)
				}
				i, err := service.HypothesisResultReceipt(investigator.Binding.OwnerAgentID, investigator.ID)
				if err != nil {
					t.Fatal(err)
				}
				if r.Accepted != (mode != "s7-source-missing" && mode != "s7-invalid") || i.Accepted != (mode != "s8-draft") {
					t.Fatalf("%s: S7=%+v S8=%+v; %s %s", mode, r, i, out, diagnostic)
				}
			})
		}
	}
}

// Exercise an allowed original Hypothesis ID without a conventional prefix.
// The Case, Main answer, dispatch and packet are all created by their real
// consumers/issuer; the ID is a fixture input, not a new authoring requirement.
func s8AllowedPlainHypothesisFixture(t *testing.T) (string, workpacket.HypothesisResultPacket) {
	t.Helper()
	root, reviewers := minimalAnswerReviewResponsibilities(t)
	for _, rel := range []string{"agents/investigator.md", "docs/control/agent-protocol.md"} {
		data, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), rel))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	minimalAnswerFinding(t, root, reviewers[0], "finding-plain-hypothesis")
	s7ListenerEvent(t, root, "PreToolUse", s7ListenerPayload(t, "synthetic-source-reviewer", "Review original "+filepath.Join(root, reviewers[0].ViewPath)))
	paths, err := minimalAnswerIntakePaths(root)
	if err != nil || len(paths) != 1 {
		t.Fatalf("original S8 intake: %v %v", paths, err)
	}
	intake := paths[0]
	stageJSON(t, root, strings.TrimPrefix(intake, root+string(filepath.Separator)), map[string]any{"grouping_rationale": "Investigate the original response boundary."})
	save := func(path string) {
		t.Helper()
		s7ListenerEvent(t, root, "PostToolUse", map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": path}, "tool_response": map[string]any{"filePath": path}})
	}
	save(intake)
	questions := filepath.Join(filepath.Dir(intake), "hypotheses.json")
	stageJSON(t, root, strings.TrimPrefix(questions, root+string(filepath.Separator)), []any{map[string]any{
		"hypothesis_id": "plain-root-cause-1", "statement": "The response branch may be selected incorrectly.", "invariant": "Rejected input produces an error.",
		"discriminator": "Inspect the actual response branch.", "expected_outcomes": map[string]string{"support": "success branch", "refute": "error branch"},
		"source_finding_ids": []string{"finding-plain-hypothesis"}, "evidence_refs": []string{"test://plain-hypothesis"},
	}})
	save(questions)
	var packets []workpacket.HypothesisResultPacket
	if out := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main"); json.Unmarshal([]byte(out), &packets) != nil || len(packets) != 1 {
		t.Fatalf("original S8 issuer: %s", out)
	}
	if packets[0].Binding.HypothesisID != "plain-root-cause-1" {
		t.Fatalf("allowed original ID changed: %+v", packets[0].Binding)
	}
	return root, packets[0]
}

func TestS8AllowedPlainHypothesisIdentityKeepsOriginalNaturalReception(t *testing.T) {
	for _, event := range []string{"PostToolUse", "MainReturn"} {
		for _, ref := range []string{"original-path", "misprinted-path"} {
			t.Run(event+"/"+ref, func(t *testing.T) {
				root, p := s8AllowedPlainHypothesisFixture(t)
				p.Draft.Answer = json.RawMessage(`{"result":"inconclusive","observed":"The original observation remains inconclusive."}`)
				stageJSON(t, root, p.AnswerPath, p.Draft)
				path := p.ViewPath
				if ref == "misprinted-path" {
					path = ".claude/wrong/view.json"
				}
				task := fmt.Sprintf("Investigate `%s` (owner `%s`). Original question: `%s`.", p.Binding.HypothesisID, p.Binding.OwnerAgentID, path)
				var out, diagnostic string
				if event == "MainReturn" {
					out, diagnostic = s7ListenerEvent(t, root, "PostToolUse", map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "completed"}})
				} else {
					out, diagnostic = s7ListenerEvent(t, root, event, s7RegisteredReferencePayload(t, "platform-plain-s8", task, event))
				}
				service, err := submission.New(root)
				if err != nil {
					t.Fatal(err)
				}
				r, err := service.HypothesisResultReceipt(p.Binding.OwnerAgentID, p.ID)
				if err != nil || !r.Accepted {
					t.Fatalf("allowed original Hypothesis not received: %+v err=%v; %s %s", r, err, out, diagnostic)
				}
			})
		}
	}
}

// The two questions are accepted and issued by the original S8 intake and
// Writer. The single actor carrying both is a synthetic platform dispatch.
func s8TwoIssuedInvestigatorsFixture(t *testing.T) (string, []workpacket.HypothesisResultPacket) {
	t.Helper()
	root, reviewers := minimalAnswerReviewResponsibilities(t)
	for _, rel := range []string{"agents/investigator.md", "docs/control/agent-protocol.md"} {
		data, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), rel))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	minimalAnswerFinding(t, root, reviewers[0], "finding-two-investigations")
	s7ListenerEvent(t, root, "PreToolUse", s7ListenerPayload(t, "source-reviewer", "Review original "+filepath.Join(root, reviewers[0].ViewPath)))
	paths, err := minimalAnswerIntakePaths(root)
	if err != nil || len(paths) != 1 {
		t.Fatalf("original intake: %v %v", paths, err)
	}
	intake := paths[0]
	stageJSON(t, root, strings.TrimPrefix(intake, root+string(filepath.Separator)), map[string]any{"grouping_rationale": "Investigate two distinct candidate boundaries."})
	save := func(path string) {
		t.Helper()
		s7ListenerEvent(t, root, "PostToolUse", map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": path}, "tool_response": map[string]any{"filePath": path}})
	}
	save(intake)
	questions := filepath.Join(filepath.Dir(intake), "hypotheses.json")
	stageJSON(t, root, strings.TrimPrefix(questions, root+string(filepath.Separator)), []any{
		map[string]any{"statement": "The first response branch may be selected incorrectly.", "invariant": "The first boundary rejects invalid input.", "discriminator": "Inspect the first boundary.", "expected_outcomes": map[string]string{"support": "wrong first branch", "refute": "correct first branch"}, "source_finding_ids": []string{"finding-two-investigations"}, "evidence_refs": []string{"test://first"}},
		map[string]any{"statement": "The second response branch may be selected incorrectly.", "invariant": "The second boundary rejects invalid input.", "discriminator": "Inspect the second boundary.", "expected_outcomes": map[string]string{"support": "wrong second branch", "refute": "correct second branch"}, "source_finding_ids": []string{"finding-two-investigations"}, "evidence_refs": []string{"test://second"}},
	})
	save(questions)
	var packets []workpacket.HypothesisResultPacket
	if out := stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main"); json.Unmarshal([]byte(out), &packets) != nil || len(packets) != 2 {
		t.Fatalf("original two S8 issuances: %s", out)
	}
	for _, p := range packets {
		p.Draft.Answer = json.RawMessage(`{"result":"inconclusive","observed":"This particular boundary remains inconclusive."}`)
		stageJSON(t, root, p.AnswerPath, p.Draft)
	}
	return root, packets
}

func s7s8MultipleOriginalEvent(t *testing.T, root, event, task string) (string, string) {
	t.Helper()
	if event == "MainReturn" {
		return s7ListenerEvent(t, root, "PostToolUse", map[string]any{"agent_id": "Main", "tool_name": "Agent", "tool_input": map[string]any{"prompt": task}, "tool_response": map[string]any{"status": "completed"}})
	}
	return s7ListenerEvent(t, root, event, s7RegisteredReferencePayload(t, "synthetic-multiple-originals", task, event))
}

func TestS7ExplicitPluralViewListUsesEveryOriginalResponsibility(t *testing.T) {
	for _, event := range []string{"PostToolUse", "MainReturn"} {
		for _, mode := range []string{"both-known", "one-unknown", "second-background"} {
			t.Run(event+"/"+mode, func(t *testing.T) {
				root, ps := s7SameLensIssuedPairFixture(t)
				for _, p := range ps {
					s7ListenerAnswer(t, root, p)
				}
				task := "This single synthetic actor is explicitly assigned the following original responsibilities.\nS7 responsibilities:\n- `" + ps[0].ViewPath + "`\n- `" + ps[1].ViewPath + "`"
				if mode == "one-unknown" {
					task = "This single synthetic actor is explicitly assigned the following original responsibilities.\nS7 responsibilities:\n- `" + ps[0].ViewPath + "`\n- `.claude/not-issued/view.json`"
				}
				if mode == "second-background" {
					task = "This single synthetic actor is explicitly assigned the following original responsibilities.\nS7 responsibilities:\n- `" + ps[0].ViewPath + "`\nBackground comparison only:\n- `" + ps[1].ViewPath + "`"
				}
				out, diagnostic := s7s8MultipleOriginalEvent(t, root, event, task)
				a, b := minimalAnswerReviewReceipt(t, root, ps[0]), minimalAnswerReviewReceipt(t, root, ps[1])
				if !a.Accepted || b.Accepted != (mode == "both-known") {
					t.Fatalf("%s: first=%+v second=%+v; %s %s", mode, a, b, out, diagnostic)
				}
			})
		}
	}
}

func TestS8ExplicitPluralInvestigateAndViewListsUseOriginalResponsibilities(t *testing.T) {
	c := s8FixtureCatalogForTest(t)
	for _, event := range []string{"PostToolUse", "MainReturn"} {
		for _, mode := range []string{"investigate-both", "investigate-unknown", "investigate-background", "view-both", "view-unknown", "view-background"} {
			t.Run(event+"/"+mode, func(t *testing.T) {
				root, ps := s8FixtureFromCatalog(t, c)
				item := func(p workpacket.HypothesisResultPacket) string {
					return fmt.Sprintf("investigate `%s` (owner `%s`)", p.Binding.HypothesisID, p.Binding.OwnerAgentID)
				}
				intro := "This single synthetic actor is explicitly assigned the following original responsibilities.\nS8 responsibilities: "
				var task string
				switch mode {
				case "investigate-both":
					task = intro + item(ps[0]) + ", and " + item(ps[1]) + "."
				case "investigate-unknown":
					task = intro + item(ps[0]) + ", and investigate `not-issued-question` (owner `not-issued-owner`)."
				case "investigate-background":
					task = intro + item(ps[0]) + ". Background comparison only: " + item(ps[1]) + "."
				case "view-both":
					task = intro + "\n- `" + ps[0].ViewPath + "`\n- `" + ps[1].ViewPath + "`"
				case "view-unknown":
					task = intro + "\n- `" + ps[0].ViewPath + "`\n- `.claude/not-issued/view.json`"
				case "view-background":
					task = intro + "\n- `" + ps[0].ViewPath + "`\nBackground comparison only:\n- `" + ps[1].ViewPath + "`"
				}
				out, diagnostic := s7s8MultipleOriginalEvent(t, root, event, task)
				service, err := submission.New(root)
				if err != nil {
					t.Fatal(err)
				}
				a, err := service.HypothesisResultReceipt(ps[0].Binding.OwnerAgentID, ps[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				b, err := service.HypothesisResultReceipt(ps[1].Binding.OwnerAgentID, ps[1].ID)
				if err != nil {
					t.Fatal(err)
				}
				wantBoth := mode == "investigate-both" || mode == "view-both"
				if !a.Accepted || b.Accepted != wantBoth {
					t.Fatalf("%s: first=%+v second=%+v; %s %s", mode, a, b, out, diagnostic)
				}
			})
		}
	}
}

// This tests the existing singular S8 identity contract: two explicit
// investigations in a singular task conflict; mentioning the second only as
// background does not assign it. Both packets were issued by the real Writer.
func TestS8SingularOriginalDispatchDistinguishesConflictFromBackground(t *testing.T) {
	c := s8FixtureCatalogForTest(t)
	for _, event := range []string{"PreToolUse", "PostToolUse", "SubagentStop", "MainReturn"} {
		for _, mode := range []string{"explicit-conflict", "background-comparison"} {
			t.Run(event+"/"+mode, func(t *testing.T) {
				root, ps := s8FixtureFromCatalog(t, c)
				item := func(p workpacket.HypothesisResultPacket) string {
					return fmt.Sprintf("investigate `%s` (owner `%s`)", p.Binding.HypothesisID, p.Binding.OwnerAgentID)
				}
				task := "Investigate " + fmt.Sprintf("`%s` (owner `%s`)", ps[0].Binding.HypothesisID, ps[0].Binding.OwnerAgentID) + " and " + item(ps[1]) + "."
				if mode == "background-comparison" {
					task = "Investigate " + fmt.Sprintf("`%s` (owner `%s`)", ps[0].Binding.HypothesisID, ps[0].Binding.OwnerAgentID) + "; compare background " + item(ps[1]) + "."
				}
				out, diagnostic := s7s8MultipleOriginalEvent(t, root, event, task)
				service, err := submission.New(root)
				if err != nil {
					t.Fatal(err)
				}
				a, err := service.HypothesisResultReceipt(ps[0].Binding.OwnerAgentID, ps[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				b, err := service.HypothesisResultReceipt(ps[1].Binding.OwnerAgentID, ps[1].ID)
				if err != nil {
					t.Fatal(err)
				}
				if a.Accepted != (mode == "background-comparison") || b.Accepted {
					t.Fatalf("%s: first=%+v second=%+v; %s %s", mode, a, b, out, diagnostic)
				}
			})
		}
	}
}
