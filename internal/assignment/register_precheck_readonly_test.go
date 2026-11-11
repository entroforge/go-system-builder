package assignment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// G18: the registration event id names the registered workgroup, so a
// repeated registration produces the same id and the journal's global
// event-id uniqueness check answers before the Apply closure can refuse it
// with the domain's own outcome. The mutation's Precheck therefore runs the
// domain judgments under the store lock.
//
// The Precheck must not write: the store hands it the locked Runtime state,
// and the committed state is compared against a clone taken before the
// Precheck runs. The dispatch backpressure judgment is therefore read-only,
// and the dispatch pressure projection it judges is written by the Apply
// closure, which owns the committed state.

// captureCommittedStateEnv names the file the committed Runtime state is
// captured into. The capture exists for the byte comparison against the
// previous sources: it proves that the committed bytes of a Builder
// registration did not change.
const captureCommittedStateEnv = "GO_SYSTEM_BUILDER_CAPTURE_STATE"

// A Builder registration still commits the dispatch pressure projection, and
// the projection is the one the dispatch judged. The read-only refusal
// changed nothing here: the projection is written by the Apply closure.
func TestBuilderRegistrationCommitsTheDispatchPressureProjection(t *testing.T) {
	fixture := newRecordRegistrationFixture(t, recordRegistrationFixtureOptions{writePath: "src"})
	narrowRegistrationFixtureScope(t, fixture)
	state, statePath, journalPath := prepareRegistrationRuntime(t, fixture)
	configuration, _ := state["configuration"].(map[string]any)
	configuration["optimization"] = map[string]any{
		"packet":                false,
		"dispatch_backpressure": map[string]any{"max_unreceived_results": float64(3), "resume_unreceived_results": float64(1)},
	}
	manifestPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.manifestRef))
	taskPath := filepath.Join(fixture.root, filepath.FromSlash(fixture.taskRef))
	writeRegistrationJSON(t, statePath, state)

	next, err := Register(fixture.root, statePath, journalPath, Request{
		ExpectedRevision: 8,
		ManifestPath:     manifestPath,
		TaskID:           fixture.taskID,
		TaskPath:         taskPath,
		OccurredAt:       time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	projection, ok := next.State["dispatch_pressure"].(map[string]any)
	if !ok {
		t.Fatalf("the Builder registration committed no dispatch pressure projection: %v", next.State["dispatch_pressure"])
	}
	projectionJSON, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"generation":1,"paused":false,"runtime_id":"loop-s6-registration"}`
	if string(projectionJSON) != want {
		t.Fatalf("dispatch pressure projection = %s, want %s", projectionJSON, want)
	}
	committed, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var committedState map[string]any
	if err := json.Unmarshal(committed, &committedState); err != nil {
		t.Fatal(err)
	}
	if _, exists := committedState["dispatch_pressure"]; !exists {
		t.Fatal("the committed Runtime state carries no dispatch pressure projection")
	}
	if capture := os.Getenv(captureCommittedStateEnv); capture != "" {
		if err := os.WriteFile(capture, committed, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The workgroup Precheck mirror judges the entity registry in the Apply
// closure's own position. A Runtime whose entities are not an object is
// refused with the domain's own outcome, not with the journal's event-id
// collision.
func TestWorkgroupRegistrationMirrorRefusesNonObjectEntities(t *testing.T) {
	state := map[string]any{
		"entities":  "not-an-object",
		"lifecycle": map[string]any{"state": "building"},
	}
	err := workgroupRegistrationRefusal(t.TempDir(), Request{}, manifest{WorkgroupKind: "builder", WorkgroupID: "workgroup-mirror"}, state)
	if err == nil {
		t.Fatal("a Runtime whose entities are not an object must be refused")
	}
	if err.Error() != "runtime entities must be an object" {
		t.Fatalf("workgroup mirror refusal = %q, want the Apply closure's own refusal", err.Error())
	}
	if strings.Contains(err.Error(), "already exists in runtime journal") {
		t.Fatalf("the journal event-id collision shadowed the domain outcome: %v", err)
	}
}

// The TASK Precheck mirror judges the entity registry before the duplicate
// TASK judgment, in the Apply closure's own order. A Runtime whose entities
// are not an object is refused with the domain's own outcome, not with the
// journal's event-id collision.
func TestTaskRegistrationMirrorRefusesNonObjectEntities(t *testing.T) {
	state := map[string]any{"entities": "not-an-object"}
	err := taskRegistrationRefusal(state, "TASK-001")
	if err == nil {
		t.Fatal("a Runtime whose entities are not an object must be refused")
	}
	if err.Error() != "entities must be an object" {
		t.Fatalf("TASK mirror refusal = %q, want the Apply closure's own refusal", err.Error())
	}
	if strings.Contains(err.Error(), "already exists in runtime journal") {
		t.Fatalf("the journal event-id collision shadowed the domain outcome: %v", err)
	}
}
