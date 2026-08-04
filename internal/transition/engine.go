package transition

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/impact"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// ResumeSentinel is the Loop Definition TR-019 sentinel that resolves to the
// pause checkpoint's from_state. Per BUG-001 §4b.2(d) the literal
// "$resume_state" was migrated to this canonical sentinel.
const ResumeSentinel = "RESUME_FROM_PAUSE"

type LockedREQ struct {
	ID         string
	Path       string
	Version    string
	SHA256     string
	ApprovedBy string
	ApprovedAt string
}

type Request struct {
	TransitionID     string
	ExpectedRevision int
	Actor            string
	Evidence         map[string]string
	// AffectedPaths are repository-relative paths changed by the tool call that
	// triggered this transition. Used by invalidate_affected_evidence.
	AffectedPaths []string
	REQ           *LockedREQ
	OccurredAt    time.Time
	Params        map[string]any
	// Journal audit fields propagated into the runtime Store (SYNC-039 §7).
	RequestID              string
	BaselineGeneration     int
	GateID                 string
	GateFingerprint        string
	ProducerResponsibility string
}

type resolvedTransition struct {
	Spec       TransitionSpec
	Phase      bool
	Global     bool
	OwnerState string
	FromStates []string
}

func Apply(root, statePath, journalPath string, request Request) (loopruntime.Snapshot, error) {
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	evidenceIDs := evidenceValues(request.Evidence)
	// Snapshot also completes any interrupted rollover before the transition is
	// resolved, so guards never inspect a mixed state/journal pair.
	snapshot, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	current := snapshot.State
	currentLifecycle, ok := current["lifecycle"].(map[string]any)
	if !ok {
		return loopruntime.Snapshot{}, fmt.Errorf("runtime lifecycle must be an object")
	}
	currentState, _ := currentLifecycle["state"].(string)
	currentPhase := nullablePhase(currentLifecycle["phase"])
	catalog, err := LoadCatalog(root)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("load transition catalog: %w", err)
	}
	resolved, err := resolveCatalog(catalog, request.TransitionID, currentState, currentPhase)
	if err != nil {
		return loopruntime.Snapshot{}, err
	}
	if err := validateRequest(root, current, resolved.Spec, request); err != nil {
		return loopruntime.Snapshot{}, err
	}
	fromCursor := cursor(currentState, currentPhase)
	targetState := resolved.Spec.To
	targetPhase := any(nil)
	if resolved.Spec.ID == "TR-019" || targetState == ResumeSentinel {
		// TR-019 resume: target the pause checkpoint's from_state/from_phase.
		pause, ok := current["pause"].(map[string]any)
		if !ok {
			return loopruntime.Snapshot{}, fmt.Errorf("TR-019 requires state.pause checkpoint")
		}
		targetState, _ = pause["from_state"].(string)
		if targetState == "" {
			return loopruntime.Snapshot{}, fmt.Errorf("TR-019: pause.from_state missing")
		}
		targetPhase = pause["from_phase"]
	} else if resolved.Phase {
		targetState = resolved.OwnerState
		targetPhase = resolved.Spec.To
	} else if stateSpec, ok := catalog.Definition.States[targetState]; ok && stateSpec.PhaseMachine != nil {
		targetPhase = catalog.Definition.PhaseMachines[*stateSpec.PhaseMachine].InitialPhase
	}
	toCursor := cursor(targetState, targetPhase)
	runtimeID, _ := current["runtime_id"].(string)
	if resolved.Spec.ID == "TR-001" && request.REQ != nil {
		runtimeID = "loop-" + request.REQ.ID
	}

	guardResults := resultRecords(resolved.Spec.Guards, "pending", "Not evaluated.")
	actionResults := resultRecords(resolved.Spec.Actions, "pending", "Not executed.")
	baselineGen := integer(baselineGeneration(current))
	if request.BaselineGeneration > 0 {
		baselineGen = request.BaselineGeneration
	}
	mutation := loopruntime.Mutation{
		EventID:                fmt.Sprintf("evt-%s-r%d", strings.ToLower(request.TransitionID), request.ExpectedRevision+1),
		TransitionID:           resolved.Spec.ID,
		Event:                  resolved.Spec.Event,
		Actor:                  request.Actor,
		IdempotencyKey:         fmt.Sprintf("runtime:%s:%d", request.TransitionID, request.ExpectedRevision),
		EvidenceIDs:            evidenceIDs,
		GuardResults:           guardResults,
		ActionResults:          actionResults,
		Message:                resolved.Spec.Description,
		OccurredAt:             occurredAt,
		From:                   fromCursor,
		To:                     toCursor,
		RuntimeID:              runtimeID,
		RequestID:              request.RequestID,
		BaselineGeneration:     baselineGen,
		GateID:                 request.GateID,
		GateFingerprint:        request.GateFingerprint,
		ProducerResponsibility: request.ProducerResponsibility,
		RequireEmptyJournal:    resolved.Spec.ID == "TR-001",
		Apply: func(state map[string]any) error {
			lifecycle, ok := state["lifecycle"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime lifecycle must be an object")
			}
			currentState, _ := lifecycle["state"].(string)
			currentPhase, _ := lifecycle["phase"].(string)
			if resolved.Phase {
				if currentState != resolved.OwnerState || currentPhase != resolved.Spec.From {
					return fmt.Errorf(
						"transition %s requires %s.%s, runtime is %s.%v",
						resolved.Spec.ID, resolved.OwnerState, resolved.Spec.From,
						currentState, lifecycle["phase"],
					)
				}
			} else if resolved.Global {
				if !contains(resolved.FromStates, currentState) {
					return fmt.Errorf("global transition %s rejects source state %s", resolved.Spec.ID, currentState)
				}
			} else {
				if currentState != resolved.Spec.From {
					return fmt.Errorf(
						"transition %s requires state %s, runtime is %s",
						resolved.Spec.ID, resolved.Spec.From, currentState,
					)
				}
				if resolved.Spec.FromPhase != "" && currentPhase != resolved.Spec.FromPhase {
					return fmt.Errorf(
						"transition %s requires %s.%s, runtime is %s.%s",
						resolved.Spec.ID, resolved.Spec.From, resolved.Spec.FromPhase,
						currentState, currentPhase,
					)
				}
			}

			if err := validateRequest(root, state, resolved.Spec, request); err != nil {
				return err
			}
			for index, name := range resolved.Spec.Guards {
				guard, ok := LookupGuard(name)
				if !ok {
					guardResults[index]["result"] = "fail"
					guardResults[index]["detail"] = "Guard is not registered."
					return fmt.Errorf("transition %s guard %s is not registered", resolved.Spec.ID, name)
				}
				if err := guard(state, request.Evidence); err != nil {
					guardResults[index]["result"] = "fail"
					guardResults[index]["detail"] = err.Error()
					return fmt.Errorf("guard %s failed: %w", name, err)
				}
				guardResults[index]["result"] = "pass"
				guardResults[index]["detail"] = "Guard evaluated successfully."
			}

			actionContext := &ActionContext{Root: root, Spec: resolved.Spec, From: fromCursor, To: toCursor, Evidence: request.Evidence, OccurredAt: occurredAt, Params: request.Params, Request: &request}
			if err := dispatchAction("capture_pause_checkpoint", actionResults, state, actionContext); err != nil {
				return err
			}

			// Apply the cursor change after the checkpoint is captured. TR-019
			// resumes to the pause checkpoint's from_state/from_phase, not to
			// the literal sentinel "RESUME_FROM_PAUSE".
			if resolved.Spec.ID == "TR-019" {
				pause, _ := state["pause"].(map[string]any)
				if pause != nil {
					if fromState, _ := pause["from_state"].(string); fromState != "" {
						lifecycle["state"] = fromState
					}
					if fromPhase, hasPhase := pause["from_phase"]; hasPhase {
						lifecycle["phase"] = fromPhase
					} else {
						lifecycle["phase"] = nil
					}
					lifecycle["phase_revision"] = integer(lifecycle["phase_revision"]) + 1
				}
			} else if resolved.Phase {
				lifecycle["phase"] = resolved.Spec.To
				lifecycle["phase_revision"] = integer(lifecycle["phase_revision"]) + 1
			} else {
				lifecycle["state"] = resolved.Spec.To
				if stateSpec, ok := catalog.Definition.States[resolved.Spec.To]; ok && stateSpec.PhaseMachine != nil {
					lifecycle["phase"] = catalog.Definition.PhaseMachines[*stateSpec.PhaseMachine].InitialPhase
				} else {
					lifecycle["phase"] = nil
				}
				lifecycle["phase_revision"] = integer(lifecycle["phase_revision"]) + 1
			}

			for _, name := range resolved.Spec.Actions {
				if name == "capture_pause_checkpoint" {
					continue
				}
				if err := dispatchAction(name, actionResults, state, actionContext); err != nil {
					return err
				}
			}
			state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	}

	store := loopruntime.NewStore(statePath, journalPath)
	// Pre-commit validator per BUG-001 §4b.2(g). Runs semantic.ValidateRuntimeBytes
	// on the post-mutation state before the atomic write; if invalid, the
	// runtime is not committed and no journal entry is appended.
	store.PreCommitValidator = func(state map[string]any) error {
		return MarshalAndValidateRuntime(root, state)
	}
	return store.Update(request.ExpectedRevision, mutation)
}

