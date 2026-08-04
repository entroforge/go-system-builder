// angle_complete_guard_test.go exercises the three pre-review angle
// completeness guards. Each FR in the conjunction has at least one negative
// case; positive cases round-trip to confirm the happy path still passes.
// The test fixture writes minimal angle_declaration and team_manifest files
// under the temp root, then invokes the guard directly.
package transition_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/transition"
)

// angleFixture builds a state map plus the angle_declaration and team_manifest
// files on disk for a single dimension. Defaults match FR-003 (3 declared
// angles, all valid targets) and FR-004 (one inherited angle with confirm).
// Each builder option overrides one field so negative tests stay focused.
type angleFixture struct {
	dimension            string
	declaredCount        int
	declaredTargets      []string
	dispositions         []map[string]any
	inherited            []map[string]any
	minAngles            *int
	dispatchedAt         string
	committedAt          string
	declDimension        string
	workgroupKind        string
	reqPath              string
	reqUIImpact          string
	withAngleEvidence    bool
	withManifestEvidence bool
}

func defaultAngleFixture(dimension string) angleFixture {
	return angleFixture{
		dimension:     dimension,
		declaredCount: 3,
		declaredTargets: []string{
			"internal/cli/projection.go",
			"internal/runtime/store.go",
			"docs/loop-definition.json",
		},
		dispositions: []map[string]any{
			{"angle_id": "ANG-RUNTIME-001", "kind": "confirm", "note": "still relevant"},
		},
		inherited: []map[string]any{
			{
				"id": "ANG-RUNTIME-001", "module": "internal/runtime",
				"statement": "first angle", "target": "internal/runtime/store.go",
				"last_applied_in": "REQ-001",
			},
		},
		minAngles:            intPtr(3),
		dispatchedAt:         "2026-07-21T10:00:00Z",
		committedAt:          "2026-07-21T09:59:00Z",
		declDimension:        dimension,
		workgroupKind:        "delivery_verifier",
		reqPath:              "docs/requirements/REQ-TEST.md",
		reqUIImpact:          "changed",
		withAngleEvidence:    true,
		withManifestEvidence: true,
	}
}

func intPtr(v int) *int { return &v }

// buildAngleFixtureState materializes the fixture into a state map and writes
// the supporting files under root. Returns the state, ready for guard
// invocation.
func buildAngleFixtureState(t *testing.T, root string, f angleFixture) map[string]any {
	t.Helper()
	// Write the bound REQ with the requested ui_impact. MkdirAll so the
	// docs/requirements tree exists before WriteFile.
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, f.reqPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	reqContent := fmt.Sprintf("# Test REQ\n> 状态：locked\n> 版本：v1.0.0\n> UI impact：%s\n", f.reqUIImpact)
	if err := os.WriteFile(filepath.Join(root, f.reqPath), []byte(reqContent), 0o644); err != nil {
		t.Fatal(err)
	}

	state := stateAtVerificationMap(1)
	state["root"] = root
	state["bound_req"] = map[string]any{
		"id":          "REQ-TEST",
		"path":        f.reqPath,
		"version":     "v1.0.0",
		"sha256":      "x",
		"status":      "locked",
		"approved_by": "tester",
		"approved_at": "2026-01-01T00:00:00Z",
		"metadata":    map[string]any{"ui_impact": f.reqUIImpact},
	}

	if f.withAngleEvidence {
		writeAngleDeclaration(t, root, f)
		state["evidence"] = append(state["evidence"].([]any), map[string]any{
			"id": "ev-angle", "kind": "angle_declaration",
			"path": "evidence/angle.json", "sha256": "x",
			"status": "valid", "baseline_generation": float64(1),
			"review_round": float64(1), "produced_by": []any{"a"},
			"dimension": f.dimension,
		})
	}
	if f.withManifestEvidence {
		writeTeamManifest(t, root, f)
		state["evidence"] = append(state["evidence"].([]any), map[string]any{
			"id": "ev-manifest", "kind": "team_manifest",
			"path": "evidence/manifest.json", "sha256": "x",
			"status": "valid", "baseline_generation": float64(1),
			"review_round": float64(1), "produced_by": []any{"a"},
			"dimension": f.dimension,
		})
	}
	return state
}

