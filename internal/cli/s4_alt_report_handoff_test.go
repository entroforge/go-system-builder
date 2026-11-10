package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS4NaturalSaveWithOccupiedReportPreparesBothOriginalS5Responsibilities(t *testing.T) {
	root, packet := s4HandoffFixture(t)
	preferred := ".claude/evidence/submissions/" + packet.ID + "/answer.json"
	occupied := filepath.Join(root, preferred)
	unknown := []byte("unknown report owner; preserve these original bytes\n")
	if err := os.MkdirAll(filepath.Dir(occupied), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(occupied, unknown, 0600); err != nil {
		t.Fatal(err)
	}
	stageJSON(t, root, packet.AnswerPath, completedTaskPlanningDraft(packet))
	body, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse", "session_id": "s4-alternate-report-natural-save", "cwd": root,
		"tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, packet.AnswerPath)}, "tool_response": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PostToolUse", "--root", root}, bytes.NewReader(body), &out, &diag); code != 0 {
		t.Fatalf("natural S4 save failed %d: %s %s", code, &out, &diag)
	}
	if got := mustStageFile(t, occupied); !bytes.Equal(got, unknown) {
		t.Fatalf("unknown preferred report changed: %q", got)
	}
	snap := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(snap.State, packet.ID)
	if err != nil || !found || record.ControlStatus != "advanced" || record.TransitionEventID == "" {
		t.Fatalf("original S4 reception and TR-002 did not complete: %+v %v; %s %s", record, err, &out, &diag)
	}
	rows, _ := snap.State["evidence"].([]any)
	var registered string
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] == record.EvidenceID {
			registered, _ = row["path"].(string)
		}
	}
	if !strings.HasPrefix(registered, ".claude/evidence-alt-") {
		t.Fatalf("occupied report did not select an actual alternate root: %q", registered)
	}
	report, err := runtime.ReadTaskPlanningReport(root, registered)
	if err != nil || bytes.Equal(report, unknown) {
		t.Fatalf("registered alternate report unavailable: %s %v", registered, err)
	}
	var envelope struct {
		EvidenceID string `json:"evidence_id"`
		Kind       string `json:"kind"`
	}
	if err := json.Unmarshal(report, &envelope); err != nil || envelope.EvidenceID != record.EvidenceID || envelope.Kind != "planning_task" {
		t.Fatalf("alternate report does not belong to the original reception: %+v %v", envelope, err)
	}
	if snap.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Fatalf("natural S4 outlet did not enter S5: %s %s", &out, &diag)
	}
	packets, err := runtime.S5Packets(root, snap.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("alternate report did not prepare both S5 packets: count=%d err=%v; %s %s", len(packets), err, &out, &diag)
	}
	responsibilities := map[string]bool{}
	owners, ids, paths := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, p := range packets {
		if ids[p.ID] || owners[p.Binding.Owner] || responsibilities[p.Binding.Responsibility] {
			t.Fatalf("S5 packets do not have independent original identities: %+v", packets)
		}
		ids[p.ID], owners[p.Binding.Owner], responsibilities[p.Binding.Responsibility] = true, true, true
		if _, found, err := runtime.FindWorkPacket(snap.State, p.ID); err != nil || !found {
			t.Fatalf("S5 responsibility not actually issued: %s %v", p.ID, err)
		}
		for _, path := range []string{p.ViewPath, p.AnswerPath} {
			if path == "" || paths[path] {
				t.Fatalf("S5 packet file missing or shared: %q", path)
			}
			paths[path] = true
			data := mustStageFile(t, filepath.Join(root, path))
			if len(data) == 0 || !json.Valid(data) {
				t.Fatalf("issued S5 packet has no readable original JSON: %s", path)
			}
		}
		for _, ref := range []string{p.Binding.Owner, p.Binding.Responsibility, filepath.Join(root, p.ViewPath), filepath.Join(root, p.AnswerPath), "source root"} {
			if ref == "" || !strings.Contains(out.String(), ref) {
				t.Fatalf("natural handoff omitted actual S5 dispatch reference %q: %s", ref, &out)
			}
		}
	}
	for _, responsibility := range []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"} {
		if !responsibilities[responsibility] {
			t.Fatalf("original S5 responsibility not prepared: %s", responsibility)
		}
	}
}