func resolveCatalog(catalog *Catalog, id, currentState string, currentPhase any) (resolvedTransition, error) {
	if spec, ok := catalog.Transitions[id]; ok {
		if currentState != spec.From {
			return resolvedTransition{}, fmt.Errorf("transition %s rejects source state %s; requires %s", id, currentState, spec.From)
		}
		if spec.FromPhase != "" {
			phase, _ := currentPhase.(string)
			if phase != spec.FromPhase {
				return resolvedTransition{}, fmt.Errorf("transition %s rejects source cursor %s.%s; requires %s.%s", id, currentState, phase, spec.From, spec.FromPhase)
			}
		}
		return resolvedTransition{Spec: spec}, nil
	}
	for owner, machine := range catalog.Definition.PhaseMachines {
		for _, spec := range machine.Transitions {
			if spec.ID != id {
				continue
			}
			phase, _ := currentPhase.(string)
			if currentState != owner || phase != spec.From {
				return resolvedTransition{}, fmt.Errorf("phase transition %s rejects source cursor %s.%s; requires %s.%s", id, currentState, phase, owner, spec.From)
			}
			return resolvedTransition{Spec: spec, Phase: true, OwnerState: owner}, nil
		}
	}
	for _, global := range catalog.GlobalTransitions {
		if global.ID != id {
			continue
		}
		if !contains(global.FromStates, currentState) {
			return resolvedTransition{}, fmt.Errorf("global transition %s rejects source state %s", id, currentState)
		}
		return resolvedTransition{Spec: TransitionSpec{
			ID: global.ID, Event: global.Event, To: global.To, Actors: global.Actors,
			Guards: global.Guards, Actions: global.Actions, RequiredEvidence: global.RequiredEvidence,
			OnGuardFailure: global.OnGuardFailure, Description: global.Description,
		}, Global: true, FromStates: append([]string(nil), global.FromStates...)}, nil
	}
	return resolvedTransition{}, fmt.Errorf("unknown transition %q", id)
}

