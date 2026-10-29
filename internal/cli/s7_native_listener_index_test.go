package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func s7ListenerSibling(t *testing.T, root string) workpacket.ReviewResultPacket {
	t.Helper()
	markAssignmentDispatched(t, root, "assignment-qa-1", "reviewer-listener-sibling")
	current := stageSnapshot(t, root)
	for _, raw := range current.State["entities"].(map[string]any)["agents"].([]any) {
		raw.(map[string]any)["dispatch_mode"] = "plan_checkpoint"
	}
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	var packets []workpacket.ReviewResultPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root, "--agent", "Main")), &packets); err != nil {
		t.Fatal(err)
	}
	for _, p := range packets {
		if p.Binding.OwnerAgentID == "reviewer-listener-sibling" {
			return p
		}
	}
	t.Fatal("missing sibling packet")
	return workpacket.ReviewResultPacket{}
}

func s7ListenerNoncanonicalBody(t *testing.T, root string, state map[string]any, packetID, title string) any {
	t.Helper()
	ctx := packetbody.WithRoot(t.Context(), root)
	for _, raw := range state["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["packet_id"] != packetID {
			continue
		}
		if _, external, err := packetbody.ReferenceFromIndex(row); err != nil || !external {
			t.Fatalf("expected external packet body for %s: external=%t err=%v", packetID, external, err)
		}
		body, err := packetbody.Read(ctx, row)
		if err != nil {
			t.Fatal(err)
		}
		body["draft"].(map[string]any)["title"] = title
		index, err := packetbody.Publish(ctx, root, body)
		if err != nil {
			t.Fatal(err)
		}
		// Keep the responsibility index intact while publishing a readable,
		// digest-valid body that fails the original canonical draft check.
		originalRef := row["body_ref"]
		row["body_ref"] = index["body_ref"]
		return originalRef
	}
	t.Fatalf("missing packet %s", packetID)
	return nil
}

func s7ListenerRestoreBodyRef(t *testing.T, state map[string]any, packetID string, originalRef any) {
	t.Helper()
	for _, raw := range state["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["packet_id"] == packetID {
			row["body_ref"] = originalRef
			return
		}
	}
	t.Fatalf("missing packet %s", packetID)
}

// A different packet's body is unrelated only when none of its paths requires
// an exemption from the actual Main worktree drift scan.
func TestS7NativeListenerIgnoresUnrelatedNoncanonicalBody(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		t.Run(event, func(t *testing.T) {
			root, own := s7ListenerFixture(t)
			sibling := s7ListenerSibling(t, root)
			s7ListenerAnswer(t, root, own)
			// Retain the sibling's issued draft outside Main to model a question
			// whose authoring file is not present in this actual worktree. The
			// selected original remains untracked and must pass its normal proof.
			retained := filepath.Join(t.TempDir(), "sibling-answer.json")
			if err := os.Rename(filepath.Join(root, sibling.AnswerPath), retained); err != nil {
				t.Fatal(err)
			}
			current := stageSnapshot(t, root)
			s7ListenerNoncanonicalBody(t, root, current.State, sibling.ID, "noncanonical sibling title")
			stageJSON(t, root, ".claude/loop-state.json", current.State)
			out, diagnostic := s7ListenerEvent(t, root, event, s7ListenerPayload(t, own.Binding.OwnerAgentID, "Review "+filepath.Join(root, own.ViewPath)))
			if !strings.Contains(out, "accepted=true") || !strings.Contains(out, own.ID) {
				t.Fatalf("unrelated body erased current responsibility: %s %s", out, diagnostic)
			}
			accepted := stageSnapshot(t, root)
			assignments := accepted.State["review"].(map[string]any)["assignments"].(map[string]any)
			if assignments[own.Binding.AssignmentID].(map[string]any)["result_receipt"] == nil || assignments[sibling.Binding.AssignmentID].(map[string]any)["result_receipt"] != nil {
				t.Fatal("Hook did not receive exactly the selected original")
			}
			if _, err := os.Stat(retained); err != nil {
				t.Fatalf("unrelated original draft was not retained: %v", err)
			}
		})
	}
}

