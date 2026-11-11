package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestWorkspaceCheckReceiptIdentityUsesReversiblePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("long-", 30), "项目")
	base := filepath.Join(root, ".claude", "evidence", "workspace-checks")
	for _, tc := range []struct{ name, path, wantKind string }{
		{"canonical", filepath.Join(base, "assignment-测试", "receipt.json"), "relative-"},
		{"dot-alias", base + string(filepath.Separator) + "alias" + string(filepath.Separator) + ".." + string(filepath.Separator) + "receipt.json", "absolute-"},
		{"outside-or-other-volume", filepath.Join(t.TempDir(), "receipt.json"), "absolute-"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := workspaceCheckReceiptToken(root, tc.path)
			if !strings.HasPrefix(token, tc.wantKind) {
				t.Fatalf("token=%q want kind %s", token, tc.wantKind)
			}
			decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, tc.wantKind))
			if err != nil {
				t.Fatal(err)
			}
			restored := string(decoded)
			if tc.wantKind == "relative-" {
				restored = filepath.Join(base, restored)
			}
			if restored != tc.path {
				t.Fatalf("not reversible: %q != %q", restored, tc.path)
			}
		})
	}
	one := workspaceCheckReceiptToken(root, filepath.Join(base, "one", "receipt.json"))
	two := workspaceCheckReceiptToken(root, filepath.Join(base, "two", "receipt.json"))
	if one == two || strings.Contains(one, root) {
		t.Fatal("different receipts collided or project root leaked into relative token")
	}
	canonical := filepath.Join(base, "receipt.json")
	alias := base + string(filepath.Separator) + "alias" + string(filepath.Separator) + ".." + string(filepath.Separator) + "receipt.json"
	if workspaceCheckReceiptToken(root, canonical) == workspaceCheckReceiptToken(root, alias) {
		t.Fatal("different original path spellings collided")
	}
}

type receiptIdentityFixture struct {
	f           *runtimeFixture
	exec        workspace.Execution
	profile     checkexec.Profile
	observation checkexec.Observation
}