func validateRequest(root string, state map[string]any, spec TransitionSpec, request Request) error {
	if request.Actor == "" {
		return fmt.Errorf("transition actor is required")
	}
	if len(spec.Actors) > 0 && !contains(spec.Actors, request.Actor) {
		return fmt.Errorf("actor %q cannot execute %s", request.Actor, spec.ID)
	}
	seen := map[string]bool{}
	for _, kind := range spec.RequiredEvidence {
		ref := strings.TrimSpace(request.Evidence[kind])
		if ref == "" {
			return fmt.Errorf("transition %s requires evidence %s", spec.ID, kind)
		}
		if seen[ref] {
			return fmt.Errorf("transition %s reuses evidence %s for multiple requirements", spec.ID, ref)
		}
		seen[ref] = true
		if generatedEvidenceKind(kind) {
			if err := validateGeneratedEvidence(state, kind, ref, request); err != nil {
				return fmt.Errorf("transition %s evidence %s: %w", spec.ID, kind, err)
			}
		} else if spec.ID != "TR-001" {
			if err := validateCurrentEvidence(root, state, kind, ref); err != nil {
				return fmt.Errorf("transition %s evidence %s: %w", spec.ID, kind, err)
			}
		}
	}
	if spec.ID == "TR-001" && request.REQ == nil {
		return fmt.Errorf("TR-001 requires locked REQ metadata")
	}
	return nil
}