func writeAngleDeclaration(t *testing.T, root string, f angleFixture) {
	t.Helper()
	declared := []map[string]any{}
	for i := 0; i < f.declaredCount; i++ {
		target := fmt.Sprintf("path/file-%d.go", i+1)
		if i < len(f.declaredTargets) {
			target = f.declaredTargets[i]
		}
		declared = append(declared, map[string]any{
			"id":        fmt.Sprintf("DECL-ANG-%03d", i+1),
			"statement": fmt.Sprintf("investigate %s", target),
			"target":    target,
		})
	}
	body := map[string]any{
		"schema_version":  "1.0.0",
		"evidence_id":     "ev-angle",
		"runtime_id":      "loop-REQ-TEST",
		"req_id":          "REQ-TEST",
		"review_round":    1,
		"dimension":       f.declDimension,
		"declared_at":     f.committedAt,
		"committed_at":    f.committedAt,
		"declared_angles": declared,
		"dispositions":    f.dispositions,
	}
	writeJSON(t, filepath.Join(root, "evidence", "angle.json"), body)
}

func writeTeamManifest(t *testing.T, root string, f angleFixture) {
	t.Helper()
	body := map[string]any{
		"schema_version":      "1.0.0",
		"manifest_id":         "team-manifest-test",
		"version":             "v1.0.0",
		"runtime_id":          "loop-REQ-TEST",
		"req_id":              "REQ-TEST",
		"baseline_generation": 1,
		"review_round":        1,
		"platform_team_id":    "test-platform",
		"workgroup_id":        "workgroup-test",
		"workgroup_kind":      f.workgroupKind,
		"dimension":           f.dimension,
		"status":              "active",
		"dispatched_at":       f.dispatchedAt,
		"inherited_angles":    f.inherited,
	}
	if f.minAngles != nil {
		body["min_angles"] = *f.minAngles
	}
	writeJSON(t, filepath.Join(root, "evidence", "manifest.json"), body)
}

func writeJSON(t *testing.T, path string, body map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(body, "", "  ")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// lookupGuardFn resolves a guard by name and fails the test if absent.
// Centralizing this keeps the negative-case blocks readable.
func lookupGuardFn(t *testing.T, name string) transition.GuardFn {
	t.Helper()
	fn, ok := transition.LookupGuard(name)
	if !ok {
		t.Fatalf("guard %s must be registered", name)
	}
	return fn
}

// --- LookupGuard tests -------------------------------------------------------

func TestAngleCompleteGuardsRegistered(t *testing.T) {
	for _, name := range []string{"delivery_angle_complete", "qa_angle_complete", "e2e_angle_complete"} {
		registration, ok := transition.LookupGuardRegistration(name)
		if !ok {
			t.Fatalf("guard %s must be registered", name)
		}
		if registration.Enforcement != transition.GuardSemanticCheck {
			t.Fatalf("guard %s enforcement = %q, want semantic_check", name, registration.Enforcement)
		}
	}
}

// --- Positive happy path ------------------------------------------------------

func TestAngleCompletePositiveDelivery(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	state := buildAngleFixtureState(t, root, f)
	if err := lookupGuardFn(t, "delivery_angle_complete")(state, nil); err != nil {
		t.Fatalf("delivery_angle_complete happy path failed: %v", err)
	}
}

func TestAngleCompletePositiveQA(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("qa")
	state := buildAngleFixtureState(t, root, f)
	if err := lookupGuardFn(t, "qa_angle_complete")(state, nil); err != nil {
		t.Fatalf("qa_angle_complete happy path failed: %v", err)
	}
}

func TestAngleCompletePositiveE2E(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("e2e_browser")
	state := buildAngleFixtureState(t, root, f)
	if err := lookupGuardFn(t, "e2e_angle_complete")(state, nil); err != nil {
		t.Fatalf("e2e_angle_complete happy path failed: %v", err)
	}
}

// --- FR-002 negative cases ---------------------------------------------------

func TestAngleCompleteRejectsMissingAngleEvidence(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	f.withAngleEvidence = false
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "angle_declaration") {
		t.Fatalf("expected missing evidence error, got: %v", err)
	}
}

