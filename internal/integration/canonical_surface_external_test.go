package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/buildsurface"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/impact"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/review"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// TestCanonicalSurfaceUsesRealTR003ThroughS7Registration joins the original
// S5 evidence, TR-003 Writer, canonical S6 writers and Main integration to
// the buildsurface-backed S7 Draft/Register path. The S5 evidence here is a
// mechanism fixture; its assertions are not product verification claims.
func TestCanonicalSurfaceUsesRealTR003ThroughS7Registration(t *testing.T) {
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, seedCanonicalS5AndApplyTR003)
	ctx := context.Background()
	privateAnswer := filepath.Join(fixture.root, ".claude", "evidence", canonicalTeamID, canonicalTaskID, "msg-s6-main-result-1.json")
	if privateAnswer == filepath.Join(fixture.root, filepath.FromSlash(fixture.result.ResultRef)) {
		t.Fatal("fixture author answer and accepted Result must be distinct files")
	}
	if err := os.WriteFile(privateAnswer, []byte("author changed private answer after receipt"), 0600); err != nil {
		t.Fatal(err)
	}

	inspection := fixture.inspect(t, "")
	if !inspection.Ready || len(inspection.ChangedPaths) != 1 || inspection.ChangedPaths[0] != canonicalSourceFile {
		t.Fatalf("canonical Main Inspect = ready %t paths %v blockers %v", inspection.Ready, inspection.ChangedPaths, inspection.Blockers)
	}
	integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig())
	if err != nil || integrated.Checkpoint.State != integration.StateVerified {
		t.Fatalf("original Main Integrate = state %q err %v", integrated.Checkpoint.State, err)
	}

	if err := os.Remove(privateAnswer); err != nil {
		t.Fatal(err)
	}
	current := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	surface, err := buildsurface.Read(fixture.root, current.State)
	if err != nil {
		t.Fatalf("canonical buildsurface.Read after real Main: %v", err)
	}
	if len(surface.Tasks) != 1 || surface.Tasks[0].ID != canonicalTaskID || len(surface.Paths) != 1 || surface.Paths[0].Path != canonicalSourceFile || len(surface.Contributions) != 1 {
		t.Fatalf("canonical product surface = %+v; expected current TASK, one Main path/contribution", surface)
	}

	// The original TR-006 consumer starts S7's first review round. It consumes
	// the real current completion and evaluates the unchanged original Gate.
	catalog, err := transition.LoadCatalog(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := fileview.New(fixture.root, "refs/heads/main", catalog.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := qualitygate.NewEvaluator(registry).Evaluate(ctx, qualitygate.Input{
		Root: fixture.root, Snapshot: current, Files: files,
		GateID: "GATE-BUILDER-BATCH-READY", TransitionID: "TR-006",
	})
	if err != nil || gate.Status != qualitygate.StatusSatisfied {
		t.Fatalf("original TR-006 Gate = status %q missing %v conflicts %v err %v", gate.Status, gate.Missing, gate.Conflicts, err)
	}
	started, err := transition.Apply(fixture.root, fixture.statePath, fixture.journalPath, transition.Request{
		Files: files, TransitionID: "TR-006", ExpectedRevision: current.Revision,
		ExpectedRuntimeID: canonicalRuntimeID, Actor: "orchestrator",
		Evidence:               map[string]string{"builder_report_record": canonicalEvidenceIDForPath(t, current.State, fixture.result.ResultRef)},
		GateID:                 gate.GateID,
		ProducerResponsibility: "delivery_orchestrator",
	})
	if err != nil {
		t.Fatalf("original TR-006 Writer: %v", err)
	}

	// The production Draft remains a scaffold until the owner fills the
	// existing author-facing target, method and oracle fields. The fixture fills those fields
	// from its declared TASK and real Main diff, then uses original Register.
	draft, notes := review.DraftPlanForRoot(fixture.root, started.State)
	if draft == nil {
		t.Fatalf("S7 Draft is unavailable: notes=%v", notes)
	}
	authorCanonicalMechanismBehavior(draft, "e2e-workspace/s7-surface-fixture")
	for i := range draft.Claims {
		claim := &draft.Claims[i]
		if !strings.Contains(claim.Oracle, "TODO(planner)") {
			continue
		}
		if claim.Lens == "delivery" {
			claim.Oracle = fmt.Sprintf("Trace the locked %s acceptance obligations through the accepted implementation and the verified Main change at %s.", canonicalTaskID, canonicalSourceFile)
		} else {
			claim.Oracle = fmt.Sprintf("Inspect the frozen Main change at %s against this claim and record the concrete code or behavior that supports or refutes it.", canonicalSourceFile)
		}
	}
	for _, claim := range draft.Claims {
		if strings.Contains(claim.Target+claim.Assertion+claim.Oracle+claim.Method, "TODO(planner)") {
			t.Fatalf("fixture left a planner placeholder in claim %s: %+v; draft notes=%v", claim.ClaimID, claim, notes)
		}
	}
	planPath := filepath.Join(fixture.root, ".claude", "evidence", "review-plan-authored-fixture.json")
	writeJSONFixture(t, planPath, draft)
	registered, err := review.RegisterPlan(fixture.root, fixture.statePath, fixture.journalPath, review.PlanRequest{
		ExpectedRevision: started.Revision, PlanPath: planPath, OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("original S7 RegisterPlan with authored draft claims: %v", err)
	}
	if state, _ := registered.State["lifecycle"].(map[string]any); state["state"] != "verification" {
		t.Fatalf("S7 RegisterPlan cursor = %#v", state)
	}
	registeredSurface, err := buildsurface.Read(fixture.root, registered.State)
	if err != nil || !reflect.DeepEqual(registeredSurface, surface) {
		t.Fatalf("registered S7 plan changed or lost the source-backed product surface: err=%v\nregistered=%+v\noriginal=%+v", err, registeredSurface, surface)
	}
}

func seedCanonicalS5AndApplyTR003(t *testing.T, root, statePath, journalPath string) {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}

	boundReq := state["bound_req"].(map[string]any)
	reqPath := "docs/requirements/REQ-001.md"
	reqBytes := []byte("# REQ-001: Main surface fixture\n\n> Status: locked\n> Version: v1.0.0\n\nThe scoped behavior is observable in the Main product surface.\n")
	archPath := "docs/architecture/ARCH-001.md"
	archBytes := []byte("# ARCH-001\n\n> Status: locked\n> Version: v1.0.0\n\nThe behavior remains behind the existing package boundary.\n")
	contractPath := "docs/dev/contracts/BE-001.md"
	contractBytes := []byte("# BE-001\n\n> Status: locked\n> Version: v1.0.0\n\nRequirement REQ-001 maps to the observable Main behavior.\n")
	for _, file := range []struct {
		path string
		data []byte
	}{
		{reqPath, reqBytes}, {archPath, archBytes}, {contractPath, contractBytes},
	} {
		writeFixtureFile(t, root, file.path, file.data)
	}
	boundReq["path"], boundReq["version"] = reqPath, "v1.0.0"
	boundReq["metadata"] = map[string]any{"ui_impact": "none"}

	documents := state["documents"].([]any)
	documents = append(documents,
		canonicalS5Document("REQ-001", "req", reqPath, "v1.0.0", reqBytes),
		canonicalS5Document("ARCH-001", "design", archPath, "v1.0.0", archBytes),
		canonicalS5Document("BE-001", "contract", contractPath, "v1.0.0", contractBytes),
	)
	state["documents"] = documents
	state["lifecycle"] = map[string]any{"state": "document_verification", "phase": nil, "phase_revision": float64(0)}
	state["milestone"] = map[string]any{
		"stage": "S5", "lifecycle_state": "document_verification", "lifecycle_phase": nil,
		"objective": "complete the document verification fixture", "action": "lock complete S5 review evidence",
		"protocol_ref": "docs/control/agent-protocol.md", "manual_ref": ".claude/bin/loop-harness.md",
		"primary_skill": "document-verification", "read": []any{"bound REQ"}, "read_order": []any{"LOOP RECOVERY packet", "bound REQ"},
		"missing": []any{}, "done_when": []any{"both independent S5 responsibilities pass"}, "questions": []any{},
		"automation": []any{}, "integration": []any{}, "human_required": false, "blocked": false, "blocker": nil,
		"event": "S5 fixture", "instruction": "evaluate the original S5 gate", "recovery": []any{},
		"source_revision": state["revision"], "updated_at": "2026-10-14T00:00:00Z",
	}
	fixtures.SeedDocumentPassS5(t, root, state, "agent-dv-spec-s6-fixture", "agent-dv-task-s6-fixture")
	writeJSONFixture(t, statePath, state)

	// Commit the registered documents and S5 review artifacts so the original
	// TR-003 View can prove that its pinned Git tree contains every reviewed
	// source; Runtime state and journal remain excluded by the fixture ignore.
	gitFixture(t, root, "add", "docs", "evidence")
	gitFixture(t, root, "commit", "-m", "S5 locked documents and independent review fixture")

	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	view, err := fileview.New(root, "refs/heads/main", catalog.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{
		Root: root, Snapshot: snapshot, Files: view, GateID: "GATE-DOCUMENT-PASS", TransitionID: "TR-003",
	})
	if err != nil || gate.Status != qualitygate.StatusSatisfied {
		t.Fatalf("real S5 Gate = status %q missing %v conflicts %v err %v", gate.Status, gate.Missing, gate.Conflicts, err)
	}
	if _, err := transition.Apply(root, statePath, journalPath, transition.Request{
		Files: view, TransitionID: "TR-003", ExpectedRevision: snapshot.Revision,
		ExpectedRuntimeID: canonicalRuntimeID, Actor: "orchestrator",
		Evidence:               map[string]string{"document_review_record": "ev-dv-spec"},
		GateEvidenceRefs:       []string{"ev-dv-spec", "ev-dv-task"},
		GateID:                 gate.GateID,
		ProducerResponsibility: "document_verifier",
	}); err != nil {
		t.Fatalf("real TR-003 Writer: %v", err)
	}
}

func canonicalS5Document(id, kind, path, version string, _ []byte) map[string]any {
	return map[string]any{
		"id": id, "kind": kind, "path": path, "version": version,
		"status": "locked", "generation": float64(1),
	}
}

func cloneCanonicalState(t *testing.T, state map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func canonicalEvidenceIDForPath(t *testing.T, state map[string]any, path string) string {
	t.Helper()
	rows, _ := state["evidence"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["path"] == path {
			id, _ := row["id"].(string)
			if id != "" {
				return id
			}
		}
	}
	t.Fatalf("accepted Result ref %s has no original evidence index row", path)
	return ""
}

// Scope invalidation removes current quality authority, not the original Git
// delivery history needed to review a fresh post-repair product baseline.
func TestCanonicalSurfaceKeepsScopeInvalidatedMainContribution(t *testing.T) {
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, seedCanonicalS5AndApplyTR003)
	integrated, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: fixture.inspect(t, "")}, fixture.integrateConfig())
	if err != nil || integrated.Checkpoint.State != integration.StateVerified {
		t.Fatalf("Main integration: %v", err)
	}
	current := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	path := integration.DefaultCheckpointStore().PathForRecord(fixture.root, fixture.record.Reference())
	affected := impact.ComputeImpact(current.State, []string{canonicalSourceFile})
	ids := impact.InvalidateEvidence(current.State, affected, "impact-original-repair")
	if len(ids) == 0 {
		t.Fatal("fixture did not invalidate original completion")
	}
	stored, found, err := integration.DefaultCheckpointStore().Load(path)
	if err != nil || !found || stored.CanonicalMainFacts == nil || stored.State != integration.StateVerified {
		t.Fatalf("scope invalidation erased actual Main checks/integration facts: %+v %v", stored, err)
	}
	for _, raw := range current.State["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["path"] == fixture.result.ResultRef && row["status"] != "invalid" {
			t.Fatal("historical reader restored current evidence validity")
		}
	}
}