func generatedEvidenceKind(kind string) bool {
	return kind == "pause_record"
}

func validateGeneratedEvidence(state map[string]any, kind, ref string, request Request) error {
	if kind == "pause_record" {
		return nil
	}
	return fmt.Errorf("unsupported generated evidence kind %s", kind)
}

func validateCurrentEvidence(root string, state map[string]any, requiredKind, ref string) error {
	items, _ := state["evidence"].([]any)
	var evidence map[string]any
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if item["id"] == ref || item["path"] == ref {
			evidence = item
			break
		}
	}
	if evidence == nil {
		return fmt.Errorf("reference %q is not current runtime evidence", ref)
	}
	if evidence["status"] != "valid" {
		return fmt.Errorf("reference %q is not valid", ref)
	}
	actualKind, _ := evidence["kind"].(string)
	if !contains(allowedEvidenceKinds(requiredKind), actualKind) {
		return fmt.Errorf("reference %q has kind %q, incompatible with %s", ref, actualKind, requiredKind)
	}
	baseline, _ := state["baseline"].(map[string]any)
	if integer(evidence["baseline_generation"]) != integer(baseline["generation"]) {
		return fmt.Errorf("reference %q belongs to baseline generation %d, current is %d", ref, integer(evidence["baseline_generation"]), integer(baseline["generation"]))
	}
	if evidenceRound := integer(evidence["review_round"]); evidenceRound > 0 {
		review, _ := state["review"].(map[string]any)
		if evidenceRound != integer(review["round"]) {
			return fmt.Errorf("reference %q belongs to review round %d, current is %d", ref, evidenceRound, integer(review["round"]))
		}
	}
	rel, _ := evidence["path"].(string)
	clean := filepath.Clean(rel)
	if rel == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("reference %q has unsafe path %q", ref, rel)
	}
	data, err := os.ReadFile(filepath.Join(root, clean))
	if err != nil {
		return fmt.Errorf("read reference %q: %w", ref, err)
	}
	want, _ := evidence["sha256"].(string)
	if actual := SHA256(data); want == "" || actual != want {
		return fmt.Errorf("reference %q fingerprint mismatch", ref)
	}
	return nil
}

func allowedEvidenceKinds(required string) []string {
	switch required {
	case "req_lock_record", "loop_authorization_record", "human_decision_record", "pause_record":
		return []string{"human_decision"}
	// BUG-PLANNING-SUBSTATE: baseline_record removed because the only
	// transition that demanded it (PTR-PLAN-01) is deleted. TR-002 has
	// required_evidence=[] and is gated by the direct-check planning_complete
	// guard.
	case "activation_record":
		return []string{"agent_activation"}
	case "builder_report_record":
		return []string{"builder_report", "agent_completion"}
	case "team_manifest_record":
		return []string{"builder_report", "team_manifest"}
	case "completion_report":
		return []string{"agent_completion", "completion_report"}
	case "delivery_review_record":
		return []string{"delivery_review"}
	case "qa_review_record":
		return []string{"qa_review"}
	case "e2e_review_record":
		return []string{"e2e_review"}
	case "review_result_record":
		return []string{"delivery_review", "qa_review", "e2e_review"}
	case "finding_record", "bug_batch_record", "root_cause_record", "repair_record":
		return []string{"bug"}
	case "targeted_reverification_record":
		return []string{"targeted_reverification"}
	case "clean_round_record":
		return []string{"clean_round"}
	case "acceptance_record":
		return []string{"acceptance"}
	case "release_audit_record":
		return []string{"release_audit"}
	case "change_impact_record":
		return []string{"change_impact"}
	// Planning sub-state transitions no longer demand separate design/UI records,
	// but TR-003 still consumes jointly verified contract/task batch records.
	case "contract_set_record", "task_batch_record", "document_review_record":
		return []string{"document_review", "document_review_record"}
	default:
		return nil
	}
}

