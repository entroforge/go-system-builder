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

	"github.com/entroforge/go-system-builder/internal/acceptance"
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

	beforeMain := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	journal, err := loopruntime.ReadJournalForState(fixture.root, beforeMain.State)
	if err != nil {
		t.Fatal(err)
	}
	start, err := loopruntime.RequireCurrentS6MainStart(beforeMain.State, journal)
	if err != nil || start.StartCommit == "" || start.TransitionEventID == "" {
		t.Fatalf("real TR-003 start receipt = %+v err %v", start, err)
	}

	inspection := fixture.inspect(t, "")
	if !inspection.Ready || len(inspection.ChangedPaths) != 1 || inspection.ChangedPaths[0] != canonicalSourceFile {
		t.Fatalf("canonical Main Inspect = ready %t paths %v blockers %v", inspection.Ready, inspection.ChangedPaths, inspection.Blockers)
	}
	integrated, err := integration.Integrate(ctx, integration.IntegrateRequest{Inspection: inspection}, fixture.integrateConfig())
	if err != nil || integrated.Checkpoint.State != integration.StateVerified {
		t.Fatalf("original Main Integrate = state %q err %v", integrated.Checkpoint.State, err)
	}

	current := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	surface, err := buildsurface.Read(fixture.root, current.State)
	if err != nil {
		t.Fatalf("canonical buildsurface.Read after real Main: %v", err)
	}
	if surface.StartCommit != start.StartCommit || surface.FinalCommit != integrated.Checkpoint.MergeCommit || len(surface.Tasks) != 1 || surface.Tasks[0].ID != canonicalTaskID || len(surface.Paths) != 1 || surface.Paths[0].Path != canonicalSourceFile || len(surface.Contributions) != 1 {
		t.Fatalf("canonical product surface = %+v; expected original TR-003 start, current TASK, one Main path/contribution", surface)
	}

	// Missing the committed first-start projection must fail closed even
	// though the rest of the Runtime and Main evidence still exists.
	withoutStart := cloneCanonicalState(t, current.State)
	delete(withoutStart["baseline"].(map[string]any), "s6_main_starts")
	if _, err := buildsurface.Read(fixture.root, withoutStart); err == nil || !strings.Contains(err.Error(), "original TR-003 common Main baseline") {
		t.Fatalf("buildsurface accepted a Runtime with its original TR-003 start removed: %v", err)
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
		Evidence: map[string]string{"builder_report_record": canonicalEvidenceIDForPath(t, current.State, fixture.result.ResultRef)},
		GateID:   gate.GateID, GateFingerprint: gate.Fingerprint,
		ProducerResponsibility: "delivery_orchestrator",
	})
	if err != nil {
		t.Fatalf("original TR-006 Writer: %v", err)
	}

	// The production Draft remains a scaffold until the owner fills the
	// existing author-facing target, method and oracle fields. The fixture fills those fields
	// from its declared TASK and real Main diff, then uses original Register.
	draft, notes := review.DraftPlanForRoot(fixture.root, started.State, 1)
	if draft == nil || draft.ProductBaseline == nil {
		t.Fatalf("canonical S7 Draft has no machine product snapshot: plan=%+v notes=%v", draft, notes)
	}
	if !reflect.DeepEqual(*draft.ProductBaseline, surface) {
		t.Fatalf("S7 Draft product snapshot differs from verified buildsurface.Read\nDraft: %+v\nRead: %+v", *draft.ProductBaseline, surface)
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
	pinnedSurface, err := buildsurface.ReadRegistered(fixture.root, registered.State)
	if err != nil || !reflect.DeepEqual(pinnedSurface, surface) {
		t.Fatalf("ReadRegistered product surface = %+v err %v; want original source-backed snapshot", pinnedSurface, err)
	}
	if err := review.VerifyCurrentProductBaseline(fixture.root, registered.State); err != nil {
		t.Fatalf("public current product-baseline verifier rejected the exact registered snapshot: %v", err)
	}

	// A pinned canonical ReviewPlan keeps the strict S6 path selected if its
	// live Assignment registry is removed. Neither the Review verifier nor the
	// S10 external denominator may downgrade that plan to legacy evidence.
	withoutAssignments := cloneCanonicalState(t, registered.State)
	delete(withoutAssignments["entities"].(map[string]any), "assignments")
	if err := review.VerifyCurrentProductBaseline(fixture.root, withoutAssignments); err == nil {
		t.Fatal("current ReviewPlan verifier accepted a pinned canonical plan after the Assignment registry was removed")
	}
	if _, err := acceptance.BuildS10ExternalBaseline(fixture.root, withoutAssignments, nil); err == nil {
		t.Fatal("S10 external baseline accepted a pinned canonical plan after the Assignment registry was removed")
	}

	// The original TR-003 denominator is the exact current locked TASK set.
	// Removing its registered TASK document must invalidate a fresh source read.
	withoutTask := cloneCanonicalState(t, registered.State)
	documents, ok := withoutTask["documents"].([]any)
	if !ok {
		t.Fatalf("Runtime documents have unexpected type %T", withoutTask["documents"])
	}
	keptDocuments := make([]any, 0, len(documents))
	removedTask := 0
	for _, raw := range documents {
		row, _ := raw.(map[string]any)
		if row != nil && row["kind"] == "task" && row["id"] == canonicalTaskID {
			removedTask++
			continue
		}
		keptDocuments = append(keptDocuments, raw)
	}
	if removedTask != 1 {
		t.Fatalf("removed %d current locked TASK documents for %s; want exactly one", removedTask, canonicalTaskID)
	}
	withoutTask["documents"] = keptDocuments
	if _, err := buildsurface.Read(fixture.root, withoutTask); err == nil {
		t.Fatal("buildsurface.Read accepted the current TASK denominator after its registered TASK document was removed")
	}

	// A Main tool log is part of the original check receipt closure. Changing
	// its durable bytes invalidates both registered and current plan
	// verification, even though no professional S7 verdict is fabricated here.
	if len(integrated.Checkpoint.CanonicalMainFacts.Checks) != 1 {
		t.Fatalf("integration checkpoint has %d Main facts", len(integrated.Checkpoint.CanonicalMainFacts.Checks))
	}
	logRef := integrated.Checkpoint.CanonicalMainFacts.Checks[0].LogRef
	originalLog, err := os.ReadFile(logRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logRef, []byte("tampered Main check log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildsurface.ReadRegistered(fixture.root, registered.State); err == nil {
		t.Fatal("ReadRegistered accepted tampered original Main log bytes")
	}
	if err := review.VerifyCurrentProductBaseline(fixture.root, registered.State); err == nil {
		t.Fatal("current product-baseline verifier accepted tampered original Main log bytes")
	}
	if err := os.WriteFile(logRef, originalLog, 0o600); err != nil {
		t.Fatalf("restore original Main log fixture: %v", err)
	}

	// A later unrelated product commit moves official Main beyond the pinned
	// ReviewPlan. The new source read records its unowned path; old registered
	// and current-plan verification must reject the stale pinned snapshot.
	unownedPath := "docs/fixture surface with spaces.txt"
	writeFixtureFile(t, fixture.root, unownedPath, []byte("unowned later product change\n"))
	gitFixture(t, fixture.root, "add", "--", unownedPath)
	gitFixture(t, fixture.root, "commit", "-m", "unowned later product change")
	updatedSurface, err := buildsurface.Read(fixture.root, registered.State)
	if err != nil {
		t.Fatalf("fresh buildsurface.Read after unrelated Main change: %v", err)
	}
	if !containsString(updatedSurface.UnattributedPaths, unownedPath) {
		t.Fatalf("fresh product baseline omitted the unowned spaced path: paths=%+v unattributed=%v", updatedSurface.Paths, updatedSurface.UnattributedPaths)
	}
	if _, err := buildsurface.ReadRegistered(fixture.root, registered.State); err == nil {
		t.Fatal("ReadRegistered accepted the old pinned snapshot after Main HEAD moved")
	}
	if err := review.VerifyCurrentProductBaseline(fixture.root, registered.State); err == nil {
		t.Fatal("current product-baseline verifier accepted the old pinned snapshot after Main HEAD moved")
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
	boundReq["path"], boundReq["version"], boundReq["sha256"] = reqPath, "v1.0.0", digestHex(reqBytes)
	boundReq["metadata"] = map[string]any{"ui_impact": "none"}
	workspace := boundReq["workspace"].(map[string]any)
	workspace["bound_commit"] = readGit(t, root, "rev-parse", "main")

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
		Evidence:         map[string]string{"document_review_record": "ev-dv-spec"},
		GateEvidenceRefs: []string{"ev-dv-spec", "ev-dv-task"},
		GateID:           gate.GateID, GateFingerprint: gate.Fingerprint,
		ProducerResponsibility: "document_verifier",
	}); err != nil {
		t.Fatalf("real TR-003 Writer: %v", err)
	}
}

func canonicalS5Document(id, kind, path, version string, data []byte) map[string]any {
	return map[string]any{
		"id": id, "kind": kind, "path": path, "version": version,
		"sha256": digestHex(data), "status": "locked", "generation": float64(1),
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
	before, err := buildsurface.Read(fixture.root, current.State)
	if err != nil {
		t.Fatal(err)
	}
	affected := impact.ComputeImpact(current.State, []string{canonicalSourceFile})
	ids := impact.InvalidateEvidence(current.State, affected, "impact-original-repair")
	if len(ids) == 0 {
		t.Fatal("fixture did not invalidate original completion")
	}
	after, err := buildsurface.Read(fixture.root, current.State)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("scope invalidation erased proven contribution: %v", err)
	}
	for _, raw := range current.State["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["path"] == fixture.result.ResultRef && row["status"] != "invalid" {
			t.Fatal("historical reader restored current evidence validity")
		}
	}
	logRef := integrated.Checkpoint.CanonicalMainFacts.Checks[0].LogRef
	if err := os.WriteFile(logRef, []byte("tampered check"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildsurface.Read(fixture.root, current.State); err == nil {
		t.Fatal("historical contribution bypassed actual Main check proof")
	}
}