// verifyTR006Source runs the real file-view verification at the Writer
// boundary, then changes the original Result before the locked re-read.
type verifyTR006Source struct {
	*fileview.View
	change func() error
}

func (v verifyTR006Source) Verify() error {
	if err := v.View.Verify(); err != nil {
		return err
	}
	return v.change()
}

func TestTR006RereadsCurrentResultInsideOriginalWriter(t *testing.T) {
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, seedCanonicalS5AndApplyTR003)
	inspection := fixture.inspect(t, "")
	if !inspection.Ready {
		t.Fatalf("Main inspection is not ready: %+v", inspection.Blockers)
	}
	if _, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig()); err != nil {
		t.Fatalf("integrate actual Result: %v", err)
	}
	current := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	catalog, err := transition.LoadCatalog(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := fileview.New(fixture.root, "refs/heads/main", catalog.Definition.FileSources)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{
		Root: fixture.root, Snapshot: current, Files: files,
		GateID: "GATE-BUILDER-BATCH-READY", TransitionID: "TR-006",
	})
	if err != nil || gate.Status != qualitygate.StatusSatisfied {
		t.Fatalf("current batch was not ready: %+v %v", gate, err)
	}
	resultPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.result.ResultRef))
	verified := false
	lockedFiles := verifyTR006Source{View: files, change: func() error {
		verified = true
		return os.WriteFile(resultPath, []byte("invalid result"), 0600)
	}}
	_, err = transition.Apply(fixture.root, fixture.statePath, fixture.journalPath, transition.Request{
		Files: lockedFiles, TransitionID: "TR-006", ExpectedRevision: current.Revision,
		ExpectedRuntimeID: canonicalRuntimeID, Actor: "orchestrator",
		Evidence: map[string]string{"builder_report_record": canonicalEvidenceIDForPath(t, current.State, fixture.result.ResultRef)},
		GateID:   gate.GateID, ProducerResponsibility: "delivery_orchestrator",
	})
	if !verified || err == nil {
		t.Fatalf("Writer accepted a Result lost at the locked boundary: verified=%t error=%v", verified, err)
	}
	after := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	if after.Revision != current.Revision || after.State["lifecycle"].(map[string]any)["state"] != "building" {
		t.Fatalf("failed locked re-read changed Runtime: revision=%d lifecycle=%v", after.Revision, after.State["lifecycle"])
	}
}