func TestAngleCompleteRejectsDeclarationAfterDispatch(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	// Declaration committed one second AFTER dispatch — violates FR-002.
	f.committedAt = "2026-07-21T10:00:01Z"
	f.dispatchedAt = "2026-07-21T10:00:00Z"
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "AFTER") {
		t.Fatalf("expected FR-002 ordering error, got: %v", err)
	}
}

func TestAngleCompleteRejectsMissingDispatchedAt(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	f.dispatchedAt = ""
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "dispatched_at") {
		t.Fatalf("expected empty dispatched_at error, got: %v", err)
	}
}

// --- FR-003 negative cases ---------------------------------------------------

func TestAngleCompleteRejectsInsufficientCount(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	f.declaredCount = 1
	f.declaredTargets = []string{"internal/cli/projection.go"}
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "minimum") {
		t.Fatalf("expected FR-003 count error, got: %v", err)
	}
}

func TestAngleCompleteRejectsEmptyAngles(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	f.declaredCount = 0
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "empty") {
		t.Fatalf("expected empty declared_angles error, got: %v", err)
	}
}

func TestAngleCompleteRejectsGenericTarget(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	f.declaredTargets = []string{
		"security",
		"internal/runtime/store.go",
		"docs/loop-definition.json",
	}
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "blacklisted") {
		t.Fatalf("expected blacklisted target error, got: %v", err)
	}
}

func TestAngleCompleteRejectsEmptyTarget(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	f.declaredTargets = []string{"", "internal/runtime/store.go", "docs/loop-definition.json"}
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "empty") {
		t.Fatalf("expected empty target error, got: %v", err)
	}
}

// --- FR-004 negative cases ---------------------------------------------------

func TestAngleCompleteRejectsMissingDisposition(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	f.dispositions = []map[string]any{} // Empty — inherited angle has no disposition.
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "no disposition") {
		t.Fatalf("expected missing disposition error, got: %v", err)
	}
}

func TestAngleCompleteRejectsDispositionNotInInherited(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	f.dispositions = []map[string]any{
		{"angle_id": "ANG-OTHER-999", "kind": "confirm", "note": "why not"},
	}
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "not in team_manifest.inherited_angles") {
		t.Fatalf("expected orphan disposition error, got: %v", err)
	}
}

func TestAngleCompleteRejectsRetractWithoutReason(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	f.dispositions = []map[string]any{
		{"angle_id": "ANG-RUNTIME-001", "kind": "retract", "note": "x"},
	}
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "reason") {
		t.Fatalf("expected retract reason error, got: %v", err)
	}
}

func TestAngleCompleteRejectsExtendWithoutTarget(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	f.dispositions = []map[string]any{
		{"angle_id": "ANG-RUNTIME-001", "kind": "extend", "note": "split into two"},
	}
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "new_angle_target") {
		t.Fatalf("expected extend target error, got: %v", err)
	}
}

func TestAngleCompleteRejectsUnknownDispositionKind(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	f.dispositions = []map[string]any{
		{"angle_id": "ANG-RUNTIME-001", "kind": "bogus", "note": "what"},
	}
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "not in") {
		t.Fatalf("expected unknown kind error, got: %v", err)
	}
}

// --- FR-010 E2E NA path ------------------------------------------------------