func dispatchAction(name string, results []map[string]any, state map[string]any, ctx *ActionContext) error {
	index := -1
	for i, declared := range ctx.Spec.Actions {
		if declared == name {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	action, ok := LookupAction(name)
	if !ok {
		results[index]["result"] = "failed"
		results[index]["detail"] = "Action is not registered."
		return fmt.Errorf("transition %s action %s is not registered", ctx.Spec.ID, name)
	}
	result, err := action(state, ctx)
	results[index]["result"] = result.Status
	results[index]["detail"] = result.Detail
	if err != nil {
		results[index]["result"] = "failed"
		return fmt.Errorf("action %s failed: %w", name, err)
	}
	if result.Status != "committed" && result.Status != "skipped" {
		return fmt.Errorf("action %s returned invalid status %q", name, result.Status)
	}
	return nil
}

// guardAllTargetedReverificationPassed checks that every blocking BUG that
// entered the bug_resolution phase has a retesting -> closed transition with
// targeted re-verification evidence. A BUG still in retesting, fixing, or
// investigation means targeted re-verification is not complete yet.
func guardAllTargetedReverificationPassed(state map[string]any) error {
	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return nil
	}
	bugs, ok := entities["bugs"].([]any)
	if !ok {
		return nil
	}
	incompleteStates := map[string]bool{
		"investigating": true, "pending_approval": true,
		"accepted": true, "assigned": true, "fixing": true, "retesting": true,
	}
	for _, raw := range bugs {
		bug, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// Canonical "blocking" severity is P0 per
		// review-evidence.schema.json §canonicalBug; actions.go
		// recordFindingBatch defaults new findings to P0.
		if severity, _ := bug["severity"].(string); severity != "P0" {
			continue
		}
		bugState, _ := bug["state"].(string)
		if incompleteStates[bugState] {
			id, _ := bug["id"].(string)
			return fmt.Errorf("BUG %s is still in state %s; targeted re-verification not complete", id, bugState)
		}
	}
	return nil
}

func bindREQ(root string, state map[string]any, request Request, occurredAt time.Time) error {
	req := request.REQ
	if req.ID == "" || req.Path == "" || req.Version == "" || req.SHA256 == "" ||
		req.ApprovedBy == "" || req.ApprovedAt == "" {
		return fmt.Errorf("locked REQ metadata is incomplete")
	}
	data, err := os.ReadFile(filepath.Join(root, req.Path))
	if err != nil {
		return fmt.Errorf("read locked REQ: %w", err)
	}
	if SHA256(data) != req.SHA256 {
		return fmt.Errorf("locked REQ fingerprint mismatch")
	}
	status := ParseMarkdownField(string(data), "状态", "Status")
	if !strings.EqualFold(status, "locked") {
		return fmt.Errorf("REQ %s is not locked (status=%q)", req.ID, status)
	}
	state["runtime_id"] = "loop-" + req.ID
	state["authorization"] = map[string]any{
		"mode":        "binding",
		"command":     "loop-harness req bind",
		"actor":       request.Actor,
		"occurred_at": occurredAt.UTC().Format(time.RFC3339Nano),
	}
	uiImpact, err := parseUIImpact(string(data))
	if err != nil {
		return err
	}
	state["bound_req"] = map[string]any{
		"path":        req.Path,
		"version":     req.Version,
		"sha256":      req.SHA256,
		"id":          req.ID,
		"status":      "locked",
		"approved_by": req.ApprovedBy,
		"approved_at": req.ApprovedAt,
		"metadata":    map[string]any{"ui_impact": uiImpact},
	}
	baseline, ok := state["baseline"].(map[string]any)
	if !ok {
		return fmt.Errorf("runtime baseline must be an object")
	}
	baseline["generation"] = max(1, integer(baseline["generation"])+1)
	baseline["captured_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
	state["documents"] = appendDocument(state["documents"], map[string]any{
		"id":         req.ID,
		"kind":       "req",
		"path":       req.Path,
		"version":    req.Version,
		"sha256":     req.SHA256,
		"status":     "locked",
		"generation": baseline["generation"],
	})
	return nil
}

// parseUIImpact reads the `UI impact` field from a locked REQ and returns
// one of {none, changed, unknown}. The third value is part of the canonical
// SM-003 planning-phase contract (LOOP-STATE-MACHINE.md §15): bindREQ
// accepts `unknown`, but the planning cannot advance until PM clarifies it
// in §12 of the REQ. The guard that enforces "unknown → planning paused"
// lives in guardUIIImpactResolved (registered as `ui_impact_resolved`).
func parseUIImpact(content string) (string, error) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		for _, separator := range []string{"：", ":"} {
			parts := strings.SplitN(line, separator, 2)
			if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), "UI impact") {
				continue
			}
			value := strings.ToLower(strings.TrimSpace(parts[1]))
			switch value {
			case "none", "changed", "unknown":
				return value, nil
			}
			return "", fmt.Errorf("REQ ui_impact must be none, changed, or unknown")
		}
	}
	return "", fmt.Errorf("locked REQ is missing UI impact metadata")
}

