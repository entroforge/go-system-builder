package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceRebindRealCLI(t *testing.T) {
	fix := newRuntimeFixture(t)
	for _, args := range [][]string{{"init", "-b", "test2"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"add", "docs"}, {"commit", "-m", "base"}} {
		if out, err := runGit(t, fix.root, args...); err != nil {
			t.Fatalf("%s %v", out, err)
		}
	}
	bindFixtureWorkspace(t, fix)
	var out, stderr bytes.Buffer
	if code := runRuntimeWorkspace([]string{"bind", "--root", fix.root}, &out, &stderr); code != 0 {
		t.Fatalf("bind: %d %s", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(fix.root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err = json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	b, err := workspace.Decode(state)
	if err != nil {
		t.Fatal(err)
	}
	// Include a real registered Worker so CLI pointer recovery is exercised.
	e, err := b.Plan(context.Background(), state, "assignment-relocation", "builder", []string{"src/**"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.Executions[e.AssignmentID] = e
	if err = b.Materialize(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	b.Executions[e.AssignmentID] = e
	state["workspace"] = workspace.Encode(b)
	data, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(fix.root, ".claude/loop-state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	beforeState := append([]byte(nil), data...)
	beforeJournal, err := os.ReadFile(filepath.Join(fix.root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	old := fix.root
	moved := filepath.Join(t.TempDir(), "moved")
	req := workspace.RebindRequest{RuntimeID: workspace.RuntimeID(state), OldMainRoot: old, OldCommonDir: b.CommonDir, NewMainRoot: moved, NewCommonDir: filepath.Join(moved, ".git"), Branch: b.Branch, ExpectedHead: b.BoundHead, Paths: map[string]string{e.Path: filepath.Join(moved, ".worktrees", filepath.Base(e.Path))}}
	if err = os.Rename(old, moved); err != nil {
		t.Fatal(err)
	}
	if _, err = runGit(t, moved, "worktree", "repair", req.Paths[e.Path]); err != nil {
		t.Fatal(err)
	}
	if _, err = b.PlanRebind(context.Background(), state, req); err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Reject unrelated authority changes before any relocation is published.
	next, err := b.PlanRebind(context.Background(), state, req)
	if err != nil {
		t.Fatal(err)
	}
	statePathBefore := filepath.Join(moved, ".claude/loop-state.json")
	journalPathBefore := filepath.Join(moved, ".claude/loop-events.jsonl")
	writerBefore := runtime.NewWriter(statePathBefore, journalPathBefore, moved, semantic.RuntimeCandidateValidator{})
	sourceSnapshot := runtime.Snapshot{Revision: int(state["revision"].(float64)), State: state}
	for _, field := range []string{"integration_branch", "bound_head", "runtime_id"} {
		bad := workspace.Encode(next)
		bad[field] = "unrelated"
		if _, err := writerBefore.RelocateWorkspace(sourceSnapshot, bad, "invalid", "test"); err == nil {
			t.Fatalf("accepted field %s", field)
		}
	}
	reader := runtime.NewStore(statePathBefore, journalPathBefore)
	if _, err := reader.RelocateWorkspace(sourceSnapshot, workspace.Encode(next), "invalid", "test"); err == nil {
		t.Fatal("reader granted write authority")
	}
	stale := sourceSnapshot
	stale.Revision++
	if _, err := writerBefore.RelocateWorkspace(stale, workspace.Encode(next), "invalid", "test"); err == nil {
		t.Fatal("stale revision accepted")
	}
	for _, suffix := range []string{".commit-pending.json", ".fingerprint-pending.json", ".rollover-pending.json", ".journal-rotation-pending.json"} {
		if err := os.WriteFile(statePathBefore+suffix, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := writerBefore.RelocateWorkspace(sourceSnapshot, workspace.Encode(next), "invalid", "test"); err == nil {
			t.Fatalf("accepted unrelated pending %s", suffix)
		}
		if err := os.Remove(statePathBefore + suffix); err != nil {
			t.Fatal(err)
		}
	}
	for _, change := range []func(*workspace.RebindRequest){
		func(r *workspace.RebindRequest) { r.RuntimeID = "wrong" },
		func(r *workspace.RebindRequest) { r.ExpectedHead = "wrong" },
		func(r *workspace.RebindRequest) { r.Branch = "test3" },
		func(r *workspace.RebindRequest) { r.NewCommonDir = filepath.Join(moved, "wrong") },
		func(r *workspace.RebindRequest) { r.Paths = map[string]string{e.Path: moved} },
	} {
		bad := req
		change(&bad)
		if _, err := b.PlanRebind(context.Background(), state, bad); err == nil {
			t.Fatal("invalid relocation identity accepted")
		}
	}
	if got, _ := os.ReadFile(statePathBefore); !bytes.Equal(got, beforeState) {
		t.Fatal("rejected relocation mutated state")
	}
	if got, _ := os.ReadFile(journalPathBefore); !bytes.Equal(got, beforeJournal) {
		t.Fatal("rejected relocation mutated journal")
	}
	data, err = json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	request := filepath.Join(t.TempDir(), "request.json")
	if err = os.WriteFile(request, data, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	for attempt := 0; attempt < 2; attempt++ {
		if code := Run([]string{"runtime", "workspace", "rebind", "--root", moved, "--request", request, "--reason", "review relocation"}, bytes.NewReader(nil), &out, &stderr); code != 0 {
			t.Fatalf("valid PlanRebind but CLI returned %d: %s", code, stderr.String())
		}
	}
	statePath := filepath.Join(moved, ".claude/loop-state.json")
	journalPath := filepath.Join(moved, ".claude/loop-events.jsonl")
	afterState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	afterJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(afterJournal), []byte("\n"))
	var event, after map[string]any
	json.Unmarshal(lines[len(lines)-1], &event)
	json.Unmarshal(afterState, &after)
	digest := func(v any) string {
		data, _ := json.MarshalIndent(v, "", "  ")
		return fmt.Sprintf("%x", sha256.Sum256(append(data, '\n')))
	}
	eventBytes, _ := json.Marshal(event)
	marker := map[string]any{"schema_version": "1.0.0", "previous_state_sha256": digest(state), "previous_revision": state["revision"], "state_sha256": digest(after), "journal_event_sha256": fmt.Sprintf("%x", sha256.Sum256(append(eventBytes, '\n'))), "request_id": event["request_id"], "idempotency_key": event["idempotency_key"], "retain_last_transition": true, "state": after, "journal_event": event}
	markerBytes, _ := json.Marshal(marker)
	for _, stage := range []string{"intent", "pending-before-state", "pending-before-journal", "pending-after-journal", "partial-pointer", "pending-during-rotation", "rotation-after-commit"} {
		t.Run(stage, func(t *testing.T) {
			source := beforeState
			journal := beforeJournal
			rotating := stage == "pending-during-rotation" || stage == "rotation-after-commit"
			if stage == "pending-before-journal" || stage == "pending-after-journal" || stage == "partial-pointer" || rotating {
				source = afterState
			}
			if stage == "pending-after-journal" || stage == "partial-pointer" || rotating {
				journal = afterJournal
			}
			os.WriteFile(statePath, source, 0600)
			os.WriteFile(journalPath, journal, 0600)
			if stage != "intent" && stage != "partial-pointer" && stage != "rotation-after-commit" {
				os.WriteFile(statePath+".commit-pending.json", markerBytes, 0600)
			}
			if rotating {
				archived := append(bytes.Join(lines[:len(lines)-1], []byte("\n")), '\n')
				archivePath := fmt.Sprintf("%s.archive.%d.jsonl", journalPath, int(event["sequence"].(float64))-1)
				if err := os.WriteFile(archivePath, archived, 0600); err != nil {
					t.Fatal(err)
				}
				defer os.Remove(archivePath)
				rotation := map[string]any{"schema_version": "1.0.0", "archived_file": archivePath, "archived_sha256": fmt.Sprintf("%x", sha256.Sum256(archived)), "archived_count": len(lines) - 1, "tail_sequence": event["sequence"], "tail_event_id": event["event_id"], "started_at": "2026-09-21T00:00:00Z"}
				payload, err := json.Marshal(rotation)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(statePath+".journal-rotation-pending.json", payload, 0600); err != nil {
					t.Fatal(err)
				}
			}
			pointer := filepath.Join(req.Paths[e.Path], ".claude/loop-workspace.json")
			var coordinates map[string]any
			payload, err := os.ReadFile(pointer)
			if err != nil {
				t.Fatal(err)
			}
			json.Unmarshal(payload, &coordinates)
			coordinates["control_root"] = old
			payload, _ = json.Marshal(coordinates)
			os.WriteFile(pointer, payload, 0600)
			out.Reset()
			stderr.Reset()
			if code := Run([]string{"runtime", "workspace", "rebind", "--root", moved, "--request", request, "--reason", "recovery"}, bytes.NewReader(nil), &out, &stderr); code != 0 {
				t.Fatalf("recovery=%d %s", code, stderr.String())
			}
			writer := runtime.NewWriter(statePath, journalPath, moved, semantic.RuntimeCandidateValidator{})
			snapshot, err := writer.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			control, err := workspace.ResolveControl(context.Background(), req.Paths[e.Path])
			if err != nil || control != moved {
				t.Fatalf("pointer recovery %s %v", control, err)
			}
			if snapshot.Revision != int(state["revision"].(float64))+1 {
				t.Fatal("replayed revision")
			}
			got, _ := os.ReadFile(journalPath)
			expectedLines := len(lines)
			if rotating {
				expectedLines = 1
			}
			if len(bytes.Split(bytes.TrimSpace(got), []byte("\n"))) != expectedLines {
				t.Fatal("duplicate journal event")
			}
		})
	}
	// Recreating the old root makes the request ambiguous, even on replay.
	os.Mkdir(old, 0700)
	out.Reset()
	stderr.Reset()
	if code := Run([]string{"runtime", "workspace", "rebind", "--root", moved, "--request", request, "--reason", "ambiguous"}, bytes.NewReader(nil), &out, &stderr); code == 0 {
		t.Fatal("copied Main accepted")
	}

}