func TestAngleCompleteE2ENAPathAcceptsOneAngle(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("e2e_browser")
	f.reqUIImpact = "none"
	f.declaredCount = 1
	f.declaredTargets = []string{"blockquote.ui_impact"}
	f.dispositions = []map[string]any{} // No inherited angles on NA path.
	f.inherited = []map[string]any{}
	state := buildAngleFixtureState(t, root, f)
	if err := lookupGuardFn(t, "e2e_angle_complete")(state, nil); err != nil {
		t.Fatalf("E2E NA happy path failed: %v", err)
	}
}

func TestAngleCompleteE2ENAPathRejectsWrongTarget(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("e2e_browser")
	f.reqUIImpact = "none"
	f.declaredCount = 1
	f.declaredTargets = []string{"internal/runtime/store.go"} // wrong target
	f.dispositions = []map[string]any{}
	f.inherited = []map[string]any{}
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "e2e_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "blockquote.ui_impact") {
		t.Fatalf("expected wrong-target error, got: %v", err)
	}
}

func TestAngleCompleteE2ENAPathRejectsMultipleAngles(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("e2e_browser")
	f.reqUIImpact = "none"
	f.declaredCount = 2
	f.declaredTargets = []string{"blockquote.ui_impact", "internal/cli/run.go"}
	f.dispositions = []map[string]any{}
	f.inherited = []map[string]any{}
	// Loosen min_angles so the "exactly 1" NA path check is the dominant
	// failure rather than the FR-003 count floor.
	one := 1
	f.minAngles = &one
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "e2e_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "exactly 1") {
		t.Fatalf("expected N=1 NA error, got: %v", err)
	}
}

func TestAngleCompleteE2ENARejectedWhenUIImpactNotNone(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("e2e_browser")
	f.reqUIImpact = "changed" // NA only when ui_impact=none
	f.declaredCount = 1
	f.declaredTargets = []string{"blockquote.ui_impact"}
	f.dispositions = []map[string]any{}
	f.inherited = []map[string]any{}
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "e2e_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "minimum") {
		t.Fatalf("expected minimum count error when ui_impact != none, got: %v", err)
	}
}

func TestAngleCompleteRejectsCrossDimensionManifest(t *testing.T) {
	root := t.TempDir()
	delivery := defaultAngleFixture("delivery")
	deliveryState := buildAngleFixtureState(t, root, delivery)

	e2e := defaultAngleFixture("e2e_browser")
	e2e.reqUIImpact = "none"
	e2e.declaredCount = 1
	e2e.declaredTargets = []string{"blockquote.ui_impact"}
	e2e.dispositions = []map[string]any{}
	e2e.inherited = []map[string]any{}
	one := 1
	e2e.minAngles = &one
	writeJSON(t, filepath.Join(root, "evidence", "angle-e2e.json"), map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-angle-e2e",
		"runtime_id": "loop-REQ-TEST", "req_id": "REQ-TEST", "review_round": 1,
		"dimension": "e2e_browser", "declared_at": e2e.committedAt, "committed_at": e2e.committedAt,
		"declared_angles": []map[string]any{{
			"id": "DECL-E2E-001", "statement": "ui impact unchanged", "target": "blockquote.ui_impact",
		}},
		"dispositions": []map[string]any{},
	})
	writeJSON(t, filepath.Join(root, "evidence", "manifest-e2e.json"), map[string]any{
		"schema_version": "1.0.0", "manifest_id": "team-manifest-e2e", "version": "v1.0.0",
		"runtime_id": "loop-REQ-TEST", "req_id": "REQ-TEST", "baseline_generation": 1,
		"review_round": 1, "platform_team_id": "platform-e2e", "workgroup_id": "workgroup-e2e",
		"workgroup_kind": "e2e_browser", "dimension": "e2e_browser", "status": "active",
		"dispatched_at": e2e.dispatchedAt, "inherited_angles": []map[string]any{},
		"min_angles": one,
	})

	state := deliveryState
	state["evidence"] = append(state["evidence"].([]any),
		map[string]any{
			"id": "ev-angle-e2e", "kind": "angle_declaration",
			"path": "evidence/angle-e2e.json", "sha256": "x",
			"status": "valid", "baseline_generation": float64(1),
			"review_round": float64(1), "produced_by": []any{"a"},
			"dimension": "e2e_browser",
		},
		map[string]any{
			"id": "ev-manifest-e2e", "kind": "team_manifest",
			"path": "evidence/manifest-e2e.json", "sha256": "x",
			"status": "valid", "baseline_generation": float64(1),
			"review_round": float64(1), "produced_by": []any{"a"},
			"dimension": "e2e_browser",
		},
	)

	if err := lookupGuardFn(t, "e2e_angle_complete")(state, nil); err != nil {
		t.Fatalf("E2E guard must use dimension-scoped manifest, got: %v", err)
	}
}