func appendDocument(value any, document map[string]any) []any {
	documents, _ := value.([]any)
	return append(documents, document)
}

// ParseMarkdownField extracts the first KEY:VALUE match from a markdown
// blockquote or paragraph. KEY matching is case-insensitive and accepts both
// the Chinese full-width (`：`) and English half-width (`:`) separators so a
// template written as `> 状态：locked` and one written as `> Status: locked`
// resolve to the same canonical value. The returned value is whitespace-
// trimmed but otherwise case-preserved; callers compare it case-sensitively
// for ENUM membership (e.g. "locked", "none", "changed").
//
// This is the single source of truth for KEY:VALUE parsing of locked-REQ
// metadata. Both the CLI (`req bind`) and the transition engine
// (`bindREQ`) call it so a REQ file is parsed the same way at every gate.
func ParseMarkdownField(content string, names ...string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		for _, sep := range []string{"：", ":"} {
			parts := strings.SplitN(line, sep, 2)
			if len(parts) != 2 {
				continue
			}
			for _, name := range names {
				if strings.EqualFold(strings.TrimSpace(parts[0]), name) {
					return strings.TrimSpace(parts[1])
				}
			}
		}
	}
	return ""
}

func evidenceValues(evidence map[string]string) []string {
	values := make([]string, 0, len(evidence))
	for _, value := range evidence {
		if value != "" {
			values = append(values, value)
		}
	}
	sort.Strings(values)
	return values
}

func resultRecords(ids []string, result, detail string) []map[string]any {
	records := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		records = append(records, map[string]any{"id": id, "result": result, "detail": detail})
	}
	return records
}

func cursor(state string, phase any) map[string]any {
	return map[string]any{"state": state, "phase": phase}
}

func nullablePhase(value any) any {
	if value == nil {
		return nil
	}
	phase, _ := value.(string)
	if phase == "" {
		return nil
	}
	return phase
}

func integer(value any) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	default:
		return 0
	}
}

