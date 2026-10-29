package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/acceptance"
	"github.com/entroforge/go-system-builder/internal/buildsurface"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func TestCanonicalDeletedSurfaceFirstPlanAndRegisteredDenominator(t *testing.T) {
	const removed = "src/pkg/input.go"
	fixture := newCanonicalMainFixtureWithWorkerSetup(t, canonicalSourceFile, true, false, enablePacketModeAndApplyTR003, func(t *testing.T, worker string) {
		if err := os.Remove(filepath.Join(worker, removed)); err != nil {
			t.Fatal(err)
		}
	})
	integrated, err := integration.Integrate(context.Background(), integration.IntegrateRequest{Inspection: fixture.inspect(t, "")}, fixture.integrateConfig())
	if err != nil || integrated.Checkpoint.State != integration.StateVerified {
		t.Fatalf("integrate deletion: %+v %v", integrated.Checkpoint, err)
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
	gate, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{Root: fixture.root, Snapshot: current, Files: files, GateID: "GATE-BUILDER-BATCH-READY", TransitionID: "TR-006"})
	if err != nil || gate.Status != qualitygate.StatusSatisfied {
		t.Fatalf("TR-006 gate: %+v %v", gate, err)
	}
	started, err := transition.Apply(fixture.root, fixture.statePath, fixture.journalPath, transition.Request{Files: files, TransitionID: "TR-006", ExpectedRevision: current.Revision, ExpectedRuntimeID: canonicalRuntimeID, Actor: "orchestrator", Evidence: map[string]string{"builder_report_record": canonicalEvidenceIDForPath(t, current.State, fixture.result.ResultRef)}, GateID: gate.GateID, GateFingerprint: gate.Fingerprint, ProducerResponsibility: "delivery_orchestrator"})
	if err != nil {
		t.Fatal(err)
	}
	draft, notes := review.DraftPlanForRoot(fixture.root, started.State, 1)
	if draft == nil || draft.ProductBaseline == nil {
		t.Fatalf("draft: %+v %v", draft, notes)
	}
	var preimage string
	for _, path := range draft.ProductBaseline.Paths {
		if path.Path == removed {
			if path.Status != "deleted" || path.PreimageCommit == "" || path.DeletedByCommit == "" || path.BlobOID == "" {
				t.Fatalf("deletion proof: %+v", path)
			}
			preimage = path.SHA256
		}
	}
	if preimage == "" {
		t.Fatal("missing deleted path")
	}
	found := false
	for _, subject := range draft.FrozenSubjects {
		if subject.Path == removed {
			found = subject.Kind == "deleted" && subject.SHA256 == preimage
		}
	}
	if !found {
		t.Fatalf("missing deletion preimage in frozen subjects: %+v", draft.FrozenSubjects)
	}
	authorCanonicalMechanismBehavior(draft, "e2e-workspace/s7-deletion-fixture")
	for i := range draft.Claims {
		draft.Claims[i].Oracle = "Trace the exact frozen source and its consumers; record the observed contract behavior."
	}
	path := filepath.Join(fixture.root, ".claude/evidence/deletion-plan.json")
	register := func() error {
		writeJSONFixture(t, path, draft)
		_, err := review.RegisterPlan(fixture.root, fixture.statePath, fixture.journalPath, review.PlanRequest{ExpectedRevision: -1, PlanPath: path})
		return err
	}
	// Losing the deletion Claim must fail even though the path is absent on disk.
	originalClaims := append([]review.Claim(nil), draft.Claims...)
	for i := range draft.Claims {
		var refs []string
		for _, ref := range draft.Claims[i].SourceRefs {
			if ref != removed {
				refs = append(refs, ref)
			}
		}
		draft.Claims[i].SourceRefs = refs
	}
	if err := register(); err == nil || !strings.Contains(err.Error(), removed+" has no Claim source_ref") {
		t.Fatalf("deletion without Claim did not reach coverage rejection: %v", err)
	}
	draft.Claims = originalClaims
	// The pinned preimage cannot be replaced by invented current bytes.
	originalBaseline := *draft.ProductBaseline
	draft.ProductBaseline.Paths = append([]buildsurface.Path(nil), originalBaseline.Paths...)
	for i := range draft.ProductBaseline.Paths {
		if draft.ProductBaseline.Paths[i].Path == removed {
			draft.ProductBaseline.Paths[i].SHA256 = strings.Repeat("0", 64)
		}
	}
	if err := register(); err == nil || !strings.Contains(err.Error(), "product baseline") {
		t.Fatalf("bad deletion preimage did not reach baseline rejection: %v", err)
	}
	*draft.ProductBaseline = originalBaseline
	if err := os.WriteFile(filepath.Join(fixture.root, removed), []byte("reappeared"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := register(); err == nil || !strings.Contains(err.Error(), removed) || !(strings.Contains(err.Error(), "reappeared") || strings.Contains(err.Error(), "product checkout differs from pinned Main")) {
		t.Fatalf("reappeared deletion did not reach original absence/source-drift rejection: %v", err)
	}
	if err := os.Remove(filepath.Join(fixture.root, removed)); err != nil {
		t.Fatal(err)
	}
	if err := register(); err != nil {
		t.Fatalf("first Plan deletion registration: %v", err)
	}
	registered := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	pinned, err := buildsurface.ReadRegistered(fixture.root, registered.State)
	if err != nil || !reflect.DeepEqual(pinned, *draft.ProductBaseline) {
		t.Fatalf("registered reader: %+v %v", pinned, err)
	}
	denominator, diagnostics := review.ChangedPathsForRootDetailed(fixture.root, registered.State)
	if len(diagnostics) > 0 || !containsString(denominator, removed) {
		t.Fatalf("S7 denominator: %v %v", denominator, diagnostics)
	}
	external, err := acceptance.BuildS10ExternalBaseline(fixture.root, registered.State, nil)
	if err != nil || !reflect.DeepEqual(external.ChangedPaths, denominator) {
		t.Fatalf("S10 baseline: %+v %v; S7 denominator %v", external, err, denominator)
	}
	// Independent reviewer owners submit their original issued Results; fixture
	// PASS strings exercise the mechanism and do not assert model review.
	for _, wave := range []string{"static", "behavior"} {
		owners := dispatchS7ReviewerAssignments(t, fixture, draft, wave)
		packets, err := review.IssueResultPackets(fixture.root, fixture.statePath, fixture.journalPath, -1, "")
		if err != nil || len(packets) != len(owners) {
			t.Fatalf("%s packets: %d owners %d: %v", wave, len(packets), len(owners), err)
		}
		for _, packet := range packets {
			writeJSONFixture(t, filepath.Join(fixture.root, filepath.FromSlash(packet.AnswerPath)), s7MechanismAnswer(t, fixture, packet))
			if _, err := review.SubmitIssuedResult(fixture.root, fixture.statePath, fixture.journalPath, review.IssuedSubmitRequest{Context: t.Context(), ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: owners[packet.Binding.AssignmentID], SourceRoot: fixture.root}); err != nil {
				t.Fatalf("deleted-surface independent Result: %v", err)
			}
		}
	}
	final := readRuntimeSnapshot(t, fixture.statePath, fixture.journalPath)
	if ptr := review.PlanPointerFromState(final.State); ptr == nil || ptr.Status != "clean" {
		t.Fatalf("deleted-surface CleanRound: %+v", ptr)
	}
	// A later real Main commit forces the fresh draft to rebuild its source
	// snapshot and digest while retaining the original deletion denominator.
	writeFixtureFile(t, fixture.root, canonicalSourceFile, []byte("main-check-marker\nfresh-round-change\n"))
	gitFixture(t, fixture.root, "add", "--", canonicalSourceFile)
	gitFixture(t, fixture.root, "commit", "-m", "fresh review product change")
	fresh, notes := review.DraftPlanForRoot(fixture.root, final.State, 2)
	if fresh == nil || fresh.ProductBaseline == nil || fresh.ProductBaseline.FinalCommit == draft.ProductBaseline.FinalCommit || review.SubjectDigest(fresh) == review.SubjectDigest(draft) {
		t.Fatalf("fresh source reconstruction: %+v %v", fresh, notes)
	}
	for _, subject := range fresh.FrozenSubjects {
		if subject.Path == removed && subject.Kind == "deleted" && subject.SHA256 == preimage {
			return
		}
	}
	t.Fatalf("fresh round lost deletion: %+v", fresh.FrozenSubjects)
}