func TestTR006ConcurrentCurrentBatchCommitsOnce(t *testing.T) {
	fixture := newCanonicalMainFixtureWithSetup(t, canonicalSourceFile, true, false, seedCanonicalS5AndApplyTR003)
	inspection := fixture.inspect(t, "")
	if !inspection.Ready {
		t.Fatalf("Main inspection is not ready: %+v", inspection.Blockers)
	}
	if _, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig()); err != nil {
		t.Fatal(err)
	}
	current := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	catalog, err := transition.LoadCatalog(fixture.root)
	if err != nil {
		t.Fatal(err)
	}
	views := make([]*fileview.View, 2)
	for i := range views {
		views[i], err = fileview.New(fixture.root, "refs/heads/main", catalog.Definition.FileSources)
		if err != nil {
			t.Fatal(err)
		}
	}
	evidenceID := canonicalEvidenceIDForPath(t, current.State, fixture.result.ResultRef)
	start, outcomes := make(chan struct{}), make(chan error, len(views))
	for _, view := range views {
		go func(view *fileview.View) {
			<-start
			_, err := transition.Apply(fixture.root, fixture.statePath, fixture.journalPath, transition.Request{
				Files: view, TransitionID: "TR-006", ExpectedRevision: current.Revision,
				ExpectedRuntimeID: canonicalRuntimeID, Actor: "orchestrator",
				Evidence: map[string]string{"builder_report_record": evidenceID},
				GateID:   "GATE-BUILDER-BATCH-READY", ProducerResponsibility: "delivery_orchestrator",
			})
			outcomes <- err
		}(view)
	}
	close(start)
	first, second := <-outcomes, <-outcomes
	if first != nil && second != nil {
		t.Fatalf("neither concurrent transition succeeded: %v; %v", first, second)
	}
	after := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	if after.State["lifecycle"].(map[string]any)["state"] != "verification" {
		t.Fatalf("concurrent handoff did not reach S7: %v", after.State["lifecycle"])
	}
	journal, err := os.ReadFile(fixture.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(journal), `"transition_id":"TR-006"`); count != 1 {
		t.Fatalf("concurrent handoff committed TR-006 %d times", count)
	}
}