func newReceiptIdentityFixture(t *testing.T) receiptIdentityFixture {
	t.Helper()
	f := newRuntimeFixture(t)
	f.state["schema_version"] = "1.2.0"
	f.state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": false}
	f.addAgent("builder", "builder", "working", "workgroup", nil)
	for _, args := range [][]string{{"init", "-b", "test2"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		if _, err := runGit(t, f.root, args...); err != nil {
			t.Fatal(err)
		}
	}
	bindFixtureWorkspace(t, f)
	b, err := workspace.New(context.Background(), f.root)
	if err != nil {
		t.Fatal(err)
	}
	p := checkexec.Profile{ID: "CP-OWN", Version: "v1", CheckIndex: 0, Command: "true", Location: "worker", TimeoutSeconds: 1, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never"}
	e := workspace.Execution{AssignmentID: "assignment", RuntimeID: workspace.RuntimeID(f.state), BaselineGeneration: workspace.Generation(f.state), Generation: 1, AgentID: "builder", Path: filepath.Join(f.root, ".worktrees/worker"), Branch: "codex/worker", TargetBranch: b.Branch, Status: "preparing", CheckLocation: "worker", WritePaths: []string{}, Checks: []string{p.Command}, Inputs: map[string]string{}, CheckProfiles: []checkexec.Profile{p}}
	if err := b.Materialize(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".claude/agents", ".claude/skills"} {
		if err := os.MkdirAll(filepath.Join(f.root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Bootstrap(e, executable); err != nil {
		t.Fatal(err)
	}
	e.Status = "ready"
	b.Executions[e.AssignmentID] = e
	f.state["workspace"] = workspace.Encode(b)
	f.persist(t)
	observation, err := checkexec.ObserveProfile(context.Background(), e.Path, p)
	if err != nil {
		t.Fatal(err)
	}
	return receiptIdentityFixture{f: f, exec: e, profile: p, observation: observation}
}

// capturedReceipt is a synthetic runner result for Writer identity tests; it
// does not claim an isolated command ran. Real runner execution is verified by
// the separate S6 integration test on a host with isolation capability.
func (f receiptIdentityFixture) capturedReceipt(t *testing.T, name, status string) checkexec.ToolReceipt {
	t.Helper()
	dir := filepath.Join(f.f.root, ".claude/evidence/workspace-checks", name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "output.log")
	if err := os.WriteFile(log, []byte("synthetic receipt identity fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := checkexec.SealReceipt(map[string]any{"version": 2, "command": f.profile.Command, "status": status, "output": log, "profile_observation": f.observation, "check_binding": workspace.CheckBinding(f.exec)})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "receipt.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var ref checkexec.ToolReceipt
	ctx := checkexec.WithReceiptCapture(checkexec.WithBinding(checkexec.WithProfile(context.Background(), f.profile), workspace.CheckBinding(f.exec)), func(r checkexec.ToolReceipt) { ref = r })
	if err := checkexec.CaptureReceipt(ctx, path, f.exec.Path, data); err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestWorkspaceCheckReceiptWriterRetainsDistinctAndConcurrentRuns(t *testing.T) {
	fixture := newReceiptIdentityFixture(t)
	refs := []checkexec.ToolReceipt{fixture.capturedReceipt(t, "one", "pass"), fixture.capturedReceipt(t, "two", "fail"), fixture.capturedReceipt(t, "three", "pass"), fixture.capturedReceipt(t, "four", "fail"), fixture.capturedReceipt(t, "five", "pass")}
	writer := runtime.NewWriter(fixture.f.statePath, fixture.f.journalPath, fixture.f.root, semantic.RuntimeCandidateValidator{})
	before, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := acceptWorkspaceToolReceipt(fixture.f.root, fixture.exec, refs[0]); err != nil {
		t.Fatal(err)
	}
	if err := acceptWorkspaceToolReceipt(fixture.f.root, fixture.exec, refs[1]); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, ref := range []checkexec.ToolReceipt{refs[2], refs[2]} {
		wg.Add(1)
		go func(ref checkexec.ToolReceipt) {
			defer wg.Done()
			errs <- acceptWorkspaceToolReceipt(fixture.f.root, fixture.exec, ref)
		}(ref)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	errs = make(chan error, 2)
	for _, ref := range refs[3:] {
		wg.Add(1)
		go func(ref checkexec.ToolReceipt) {
			defer wg.Done()
			errs <- acceptWorkspaceToolReceipt(fixture.f.root, fixture.exec, ref)
		}(ref)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	next, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := workspace.Decode(next.State)
	if err != nil {
		t.Fatal(err)
	}
	got := registry.Executions[fixture.exec.AssignmentID].ToolCheckReceipts
	if len(got) != len(refs) || next.Revision != before.Revision+len(refs) {
		t.Fatalf("want %d captured receipts and commits: receipts=%d revision=%d/%d", len(refs), len(got), next.Revision, before.Revision)
	}
	if fmt.Sprint(next.State["lifecycle"]) != fmt.Sprint(before.State["lifecycle"]) {
		t.Fatal("receipt acceptance changed stage")
	}
	journal, err := os.ReadFile(fixture.f.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ValidateCheckpointIndices(fixture.f.root, next.State, journal); err != nil {
		t.Fatalf("checkpoint pairing: %v", err)
	}
	for _, ref := range refs {
		if err := acceptWorkspaceToolReceipt(fixture.f.root, fixture.exec, ref); err != nil {
			t.Fatal(err)
		}
	}
	replay, err := writer.Snapshot()
	if err != nil || replay.Revision != next.Revision {
		t.Fatalf("replay rewrote state: revision %d/%d err=%v", replay.Revision, next.Revision, err)
	}
}

func TestWorkspaceCheckReceiptHistoricalJournalReplayAndPending(t *testing.T) {
	fixture := newReceiptIdentityFixture(t)
	oldRef := fixture.capturedReceipt(t, "historical", "pass")
	writer := runtime.NewWriter(fixture.f.statePath, fixture.f.journalPath, fixture.f.root, semantic.RuntimeCandidateValidator{})
	before, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	// The recorded old-format event is a synthetic historical fixture. It is
	// not recalculated from a receipt and contains no new product digest.
	oldEvent, oldKey := workspaceEventIdentity(fixture.exec.RuntimeID, "tool-check", fixture.exec.AssignmentID, fixture.exec.AgentID, fixture.exec.Generation, "0123456789abcdef", "")
	_, err = writer.Update(before.Revision, runtime.Mutation{EventID: oldEvent, TransitionID: "WORKSPACE-CHECK", Event: "workspace_check_received", Actor: fixture.exec.AgentID, RuntimeID: fixture.exec.RuntimeID, IdempotencyKey: oldKey, RetainLastTransition: true, ProducerResponsibility: "owned_check", Apply: func(state map[string]any) error {
		b, err := workspace.Decode(state)
		if err != nil {
			return err
		}
		e := b.Executions[fixture.exec.AssignmentID]
		if err := b.AcceptToolReceipt(&e, oldRef); err != nil {
			return err
		}
		b.Executions[e.AssignmentID] = e
		state["workspace"] = workspace.Encode(b)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	afterOld, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := acceptWorkspaceToolReceipt(fixture.f.root, fixture.exec, oldRef); err != nil {
		t.Fatalf("old registered receipt replay: %v", err)
	}
	replay, err := writer.Snapshot()
	if err != nil || replay.Revision != afterOld.Revision {
		t.Fatalf("old replay wrote: %d/%d %v", replay.Revision, afterOld.Revision, err)
	}

	newRef := fixture.capturedReceipt(t, "pending-new", "fail")
	prevState, err := os.ReadFile(fixture.f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	prevJournal, err := os.ReadFile(fixture.f.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := acceptWorkspaceToolReceipt(fixture.f.root, fixture.exec, newRef); err != nil {
		t.Fatal(err)
	}
	next, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	nextJournal, err := os.ReadFile(fixture.f.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	lines := strings.Split(strings.TrimSpace(string(nextJournal)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &event); err != nil {
		t.Fatal(err)
	}
	marker := map[string]any{"schema_version": "1.0.0", "previous_revision": afterOld.Revision, "request_id": event["request_id"], "idempotency_key": event["idempotency_key"], "retain_last_transition": true, "state": next.State, "journal_event": event}
	markerData, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.f.statePath, prevState, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.f.journalPath, prevJournal, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.f.statePath+".commit-pending.json", markerData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := acceptWorkspaceToolReceipt(fixture.f.root, fixture.exec, newRef); err != nil {
		t.Fatalf("pending recovery and replay: %v", err)
	}
	recovered, err := writer.Snapshot()
	if err != nil || recovered.Revision != next.Revision {
		t.Fatalf("pending did not restore original winner: %d/%d %v", recovered.Revision, next.Revision, err)
	}
	currentJournal, err := os.ReadFile(fixture.f.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(currentJournal) != string(nextJournal) {
		t.Fatal("pending recovery did not retain exact original event")
	}
}
