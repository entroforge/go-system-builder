package investigation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// This denial-only fixture uses the original S9 approval/dispatch/PlanReport
// and workspace prepare paths. Its authored red command is mechanical test
// input; it is not a claim that a model or Worker completed professional work.
func TestS9WorkerIsolationPreflightRequiresExplicitChecksNotProfessionalRedChecks(t *testing.T) {
	fixture, plan, owners := prepareS9RepairPlanReportHookFixtureWithSetup(t, installS9WorkerBootstrapInputs)
	if len(plan.Assignments) != 2 || len(owners) != 2 {
		t.Fatalf("original S9 fixture assignments/owners = %d/%d, want two", len(plan.Assignments), len(owners))
	}

	const redCommand = "test -f internal/api/payload.go"
	var issueOut, issueErr bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", fixture.root}, nil, &issueOut, &issueErr); code != 0 {
		t.Fatalf("issue original sibling PlanReports: code=%d stderr=%s stdout=%s", code, issueErr.String(), issueOut.String())
	}
	var packets []workpacket.RepairPlanReportPacket
	if err := json.Unmarshal(issueOut.Bytes(), &packets); err != nil || len(packets) != 2 {
		t.Fatalf("decode original sibling PlanReports: packets=%d err=%v stdout=%s", len(packets), err, issueOut.String())
	}
	for _, packet := range packets {
		answerPath := writeS9RepairPlanReportForResult(t, fixture.root, packet, redCommand)
		receipt := runS9PostToolUse(t, fixture, packet.Binding.OwnerAgentID, fixture.root, answerPath)
		if !receipt.Accepted || receipt.PacketID != packet.ID || receipt.WorkComplete {
			t.Fatalf("original S9 PlanReport Hook receipt = %+v", receipt)
		}
	}

	snapshot := currentS9Snapshot(t, fixture)
	started, err := repair.BeginRepairExecution(fixture.root, fixture.statePath, fixture.journalPath, repair.BeginRepairExecutionRequest{
		RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: snapshot.Revision, Actor: "main-session"},
	})
	if err != nil {
		t.Fatalf("original S9 Begin after complete sibling PlanReport coverage: %v", err)
	}
	if life, _ := started.State["lifecycle"].(map[string]any); life == nil || life["phase"] != "fixing" {
		t.Fatalf("original S9 Begin did not enter fixing: %v", started.State["lifecycle"])
	}

	owner := owners[0]
	if code := runS9Command(t, fixture.root, "runtime", "workspace", "bind", "--root", fixture.root); code != 0 {
		t.Fatal("original Main workspace bind failed")
	}

	// Use the original source association, explicitly configured fixture check and
	// PreflightChecks API as workspace prepare. The probe has no Runtime/Git
	// side effect; when this host supports isolation, this denial-only fixture
	// is explicitly skipped rather than claimed as Worker-success coverage.
	bound := currentS9Snapshot(t, fixture)
	registry, err := workspace.Decode(bound.State)
	if err != nil || registry == nil {
		t.Fatalf("decode original bound Main workspace: registry=%v err=%v", registry, err)
	}
	authority, err := repair.ResolveWorkspaceAuthority(fixture.root, bound.State, owner.manifestAssignmentID, owner.agentID)
	if err != nil {
		t.Fatalf("resolve original S9 Worker authority: %v", err)
	}
	checks := []string{"test -d internal/api"} // Actual fixture execution configuration, independent of prechecks.
	profiles, err := runtime.RepairResultCheckProfiles(authority.ReportRef.SHA256, checks, "worker")
	if err != nil {
		t.Fatalf("prepare explicitly configured fixture Worker profiles: %v", err)
	}
	probeExecution := workspace.Execution{CheckLocation: "worker", Checks: checks, CheckProfiles: profiles}
	probeErr := registry.PreflightChecks(context.Background(), probeExecution, fixture.root)
	if probeErr == nil {
		t.Skip("the original Worker isolation probe passes on this host; this denial-only test does not claim the positive Worker matrix")
	}
	if !strings.Contains(probeErr.Error(), "Worker check capability unavailable") {
		t.Fatalf("original Worker probe failed outside its named capability gate: %v", probeErr)
	}
	t.Logf("original isolation capability probe refused: %v", probeErr)

	// The configured execution is denied before any Writer, worktree or product
	// mutation. A professional precheck is not implicit execution configuration.
	stateBefore := mustRead(t, fixture.statePath)
	journalBefore := mustRead(t, fixture.journalPath)
	headBefore, err := workspace.Git(context.Background(), fixture.root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.PreflightChecks(context.Background(), probeExecution, fixture.root); err == nil {
		t.Fatal("explicitly configured Worker checks bypassed unavailable isolation")
	}
	assertS9RuntimeBytesUnchanged(t, fixture, stateBefore, journalBefore, "denied actual Worker check preflight")
	if head, err := workspace.Git(context.Background(), fixture.root, "rev-parse", "HEAD"); err != nil || head != headBefore {
		t.Fatalf("denied preflight changed Main HEAD: %q %v", head, err)
	}
	var prepareOut, prepareErr bytes.Buffer
	code := cli.Run([]string{"runtime", "workspace", "prepare", "--root", fixture.root,
		"--assignment", owner.manifestAssignmentID, "--agent", owner.agentID, "--check-location", "worker"},
		nil, &prepareOut, &prepareErr)
	if code != 0 {
		t.Fatalf("prepare without configured commands must not invent a Worker check: %d %s", code, prepareErr.String())
	}
	var response struct {
		Execution workspace.Execution `json:"execution"`
	}
	if err := json.Unmarshal(prepareOut.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	execution := response.Execution
	if execution.AssignmentID != owner.manifestAssignmentID || len(execution.Checks) != 0 || len(execution.CheckProfiles) != 0 || len(execution.ToolCheckReceipts) != 0 || len(execution.OwnedCheckReceipts) != 0 {
		t.Fatalf("professional red checks became configured or allegedly executed checks: %+v", execution)
	}
	if _, err := os.Stat(filepath.Join(execution.Path, "internal/api/payload.go")); !os.IsNotExist(err) {
		t.Fatalf("prepare performed product work: %v", err)
	}
	if packets, err := runtime.RepairResultPacketsContext(runtime.WithPacketBodyRoot(t.Context(), fixture.root), currentS9Snapshot(t, fixture).State); err != nil || len(packets) == 0 {
		t.Fatalf("prepare should supply only the next professional Result questionnaire: %d %v", len(packets), err)
	}
}
