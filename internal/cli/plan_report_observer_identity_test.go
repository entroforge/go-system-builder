package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
)

// planObserverFixture is the plan-report observer fixture: a dispatched
// Worker, the Runtime pair the observer writes into, and an authority-root
// plan report whose path the Worker spelled in its own words.
type planObserverFixture struct {
	*runtimeFixture
	planFile string // the plan file inside the authority root
	planRef  string // the spelling the Worker sent
}

// newPlanObserverFixture stages the authority-root plan report. The Worker
// spells the plan with a "./" prefix, so the plan_checkpoint auto-chain
// records the canonical root-relative path and the observer's guard no
// longer matches the spelling the observation was asked about.
func newPlanObserverFixture(t *testing.T, dispatchMode string) *planObserverFixture {
	t.Helper()
	fixture := newRuntimeFixture(t)
	fixture.addAgent("agent-s9", "backend-builder", "reading", "workgroup-s9", []string{"TASK-123"})
	fixture.addTask("TASK-123", "in_progress", []string{"agent-s9"})
	fixture.addTeam("workgroup-s9", "active", []string{"agent-s9"})
	entities := fixture.state["entities"].(map[string]any)
	agent, _ := entities["agents"].([]any)[0].(map[string]any)
	agent["dispatch_mode"] = dispatchMode
	withReadbackTransition(t, fixture.root)

	manifest := decodePlanCheckpointAsset(t, "team-manifest.example.json")
	manifest["manifest_id"] = "team-manifest-s9"
	manifest["runtime_id"] = "loop-REQ-039"
	manifest["req_id"] = "REQ-039"
	manifest["workgroup_id"] = "workgroup-s9"
	manifest["workgroup_kind"] = "builder"
	manifest["planned_agent_count"] = 1
	manifest["max_parallel_agents"] = 1
	manifest["separation_edges"] = []any{}
	manifestAssignment, _ := manifest["assignments"].([]any)[0].(map[string]any)
	manifestAssignment["assignment_id"] = "assignment-s9-unit"
	manifestAssignment["role_family"] = "backend-builder"
	manifestAssignment["agent_id"] = "agent-s9"
	manifest["assignments"] = []any{manifestAssignment}
	manifestDir := filepath.Join(fixture.root, ".claude", "workgroups", "REQ-039", "workgroup-s9")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePlanCheckpointJSON(t, manifestDir, "manifest.json", manifest)

	plan := decodePlanCheckpointMessage(t)
	plan["runtime_id"] = "loop-REQ-039"
	plan["agent_id"] = "agent-s9"
	plan["task_id"] = "TASK-123"
	plan["team_id"] = "workgroup-s9"
	plan["assignment_id"] = "assignment-s9-unit"
	plan["assignment_revision"] = 1
	planDir := filepath.Join(fixture.root, ".claude", "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePlanCheckpointJSON(t, planDir, "plan.json", plan)
	fixture.persist(t)

	return &planObserverFixture{
		runtimeFixture: fixture,
		planFile:       filepath.Join(planDir, "plan.json"),
		planRef:        "./.claude/plans/plan.json",
	}
}

// withReadbackTransition gives the runtime root the agent lifecycle
// transition the plan_checkpoint auto-chain resolves, so the fixture can
// drive the real production writer.
func withReadbackTransition(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "docs", "control", "loop-definition.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var definition map[string]any
	if err := json.Unmarshal(data, &definition); err != nil {
		t.Fatal(err)
	}
	lifecycles, _ := definition["entity_lifecycles"].(map[string]any)
	agentLifecycle, _ := lifecycles["agent"].(map[string]any)
	transitions, _ := agentLifecycle["transitions"].([]any)
	agentLifecycle["transitions"] = append(transitions, map[string]any{
		"from": "reading", "event": "readback_submitted", "to": "understanding_submitted",
	})
	encoded, err := json.MarshalIndent(definition, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}

// observe drives the plan-report observer the way the PostToolUse hook does.
func (f *planObserverFixture) observe(t *testing.T, sessionID, ref string) (string, string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse",
		"session_id":      sessionID,
		"tool_name":       "SendMessage",
		"tool_use_id":     "plan-observer-" + sessionID,
		"cwd":             "",
		"tool_input": map[string]any{
			"message_type":  "plan_report",
			"agent_id":      "agent-s9",
			"teammate_name": "agent-s9",
			"plan_ref":      ref,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"hook", "--event", "PostToolUse", "--root", f.root}, strings.NewReader(string(payload)), &stdout, &stderr); code != 0 {
		t.Fatalf("observer exit code %d: %s", code, stderr.String())
	}
	return stdout.String(), stderr.String()
}

// autoChainReadback drives the plan_checkpoint auto-chain's readback step
// on its own, the call that records the canonical root-relative plan path.
func (f *planObserverFixture) autoChainReadback(t *testing.T) {
	t.Helper()
	if _, err := assignment.AdvanceAgent(f.root, f.statePath, f.journalPath, assignment.AgentEventRequest{
		AgentID:          "agent-s9",
		Event:            "readback_submitted",
		MessagePath:      f.planFile,
		ExpectedRevision: -1,
	}); err != nil {
		t.Fatalf("plan_checkpoint readback step failed: %v", err)
	}
}

// clearPlanCheckpoint erases the Worker's plan checkpoint the way an
// Assignment revision erases it from the prior owner.
func (f *planObserverFixture) clearPlanCheckpoint(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	entities, _ := state["entities"].(map[string]any)
	for _, raw := range entities["agents"].([]any) {
		agent, _ := raw.(map[string]any)
		if agent == nil || agent["id"] != "agent-s9" {
			continue
		}
		agent["plan_reported_ref"] = nil
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.statePath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *planObserverFixture) agentPlanReportedRef(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	entities, _ := state["entities"].(map[string]any)
	for _, raw := range entities["agents"].([]any) {
		agent, _ := raw.(map[string]any)
		if agent == nil || agent["id"] != "agent-s9" {
			continue
		}
		ref, _ := agent["plan_reported_ref"].(string)
		return ref
	}
	return ""
}

func (f *planObserverFixture) revision(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	revision, _ := state["revision"].(float64)
	return int(revision)
}

func (f *planObserverFixture) journalEventIDs(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(f.journalPath)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode journal event: %v", err)
		}
		id, _ := event["event_id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// TestPlanReportObserver is the repeated observation of one plan after the
// plan_checkpoint auto-chain rewrote the guard key with the canonical
// root-relative path. The caller asked whether the checkpoint may name the
// plan, so the caller must hear the domain's judgment, and the refused
// repeat must write neither Runtime state nor a journal fact.
func TestPlanReportObserver(t *testing.T) {
	fixture := newPlanObserverFixture(t, "plan_checkpoint")

	stdout, stderr := fixture.observe(t, "observer-first", fixture.planRef)
	if !strings.Contains(stdout, "plan_report observed for agent-s9") {
		t.Fatalf("first observation was not recorded: stdout=%s stderr=%s", stdout, stderr)
	}
	// The auto-chain recorded the canonical root-relative path, so the
	// guard no longer matches the spelling the observation was asked about.
	if got := fixture.agentPlanReportedRef(t); got != ".claude/plans/plan.json" {
		t.Fatalf("auto-chain recorded %q, want the canonical plan path", got)
	}
	if got := fixture.agentPlanReportedRef(t); got == fixture.planRef {
		t.Fatalf("the fixture must spell the plan differently from the canonical path, or the guard is not defeated")
	}
	revisionBefore := fixture.revision(t)
	eventsBefore := fixture.journalEventIDs(t)

	stdout, stderr = fixture.observe(t, "observer-repeat", fixture.planRef)
	if !strings.Contains(stderr, "agent checkpoint already refers to a different plan; investigate dispatch before replacing it") {
		t.Fatalf("repeated observation must answer with the domain judgment: stdout=%s stderr=%s", stdout, stderr)
	}
	if strings.Contains(stderr, "already exists in runtime journal") {
		t.Fatalf("repeated observation must not answer with the journal key collision: %s", stderr)
	}
	if got := fixture.revision(t); got != revisionBefore {
		t.Fatalf("refused observation moved the Runtime revision: got=%d want=%d", got, revisionBefore)
	}
	if got := fixture.journalEventIDs(t); len(got) != len(eventsBefore) {
		t.Fatalf("refused observation wrote a journal event: got=%v want=%v", got, eventsBefore)
	}
}

// TestPlanReportObserverRevisionTriggerAnswersWithDomainJudgment is the
// repeated observation of one plan after an Assignment revision erased the
// Worker's plan checkpoint. The erased checkpoint leaves the observer no
// domain fact to compare, so the domain's own record of the observation is
// the answer the caller has to receive.
func TestPlanReportObserverRevisionTriggerAnswersWithDomainJudgment(t *testing.T) {
	fixture := newPlanObserverFixture(t, "plan_checkpoint")

	stdout, stderr := fixture.observe(t, "observer-revision-first", fixture.planRef)
	if !strings.Contains(stdout, "plan_report observed for agent-s9") {
		t.Fatalf("first observation was not recorded: stdout=%s stderr=%s", stdout, stderr)
	}
	eventsBefore := fixture.journalEventIDs(t)

	// The Assignment revision erases the prior owner's plan checkpoint.
	fixture.clearPlanCheckpoint(t)
	revisionBefore := fixture.revision(t)

	stdout, stderr = fixture.observe(t, "observer-revision-repeat", fixture.planRef)
	if strings.Contains(stderr, "already exists in runtime journal") {
		t.Fatalf("repeated observation must not answer with the journal key collision: %s", stderr)
	}
	if !strings.Contains(stderr, "already registered") {
		t.Fatalf("repeated observation must answer with the domain judgment: stdout=%s stderr=%s", stdout, stderr)
	}
	if got := fixture.revision(t); got != revisionBefore {
		t.Fatalf("refused observation moved the Runtime revision: got=%d want=%d", got, revisionBefore)
	}
	if got := fixture.journalEventIDs(t); len(got) != len(eventsBefore) {
		t.Fatalf("refused observation wrote a journal event: got=%v want=%v", got, eventsBefore)
	}
}

// TestPlanReportObserverLegitimateRepeatStaysSilent is the repeat the guard
// already skips: the checkpoint still names the spelling the observation
// carried, so there is nothing to judge and nothing to write.
func TestPlanReportObserverLegitimateRepeatStaysSilent(t *testing.T) {
	fixture := newPlanObserverFixture(t, "plan_approval_required")

	stdout, stderr := fixture.observe(t, "observer-legitimate-first", fixture.planRef)
	if !strings.Contains(stdout, "plan_report observed for agent-s9") {
		t.Fatalf("first observation was not recorded: stdout=%s stderr=%s", stdout, stderr)
	}
	if got := fixture.agentPlanReportedRef(t); got != fixture.planRef {
		t.Fatalf("first observation recorded %q, want the Worker's spelling %q", got, fixture.planRef)
	}
	revisionBefore := fixture.revision(t)
	eventsBefore := fixture.journalEventIDs(t)

	stdout, stderr = fixture.observe(t, "observer-legitimate-repeat", fixture.planRef)
	if strings.Contains(stderr, "registration failed") || strings.Contains(stderr, "rejected") {
		t.Fatalf("a repeat the guard skips must stay silent: stdout=%s stderr=%s", stdout, stderr)
	}
	if !strings.Contains(stdout, "plan_report observed for agent-s9") {
		t.Fatalf("a repeat the guard skips must still be observed: stdout=%s stderr=%s", stdout, stderr)
	}
	if got := fixture.revision(t); got != revisionBefore {
		t.Fatalf("skipped repeat moved the Runtime revision: got=%d want=%d", got, revisionBefore)
	}
	if got := fixture.journalEventIDs(t); len(got) != len(eventsBefore) {
		t.Fatalf("skipped repeat wrote a journal event: got=%v want=%v", got, eventsBefore)
	}
}

// TestPlanReportObserverIdentityCarriesSemanticCoordinates pins the identity
// of a plan observation: it names the Worker, the Assignment revision the
// dispatch was made for and the plan itself, and it carries no Runtime write
// counter, so the same plan keeps one identity while the Runtime moves.
func TestPlanReportObserverIdentityCarriesSemanticCoordinates(t *testing.T) {
	fixture := newPlanObserverFixture(t, "plan_checkpoint")

	stdout, stderr := fixture.observe(t, "observer-identity-first", fixture.planRef)
	if !strings.Contains(stdout, "plan_report observed for agent-s9") {
		t.Fatalf("first observation was not recorded: stdout=%s stderr=%s", stdout, stderr)
	}
	events := fixture.journalEventIDs(t)
	if len(events) < 1 {
		t.Fatalf("first observation wrote no journal event: %v", events)
	}
	eventID := events[0]
	// The identity is derived from the domain facts only: the observing
	// Worker, the Assignment revision the dispatch was made for and the
	// canonical plan path. The Runtime revision the observation landed on
	// is not part of it.
	want := fmt.Sprintf("evt-plan-observed-%s-%s-r%d-%s", "agent-s9", "assignment-s9-unit", 1, domainEventToken(".claude/plans/plan.json"))
	if eventID != want {
		t.Fatalf("observation identity %q does not name the semantic coordinates %q", eventID, want)
	}

	// A second spelling of the same plan names the same plan, so the
	// identity does not fork with the spelling of the path: the domain
	// finds the observation already registered.
	fixture.clearPlanCheckpoint(t)
	stdout, stderr = fixture.observe(t, "observer-identity-spelling", ".claude/plans/plan.json")
	if !strings.Contains(stderr, "already registered") {
		t.Fatalf("one plan spelled two ways must keep one identity: stdout=%s stderr=%s", stdout, stderr)
	}
	repeated := fixture.journalEventIDs(t)
	if len(repeated) != len(events) {
		t.Fatalf("the refused second spelling wrote a journal event: %v", repeated)
	}
}
