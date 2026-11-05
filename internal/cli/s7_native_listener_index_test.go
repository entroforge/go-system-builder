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

func s7ListenerAlternateBody(t *testing.T, root string, state map[string]any, packetID, title string) any {
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
		// Give the deliberately changed test body its own fixture path. The
		// original packet-ID path is immutable and no longer content-addressed.
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		ref := packetbody.Reference{Format: packetbody.BodyFormat, Path: packetbody.Directory + "p/fixture/noncanonical.json"}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, ref.Path)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ref.Path), data, 0600); err != nil {
			t.Fatal(err)
		}
		index := packetbody.Index(body, ref)
		// Keep the responsibility index intact while pointing to a distinct
		// readable body for an unrelated issued responsibility.
		originalRef := row["body_ref"]
		row["body_ref"] = index["body_ref"]
		return originalRef
	}
	t.Fatalf("missing packet %s", packetID)
	return nil
}

// A different packet's body is not a proof obligation for this responsibility.
func TestS7NativeListenerIgnoresUnrelatedAlternateBody(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		t.Run(event, func(t *testing.T) {
			root, own := s7ListenerFixture(t)
			sibling := s7ListenerSibling(t, root)
			s7ListenerAnswer(t, root, own)
			// Retain the sibling's issued draft outside Main to model a question
			// whose authoring file is not present in this actual worktree. The
			// selected Reviewer's original answer remains independently receivable.
			retained := filepath.Join(t.TempDir(), "sibling-answer.json")
			if err := os.Rename(filepath.Join(root, sibling.AnswerPath), retained); err != nil {
				t.Fatal(err)
			}
			current := stageSnapshot(t, root)
			s7ListenerAlternateBody(t, root, current.State, sibling.ID, "noncanonical sibling title")
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

func TestS7NativeListenerDoesNotReauthenticateAcceptedSibling(t *testing.T) {
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
			// An unrelated prior packet body is not a new proof obligation for
			// the pending Reviewer's original responsibility.
			s7ListenerAlternateBody(t, root, before.State, prior.ID, "changed accepted sibling title")
			stageJSON(t, root, ".claude/loop-state.json", before.State)
			payload := s7ListenerPayload(t, pending.Binding.OwnerAgentID, "Review "+filepath.Join(root, pending.ViewPath))
			out, diagnostic = s7ListenerEvent(t, root, event, payload)
			if !strings.Contains(out, "accepted=true") || !strings.Contains(out, pending.ID) || strings.Contains(out, "marked stale") {
				t.Fatalf("unrelated prior body blocked the pending original: %s %s", out, diagnostic)
			}
			after := stageSnapshot(t, root)
			assignments := after.State["review"].(map[string]any)["assignments"].(map[string]any)
			if !reflect.DeepEqual(priorReceipt, assignments[prior.Binding.AssignmentID].(map[string]any)["result_receipt"]) || assignments[pending.Binding.AssignmentID].(map[string]any)["result_receipt"] == nil {
				t.Fatal("prior receipt was changed or pending answer was not accepted")
			}
			if actual, err := os.ReadFile(filepath.Join(root, pending.AnswerPath)); err != nil || !bytes.Equal(actual, pendingBytes) {
				t.Fatalf("reception changed the pending original answer: %v", err)
			}
		})
	}
}

func TestS7NativeListenerReportsSelectedUnreadableBody(t *testing.T) {
	root, own := s7ListenerFixture(t)
	s7ListenerAnswer(t, root, own)
	current := stageSnapshot(t, root)
	found := false
	for _, raw := range current.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["packet_id"] != own.ID {
			continue
		}
		ref, external, err := packetbody.ReferenceFromIndex(row)
		if err != nil || !external {
			t.Fatalf("expected original packet body: external=%v err=%v", external, err)
		}
		if err := os.WriteFile(filepath.Join(root, ref.Path), []byte("{"), 0600); err != nil {
			t.Fatal(err)
		}
		found = true
	}
	if !found {
		t.Fatal("selected packet index missing")
	}
	before := current.Revision
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		out, diagnostic := s7ListenerEvent(t, root, event, s7ListenerPayload(t, own.Binding.OwnerAgentID, "Review "+filepath.Join(root, own.ViewPath)))
		for _, want := range []string{own.ID, own.Binding.OwnerAgentID, own.AnswerPath, "Main must restore"} {
			if !strings.Contains(out, want) {
				t.Fatalf("selected unreadable original lost %q: %s %s", want, out, diagnostic)
			}
		}
		if strings.Contains(out, "Correct the specified professional fields") || stageSnapshot(t, root).Revision != before {
			t.Fatal("system body failure became author work or reception")
		}
	}
	out, diagnostic := s7ListenerEvent(t, root, "PreToolUse", s7ListenerPayload(t, "unrelated-helper", "Read an unrelated file"))
	// R-16: the actor's own model context stays silent. The unresolved
	// original responsibility is a Main-facing diagnostic only, so the helper
	// inherits neither the actor channel nor another actor's packet, owner or
	// answer path.
	if out != "" {
		t.Fatalf("helper inherited another actor's body failure in the actor channel: %s %s", out, diagnostic)
	}
	if !strings.Contains(diagnostic, "Main must inspect the original dispatch") {
		t.Fatalf("helper's unresolved original responsibility was not reported to Main: %s %s", out, diagnostic)
	}
	for _, foreign := range []string{own.ID, own.Binding.OwnerAgentID, own.AnswerPath} {
		if strings.Contains(out, foreign) || strings.Contains(diagnostic, foreign) {
			t.Fatalf("helper inherited another actor's body failure: %s %s", out, diagnostic)
		}
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