func baselineGeneration(state map[string]any) int {
	baseline, ok := state["baseline"].(map[string]any)
	if !ok {
		return 1
	}
	gen := integer(baseline["generation"])
	if gen < 1 {
		return 1
	}
	return gen
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func SHA256(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// capturePauseCheckpoint snapshots the runtime into state["pause"] before the
// transition moves the loop into the paused state. The checkpoint lets TR-019
// (resume) validate that no baseline drifted while paused.
//
// This must be called BEFORE the lifecycle cursor is moved, so from_state and
// from_phase reflect the source state. We read resolved.Spec.From directly to
// avoid depending on the current (pre-move) lifecycle values.
func capturePauseCheckpoint(state map[string]any, resolved resolvedTransition, occurredAt time.Time) error {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle == nil {
		return fmt.Errorf("capture_pause_checkpoint: lifecycle missing")
	}
	// SINGLE call site invariant (BUG-001 §4b.2(f)). Reject a second capture
	// within the same transition so the engine cannot write two pause
	// checkpoints for one pause event. The runtime's state schema allows
	// `pause: null` (no active checkpoint) or a map (active checkpoint); a
	// nil-value presence is the "no checkpoint" case.
	if existing, ok := state["pause"]; ok && existing != nil {
		return fmt.Errorf("capture_pause_checkpoint: pause checkpoint already exists; would overwrite")
	}
	// Use the transition spec's From for the state; phase comes from the
	// pre-move lifecycle (this function runs before the cursor changes).
	// For GTRs (global transitions) the spec's From is empty — we fall
	// back to the lifecycle's current state so the pause checkpoint
	// captures where the loop is actually paused from.
	fromState := resolved.Spec.From
	if fromState == "" && resolved.Global {
		fromState, _ = lifecycle["state"].(string)
	}
	fromPhase := lifecycle["phase"]
	phaseRevision := integer(lifecycle["phase_revision"])

	baseline, _ := state["baseline"].(map[string]any)
	baselineGeneration := 0
	if baseline != nil {
		baselineGeneration = integer(baseline["generation"])
	}

	review, _ := state["review"].(map[string]any)
	reviewRound := 0
	if review != nil {
		reviewRound = integer(review["round"])
	}

	entities, _ := state["entities"].(map[string]any)
	entitySnapshotRevision := 0
	if entities != nil {
		entitySnapshotRevision = len(asEntityArray(entities))
	}

	documents := documentFingerprints(state)
	keys := committedIdempotencyKeys(state)

	state["pause"] = map[string]any{
		"from_state":                 fromState,
		"from_phase":                 fromPhase,
		"phase_revision":             phaseRevision,
		"baseline_generation":        baselineGeneration,
		"review_round":               reviewRound,
		"entity_snapshot_revision":   entitySnapshotRevision,
		"reason":                     resolved.Spec.Description,
		"required_human_action":      pauseRequiredAction(fromState),
		"document_fingerprints":      documents,
		"committed_idempotency_keys": keys,
		"paused_at":                  occurredAt.UTC().Format(time.RFC3339Nano),
	}
	return nil
}

// restoreFromPause clears state["pause"] when TR-019 (resume) commits. It
// re-hashes every document referenced in the pause checkpoint's
// document_fingerprints and rejects the resume if any file has drifted,
// enforcing the baselines_unchanged guard.
func restoreFromPause(root string, state map[string]any) error {
	return restoreFromPauseAction(root, state)
}

// restoreFromPauseAction is the action-registry entry point; same semantics
// as restoreFromPause.
func restoreFromPauseAction(root string, state map[string]any) error {
	pause, ok := state["pause"].(map[string]any)
	if !ok {
		return fmt.Errorf("restore_state_phase_and_entities: no pause checkpoint to restore from")
	}
	fingerprints, ok := pause["document_fingerprints"].([]any)
	if !ok {
		return fmt.Errorf("restore_state_phase_and_entities: checkpoint missing document_fingerprints")
	}
	for _, raw := range fingerprints {
		doc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		path, _ := doc["path"].(string)
		expectedSHA, _ := doc["sha256"].(string)
		if path == "" || expectedSHA == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return fmt.Errorf("baselines_unchanged: cannot read %s: %w", path, err)
		}
		actualSHA := fmt.Sprintf("%x", sha256.Sum256(data))
		if actualSHA != expectedSHA {
			return fmt.Errorf(
				"baselines_unchanged: %s fingerprint drifted (pause recorded %s, actual %s)",
				path, expectedSHA[:12], actualSHA[:12])
		}
	}
	state["pause"] = nil
	return nil
}

// incrementBaselineAndInvalidate bumps the baseline generation and marks every
// evidence entry from a prior generation as invalid via the impact package.
// Used by TR-020 (reinit on human-locked new REQ).
//
// A baseline generation change invalidates the entire specification chain
// regardless of scope_refs — every evidence entry whose baseline_generation
// is <= the old generation must be invalidated. We build the impact list
// directly (not via ComputeImpact, which matches by scope) and delegate the
// actual mutation to impact.InvalidateEvidence so the status/invalidated_by/
// invalidation_rule/invalidation_reason fields are set in exactly one place.
func incrementBaselineAndInvalidate(state map[string]any, request Request, occurredAt time.Time) error {
	ctx := ActionContext{Spec: TransitionSpec{ID: "TR-020"}, OccurredAt: occurredAt}
	if _, err := incrementBaselineAndInvalidateAction(state, &ctx); err != nil {
		return err
	}
	return nil
}

// incrementBaselineAndInvalidateAction is the action-registry entry point
// with the same semantics.
func incrementBaselineAndInvalidateAction(state map[string]any, ctx *ActionContext) (ActionResult, error) {
	baseline, ok := state["baseline"].(map[string]any)
	if !ok {
		return ActionResult{Status: "failed", Detail: "baseline missing"}, fmt.Errorf("increment_baseline_generation: baseline missing")
	}
	oldGeneration := integer(baseline["generation"])
	baseline["generation"] = oldGeneration + 1
	baseline["captured_at"] = ctx.OccurredAt.UTC().Format(time.RFC3339Nano)

	evidenceSlice, ok := state["evidence"].([]any)
	if !ok {
		return ActionResult{Status: "committed", MutationApplied: true, Detail: "no evidence to invalidate"}, nil
	}
	var impacts []impact.EvidenceImpact
	for _, raw := range evidenceSlice {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if integer(entry["baseline_generation"]) <= oldGeneration {
			id, _ := entry["id"].(string)
			status, _ := entry["status"].(string)
			impacts = append(impacts, impact.EvidenceImpact{
				EvidenceID:     id,
				Rule:           "baseline_generation_change",
				Reason:         "baseline generation incremented; downstream evidence superseded",
				CurrentStatus:  status,
				AlreadyInvalid: status == "invalid",
			})
		}
	}
	impact.InvalidateEvidence(state, impacts, "TR-020")
	return ActionResult{Status: "committed", MutationApplied: true, Detail: "baseline generation bumped and downstream evidence invalidated"}, nil
}

// invalidateAllDownstream marks all currently-valid evidence as invalid via the
// impact package. Used by repair transitions that need a full evidence sweep
// without a baseline bump. Every currently-valid evidence entry is converted
// to an EvidenceImpact so impact.InvalidateEvidence handles the mutation.
func invalidateAllDownstream(state map[string]any, source string) {
	invalidateAllDownstreamAction(state, source)
}

// invalidateAllDownstreamAction is the action-registry entry point; same
// semantics as invalidateAllDownstream.
func invalidateAllDownstreamAction(state map[string]any, source string) {
	evidenceSlice, ok := state["evidence"].([]any)
	if !ok {
		return
	}
	var impacts []impact.EvidenceImpact
	for _, raw := range evidenceSlice {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		status, _ := entry["status"].(string)
		if status == "valid" {
			id, _ := entry["id"].(string)
			impacts = append(impacts, impact.EvidenceImpact{
				EvidenceID:     id,
				Rule:           "downstream_invalidation",
				Reason:         "all downstream evidence invalidated by " + source,
				CurrentStatus:  status,
				AlreadyInvalid: false,
			})
		}
	}
	impact.InvalidateEvidence(state, impacts, source)
}

func documentFingerprints(state map[string]any) []any {
	documents, ok := state["documents"].([]any)
	if !ok {
		return []any{}
	}
	out := make([]any, 0, len(documents))
	for _, raw := range documents {
		doc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		path, _ := doc["path"].(string)
		version, _ := doc["version"].(string)
		sha, _ := doc["sha256"].(string)
		if path == "" {
			continue
		}
		out = append(out, map[string]any{
			"path":    path,
			"version": version,
			"sha256":  sha,
		})
	}
	return out
}

func committedIdempotencyKeys(state map[string]any) []string {
	last, ok := state["last_transition"].(map[string]any)
	if !ok {
		return []string{}
	}
	if key, _ := last["idempotency_key"].(string); key != "" {
		return []string{key}
	}
	return []string{}
}

func asEntityArray(entities map[string]any) []any {
	var combined []any
	for _, key := range []string{"agents", "tasks", "bugs", "teams"} {
		if arr, ok := entities[key].([]any); ok {
			combined = append(combined, arr...)
		}
	}
	return combined
}

func pauseRequiredAction(fromState string) string {
	switch fromState {
	case "verification":
		return "review the blocking finding or REQ change, then resume or re-lock the REQ"
	case "bug_resolution":
		return "review the repair REQ/spec change, then resume or re-lock the REQ"
	case "release_audit":
		return "review the audit blocker, then resume or re-lock the REQ"
	default:
		return "review the pause reason, then resume, re-lock the REQ, or abort"
	}
}