func TestAngleCompleteDimensionMismatch(t *testing.T) {
	root := t.TempDir()
	// The delivery guard expects a delivery declaration.
	f := defaultAngleFixture("delivery")
	f.declDimension = "qa"
	state := buildAngleFixtureState(t, root, f)
	err := lookupGuardFn(t, "delivery_angle_complete")(state, nil)
	if err == nil || !containsStr(err.Error(), "does not match") {
		t.Fatalf("expected dimension mismatch error, got: %v", err)
	}
}

// --- Legacy evidenceBackedGuard ids are no longer wired ----------------------

func TestAngleCompleteLegacyGuardsRemoved(t *testing.T) {
	// FR-009: the legacy evidenceBackedGuard stubs on the DV/QA/E2E phase
	// transitions are replaced by the dedicated angle_complete guards. The
	// legacy ids must not be registered (or, if registered, must not be
	// referenced by any transition in loop-definition.json).
	for _, legacy := range []string{
		"delivery_team_complete", "delivery_round_passed",
		"qa_team_complete", "qa_round_passed",
		"e2e_team_complete", "e2e_round_passed",
	} {
		if _, ok := transition.LookupGuard(legacy); ok {
			t.Fatalf("legacy guard %s should not be registered after TASK-003-C", legacy)
		}
	}
}

// --- Evidence lookup edge cases ----------------------------------------------

func TestAngleCompleteSkipsStaleRoundEvidence(t *testing.T) {
	root := t.TempDir()
	f := defaultAngleFixture("delivery")
	state := buildAngleFixtureState(t, root, f)
	// Insert a stale angle_declaration from review_round=99; the current
	// round is 1 so the stale one must not satisfy the guard.
	staleBody := map[string]any{
		"schema_version": "1.0.0",
		"evidence_id":    "ev-angle-stale",
		"runtime_id":     "loop-REQ-TEST",
		"req_id":         "REQ-TEST",
		"review_round":   99,
		"dimension":      "delivery",
		"declared_at":    "2020-01-01T00:00:00Z",
		"committed_at":   "2020-01-01T00:00:00Z",
		"declared_angles": []map[string]any{
			{"id": "DECL-ANG-001", "statement": "x", "target": "internal/cli/run.go"},
		},
		"dispositions": []map[string]any{},
	}
	writeJSON(t, filepath.Join(root, "evidence", "angle-stale.json"), staleBody)
	state["evidence"] = append(state["evidence"].([]any), map[string]any{
		"id": "ev-angle-stale", "kind": "angle_declaration",
		"path": "evidence/angle-stale.json", "sha256": "x",
		"status": "valid", "baseline_generation": float64(1),
		"review_round": float64(99), "produced_by": []any{"a"},
		"dimension": "delivery",
	})
	if err := lookupGuardFn(t, "delivery_angle_complete")(state, nil); err != nil {
		t.Fatalf("happy path should still pass (stale evidence must be ignored), got: %v", err)
	}
}

// roundBound forces a time.Now() equal to f.committedAt into the test scope.
// Currently unused; kept for symmetry with future tests that may need a
// frozen clock.
func roundBound(_ time.Time) {}