func TestS7NativeListenerRestoresSiblingSourceNeededForDriftProof(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		t.Run(event, func(t *testing.T) {
			root, prior := s7ListenerFixture(t)
			pending := s7ListenerSibling(t, root)
			s7ListenerAnswer(t, root, prior)
			out, diagnostic := s7ListenerEvent(t, root, event, s7ListenerPayload(t, prior.Binding.OwnerAgentID, "Review "+filepath.Join(root, prior.ViewPath)))
			if !strings.Contains(out, "accepted=true") {
				t.Fatalf("first original not received: %s %s", out, diagnostic)
			}
			before := stageSnapshot(t, root)
			priorReceipt := before.State["review"].(map[string]any)["assignments"].(map[string]any)[prior.Binding.AssignmentID].(map[string]any)["result_receipt"]
			if priorReceipt == nil {
				t.Fatal("first original receipt missing")
			}
			s7ListenerAnswer(t, root, pending)
			pendingBytes, err := os.ReadFile(filepath.Join(root, pending.AnswerPath))
			if err != nil {
				t.Fatal(err)
			}
			originalRef := s7ListenerNoncanonicalBody(t, root, before.State, prior.ID, "noncanonical accepted sibling title")
			stageJSON(t, root, ".claude/loop-state.json", before.State)
			payload := s7ListenerPayload(t, pending.Binding.OwnerAgentID, "Review "+filepath.Join(root, pending.ViewPath))
			out, diagnostic = s7ListenerEvent(t, root, event, payload)
			for _, want := range []string{pending.ID, pending.Binding.OwnerAgentID, pending.AnswerPath, prior.ID, "canonical", "marked stale", "runtime review-plan revive", "rework/new-round"} {
				if !strings.Contains(out, want) {
					t.Fatalf("missing source failure or recovery %q: %s %s", want, out, diagnostic)
				}
			}
			if strings.Contains(out, "accepted=true") || strings.Contains(out, "must be re-planned") || strings.Contains(out, "Correct the specified professional fields") {
				t.Fatalf("system source failure misreported: %s %s", out, diagnostic)
			}
			stale := stageSnapshot(t, root)
			reviewState := stale.State["review"].(map[string]any)
			assignments := reviewState["assignments"].(map[string]any)
			if reviewState["plan"].(map[string]any)["status"] != "stale" || !reflect.DeepEqual(priorReceipt, assignments[prior.Binding.AssignmentID].(map[string]any)["result_receipt"]) || assignments[pending.Binding.AssignmentID].(map[string]any)["result_receipt"] != nil {
				t.Fatal("source failure did not preserve the prior receipt and pending responsibility")
			}
			// A repeated observation of the same stale plan still gives the
			// original recovery entry without recording another stale event.
			repeated, repeatedDiagnostic := s7ListenerEvent(t, root, event, payload)
			if !strings.Contains(repeated, "runtime review-plan revive") || stageSnapshot(t, root).Revision != stale.Revision {
				t.Fatalf("stale replay lost recovery or changed Runtime: %s %s", repeated, repeatedDiagnostic)
			}
			// Restore only the original source reference; retain the stale event,
			// prior receipt, same Plan and the pending author's actual bytes.
			s7ListenerRestoreBodyRef(t, stale.State, prior.ID, originalRef)
			stageJSON(t, root, ".claude/loop-state.json", stale.State)
			stageCLI(t, "runtime", "review-plan", "revive", "--root", root)
			revived := stageSnapshot(t, root)
			if revived.State["review"].(map[string]any)["plan"].(map[string]any)["status"] != "running" || !reflect.DeepEqual(priorReceipt, revived.State["review"].(map[string]any)["assignments"].(map[string]any)[prior.Binding.AssignmentID].(map[string]any)["result_receipt"]) {
				t.Fatal("revive did not preserve the unchanged Plan and prior receipt")
			}
			if actual, err := os.ReadFile(filepath.Join(root, pending.AnswerPath)); err != nil || !bytes.Equal(actual, pendingBytes) {
				t.Fatalf("recovery changed the pending original answer: %v", err)
			}
			out, diagnostic = s7ListenerEvent(t, root, event, payload)
			if !strings.Contains(out, "accepted=true") || !strings.Contains(out, pending.ID) {
				t.Fatalf("original answer was not accepted after source recovery and revive: %s %s", out, diagnostic)
			}
			accepted := stageSnapshot(t, root)
			assignments = accepted.State["review"].(map[string]any)["assignments"].(map[string]any)
			if !reflect.DeepEqual(priorReceipt, assignments[prior.Binding.AssignmentID].(map[string]any)["result_receipt"]) || assignments[pending.Binding.AssignmentID].(map[string]any)["result_receipt"] == nil {
				t.Fatal("recovery lost the prior receipt or did not accept the pending original")
			}
		})
	}
}

func TestS7NativeListenerReportsSelectedCanonicalBodyFailure(t *testing.T) {
	root, own := s7ListenerFixture(t)
	current := stageSnapshot(t, root)
	s7ListenerNoncanonicalBody(t, root, current.State, own.ID, "noncanonical selected title")
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	before := current.Revision
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		out, diagnostic := s7ListenerEvent(t, root, event, s7ListenerPayload(t, own.Binding.OwnerAgentID, "Review "+filepath.Join(root, own.ViewPath)))
		for _, want := range []string{own.ID, own.Binding.OwnerAgentID, own.AnswerPath, "canonical", "Main must restore"} {
			if !strings.Contains(out, want) {
				t.Fatalf("selected body failure lost %q: %s %s", want, out, diagnostic)
			}
		}
		if strings.Contains(out, "Correct the specified professional fields") || stageSnapshot(t, root).Revision != before {
			t.Fatal("system body failure became author work or reception")
		}
	}
	// An unrelated helper has no original responsibility to diagnose.
	out, diagnostic := s7ListenerEvent(t, root, "PreToolUse", s7ListenerPayload(t, "unrelated-helper", "Read an unrelated file"))
	if out != "" || diagnostic != "" {
		t.Fatalf("helper inherited another actor's body failure: %s %s", out, diagnostic)
	}
}

func TestS7NativeListenerReportsSharedIndexFailure(t *testing.T) {
	root, own := s7ListenerFixture(t)
	current := stageSnapshot(t, root)
	entities := current.State["entities"].(map[string]any)
	entities["work_packets"] = append(entities["work_packets"].([]any), map[string]any{"packet_id": own.ID, "template_id": "s5-document-review-v1"})
	stageJSON(t, root, ".claude/loop-state.json", current.State)
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		out, diagnostic := s7ListenerEvent(t, root, event, s7ListenerPayload(t, own.Binding.OwnerAgentID, "Review "+filepath.Join(root, own.ViewPath)))
		for _, want := range []string{"index is unavailable", "Main must restore", "duplicate Runtime work packet ID"} {
			if !strings.Contains(out, want) {
				t.Fatalf("shared index failure swallowed %q: %s %s", want, out, diagnostic)
			}
		}
		if stageSnapshot(t, root).Revision != current.Revision {
			t.Fatal("invalid shared index received an answer")
		}
	}
}
