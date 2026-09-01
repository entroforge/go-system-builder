package transition

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/impact"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/verification"
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
	// ExpectedRuntimeID binds a caller's snapshot to the runtime identity as
	// well as its numeric revision. This prevents a pre-bind revision-zero
	// snapshot from being accepted after bind created a new runtime identity.
	ExpectedRuntimeID string
	Actor             string
	Evidence          map[string]string
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
	// Apply is an explicit mutation boundary. Its writer may recover a durable
	// pending operation before guards inspect the state/journal pair.
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	snapshot, err := store.Snapshot()
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
	if expectedRuntimeID := strings.TrimSpace(request.ExpectedRuntimeID); expectedRuntimeID != "" && !(request.TransitionID == "TR-001" && expectedRuntimeID == "loop-inactive") {
		currentRuntimeID, _ := current["runtime_id"].(string)
		if currentRuntimeID != expectedRuntimeID {
			return loopruntime.Snapshot{}, fmt.Errorf("%w: expected %q, current %q", loopruntime.ErrStaleRuntimeIdentity, expectedRuntimeID, currentRuntimeID)
		}
	}
	commitRevision := snapshot.Revision
	if request.ExpectedRevision >= 0 {
		commitRevision = request.ExpectedRevision
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("load transition catalog: %w", err)
	}
	resolved, err := resolveCatalog(catalog, request.TransitionID, currentState, currentPhase)
	if err != nil {
		return loopruntime.Snapshot{}, err
	}
	// RC-06 (S10-2): forbidden_events were decoded into the catalog but never
	// consumed by Apply — the "strong_block" table was documentation. The two
	// acceptance/release-audit strong blocks are now a hard pre-Apply barrier:
	// no clean round, no ACC, no release audit — regardless of what evidence
	// refs the caller supplies. `clean_round_still_valid` re-checks this inside
	// the mutation, but the catalog declaration itself must gate Apply
	// fail-closed (create_acc_without_clean_round / run_release_audit_without_acc).
	if err := enforceForbiddenEvents(catalog, resolved.Spec, current); err != nil {
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
		EventID:                fmt.Sprintf("evt-%s-r%d", strings.ToLower(request.TransitionID), commitRevision+1),
		TransitionID:           resolved.Spec.ID,
		Event:                  resolved.Spec.Event,
		Actor:                  request.Actor,
		IdempotencyKey:         fmt.Sprintf("runtime:%s:%d", request.TransitionID, commitRevision),
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
		BoundaryReset:          resolved.Spec.ID == "TR-001",
		Apply: func(state map[string]any) error {
			// Direct-check guards resolve disk paths from state["root"]. The
			// writer re-reads state from disk before invoking this closure, so
			// the default must live HERE: without it guards fall back to "." and
			// can pass vacuously (or read the wrong tree) in CLI flows
			// (L3-S4 v4.0.1). A pre-set root (unit-test temp root) wins.
			if currentRoot, _ := state["root"].(string); currentRoot == "" {
				state["root"] = root
			}
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

			// Human decisions authorize one committed transition only. Consume
			// before transition actions so a baseline-reset action cannot first
			// invalidate the decision that authorized this very transition.
			if resolved.Spec.HumanDecisionScope != "" {
				decisionRef := strings.TrimSpace(request.Evidence["human_decision_record"])
				if err := loopruntime.ConsumeHumanDecisionEvidence(state, decisionRef, resolved.Spec.ID, occurredAt); err != nil {
					return fmt.Errorf("transition %s: consume human_decision evidence: %w", resolved.Spec.ID, err)
				}
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

			// A pause checkpoint is only meaningful while paused. After all
			// actions ran (TR-019's restore reads the checkpoint), any
			// transition out of `paused` (TR-019 restore, TR-020 amend,
			// TR-021 abort) must drop it, or the next pause would be
			// rejected as an overwrite.
			if lifecycle["state"] != "paused" {
				state["pause"] = nil
			}
			state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	}

	// Inject the root-aware semantic validator at the composition boundary.
	// Runtime owns the commit boundary but does not import this package.
	if request.ExpectedRevision < 0 {
		return store.UpdateCurrent(mutation)
	}
	return store.Update(request.ExpectedRevision, mutation)
}

// enforceForbiddenEvents is the Apply-time consumer of the Loop Definition's
// strong_block forbidden_events table (RC-06, S10-2). Before this barrier the
// table was only read by conformance tests; a caller with a hand-built
// evidence map could fire TR-015/TR-017 with an empty or stale clean round and
// the runtime would record the ACC anyway.
//
// Event → predicate mapping:
//   - create_acc_without_clean_round (TR-015 acceptance→release_audit): the
//     machine CleanRound must evaluate PASS over the current runtime.
//   - run_release_audit_without_acc (TR-017 release_audit→awaiting_human_release):
//     identical predicate — the audit's own release_audit_record evidence does
//     not substitute for the ACC precondition.
//
// The barrier is evaluated pre-Apply against the current snapshot (fail-closed
// on an unreadable review section) so no journal row is written for a refused
// mutation.
func enforceForbiddenEvents(catalog *Catalog, spec TransitionSpec, state map[string]any) error {
	fe, declared := catalog.ForbiddenEvents["create_acc_without_clean_round"]
	if !declared {
		// Fail closed: a definition that drops the declaration loses its
		// acceptance barrier and must refuse to run rather than degrade.
		return fmt.Errorf("loop definition no longer declares forbidden event create_acc_without_clean_round; refusal to evaluate TR-015/TR-017 without it")
	}
	switch spec.ID {
	case "TR-015", "TR-017":
	default:
		_ = fe
		return nil
	}
	result := verification.EvaluateCleanRound(state)
	if !result.Passed {
		return fmt.Errorf(
			"forbidden event %s: transition %s rejected — no valid clean round for review round %d: %v",
			fe.Event, spec.ID, result.ReviewRound, result.Reasons,
		)
	}
	return nil
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
			HumanDecisionScope: global.HumanDecisionScope,
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
	catalog := evidence.DefaultCatalog()
	for _, kind := range spec.RequiredEvidence {
		ref := strings.TrimSpace(request.Evidence[kind])
		if ref == "" {
			return fmt.Errorf("%s", catalog.MissingBindingMessage(spec.ID, kind))
		}
		if seen[ref] {
			return fmt.Errorf("transition %s reuses evidence %s for multiple requirements", spec.ID, ref)
		}
		seen[ref] = true
		if generatedEvidenceKind(kind) {
			if err := validateGeneratedEvidence(state, kind, ref, request); err != nil {
				return fmt.Errorf("transition %s evidence %s: %w", spec.ID, kind, err)
			}
		} else if !(spec.ID == "TR-001" || (spec.ID == "TR-020" && kind == "req_lock_record")) {
			if err := validateCurrentEvidence(root, state, kind, ref); err != nil {
				return fmt.Errorf("transition %s evidence %s: %w", spec.ID, kind, err)
			}
		}
	}
	if spec.ID == "TR-001" && request.REQ == nil {
		return fmt.Errorf("TR-001 requires locked REQ metadata")
	}
	if spec.ID == "TR-020" && request.REQ == nil {
		return fmt.Errorf("TR-020 requires the amended locked REQ metadata")
	}
	if spec.HumanDecisionScope != "" {
		if err := validateHumanDecisionScope(root, state, spec, request); err != nil {
			return fmt.Errorf("transition %s: %w", spec.ID, err)
		}
	}
	return nil
}

// ErrBaselineDrift marks the resume path's fingerprint-drift refusal so
// callers can route to amendment with errors.Is instead of string matching.
var ErrBaselineDrift = errors.New("baselines_unchanged")

func generatedEvidenceKind(kind string) bool {
	return evidence.DefaultCatalog().IsGenerated(kind)
}

func validateGeneratedEvidence(state map[string]any, kind, ref string, request Request) error {
	generator, ok := evidence.DefaultCatalog().Generator(kind)
	if !ok {
		return fmt.Errorf("unsupported generated evidence kind %s", kind)
	}
	if !generatedEvidenceReferenceMatches(strings.TrimSpace(ref), generator.Reference) {
		return fmt.Errorf("reference %q does not match catalog generator %q", ref, generator.Reference)
	}
	return nil
}

func generatedEvidenceReferenceMatches(ref, canonical string) bool {
	return ref == canonical
}

func validateCurrentEvidence(root string, state map[string]any, requiredKind, ref string) error {
	items, _ := state["evidence"].([]any)
	var evidenceItem map[string]any
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if item["id"] == ref || item["path"] == ref {
			evidenceItem = item
			break
		}
	}
	if evidenceItem == nil {
		return fmt.Errorf("reference %q is not current runtime evidence", ref)
	}
	if evidenceItem["status"] != "valid" {
		return fmt.Errorf("reference %q is not valid", ref)
	}
	actualKind, _ := evidenceItem["kind"].(string)
	if !evidence.DefaultCatalog().Accepts(requiredKind, actualKind) {
		return fmt.Errorf("reference %q has kind %q, incompatible with %s", ref, actualKind, requiredKind)
	}
	baseline, _ := state["baseline"].(map[string]any)
	if integer(evidenceItem["baseline_generation"]) != integer(baseline["generation"]) {
		return fmt.Errorf("reference %q belongs to baseline generation %d, current is %d", ref, integer(evidenceItem["baseline_generation"]), integer(baseline["generation"]))
	}
	if evidenceRound := integer(evidenceItem["review_round"]); evidenceRound > 0 {
		review, _ := state["review"].(map[string]any)
		if evidenceRound != integer(review["round"]) {
			return fmt.Errorf("reference %q belongs to review round %d, current is %d", ref, evidenceRound, integer(review["round"]))
		}
	}
	rel, _ := evidenceItem["path"].(string)
	clean := filepath.Clean(rel)
	if rel == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("reference %q has unsafe path %q", ref, rel)
	}
	data, err := os.ReadFile(filepath.Join(root, clean))
	if err != nil {
		return fmt.Errorf("read reference %q: %w", ref, err)
	}
	want, _ := evidenceItem["sha256"].(string)
	if actual := SHA256(data); want == "" || actual != want {
		return fmt.Errorf("reference %q fingerprint mismatch", ref)
	}
	return nil
}

func allowedEvidenceKinds(required string) []string {
	return evidence.DefaultCatalog().AcceptedKinds(required)
}

// validateHumanDecisionScope enforces that a human-boundary transition's
// human_decision evidence is scoped to the semantic verb and runtime identity.
// Runtime revision is deliberately not part of the Agent-facing evidence
// contract. The old @revision form remains readable for migration.
func validateHumanDecisionScope(root string, state map[string]any, spec TransitionSpec, request Request) error {
	runtimeID, _ := state["runtime_id"].(string)
	expected := fmt.Sprintf("%s:%s", spec.HumanDecisionScope, runtimeID)
	legacyExpected := fmt.Sprintf("%s@%d", expected, integer(state["revision"]))
	items, _ := state["evidence"].([]any)
	// Prefer the evidence bound to the canonical human_decision slot: stray
	// extra keys in request.Evidence must not widen the gate's match set.
	var preferred string
	if slot, ok := request.Evidence["human_decision_record"]; ok && strings.TrimSpace(slot) != "" {
		preferred = strings.TrimSpace(slot)
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil || item["kind"] != "human_decision" || item["status"] != "valid" {
			continue
		}
		idRef, _ := item["id"].(string)
		pathRef, _ := item["path"].(string)
		cited := preferred != "" && (preferred == idRef || preferred == pathRef)
		if !cited && preferred == "" {
			for _, value := range request.Evidence {
				if value == idRef || value == pathRef {
					cited = true
					break
				}
			}
		}
		if !cited {
			continue
		}
		if loopruntime.HumanDecisionEvidenceConsumed(item) {
			return fmt.Errorf("human_decision evidence %q was already consumed; create a new decision for transition %s", idRef, spec.ID)
		}
		refs := toStringSlice(item["scope_refs"])
		if containsString(refs, expected) || containsString(refs, legacyExpected) {
			if err := validateHumanDecisionArtifact(root, state, spec, item); err != nil {
				return err
			}
			return nil
		}
		return fmt.Errorf("human_decision evidence %q must include semantic scope %q in its scope_refs; human_decision_scope comes from Loop Definition, not the decision artifact", idRef, expected)
	}
	return fmt.Errorf("transition %s cites no current human_decision evidence; the decision must be registered and scoped to %q", spec.ID, expected)
}

// validateHumanDecisionArtifact binds JSON decision records to the exact
// lifecycle cursor, runtime, fixed transition and evidence ID they were
// created for. Markdown records remain readable during migration. The S7
// budget command has its own structured JSON contract and performs its target
// checks before handing return_to_governance to GTR-006.
func validateHumanDecisionArtifact(root string, state map[string]any, spec TransitionSpec, item map[string]any) error {
	pathRef := strings.TrimSpace(stringValue(item["path"]))
	if filepath.Ext(pathRef) != ".json" {
		return nil
	}
	if spec.ID == "GTR-006" {
		return nil
	}
	clean := filepath.Clean(pathRef)
	if pathRef == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("human_decision evidence %q has unsafe artifact path %q", stringValue(item["id"]), pathRef)
	}
	data, err := os.ReadFile(filepath.Join(root, clean))
	if err != nil {
		return fmt.Errorf("read human_decision artifact %q: %w", stringValue(item["id"]), err)
	}
	var artifact map[string]any
	if err := json.Unmarshal(data, &artifact); err != nil {
		return fmt.Errorf("decode human_decision artifact %q: %w", stringValue(item["id"]), err)
	}
	runtimeID := stringValue(state["runtime_id"])
	if value, ok := artifact["runtime_id"]; !ok || stringValue(value) == "" {
		return fmt.Errorf("human_decision artifact %q must declare runtime_id", stringValue(item["id"]))
	} else if stringValue(value) != runtimeID {
		return fmt.Errorf("human_decision artifact %q targets runtime %q, current runtime is %q", stringValue(item["id"]), stringValue(value), runtimeID)
	}
	if value, ok := artifact["decision_id"]; !ok || stringValue(value) == "" {
		return fmt.Errorf("human_decision artifact %q must declare decision_id", stringValue(item["id"]))
	} else if stringValue(value) != stringValue(item["id"]) {
		return fmt.Errorf("human_decision artifact %q declares decision_id %q", stringValue(item["id"]), stringValue(value))
	}
	wantDisposition := dispositionForHumanDecisionEvent(spec.Event)
	if wantDisposition != "" && stringValue(artifact["disposition"]) != wantDisposition {
		return fmt.Errorf("human_decision artifact %q disposition %q does not authorize %s", stringValue(item["id"]), stringValue(artifact["disposition"]), spec.ID)
	}
	rawCursor, ok := artifact["target_cursor"]
	if !ok {
		return fmt.Errorf("human_decision artifact %q must declare target_cursor", stringValue(item["id"]))
	}
	cursor, ok := rawCursor.(map[string]any)
	if !ok {
		return fmt.Errorf("human_decision artifact %q target_cursor must be an object", stringValue(item["id"]))
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	wantState := stringValue(lifecycle["state"])
	if stringValue(cursor["state"]) != wantState {
		return fmt.Errorf("human_decision artifact %q targets state %q, current state is %q", stringValue(item["id"]), stringValue(cursor["state"]), wantState)
	}
	if phaseValue(cursor["phase"]) != phaseValue(lifecycle["phase"]) {
		return fmt.Errorf("human_decision artifact %q targets phase %q, current phase is %q", stringValue(item["id"]), phaseValue(cursor["phase"]), phaseValue(lifecycle["phase"]))
	}
	return nil
}

func dispositionForHumanDecisionEvent(event string) string {
	switch event {
	case "human_release_approved":
		return "approve"
	case "human_release_deferred":
		return "defer"
	case "human_release_rejected_defect":
		return "reject_defect"
	case "human_release_rejected_acceptance":
		return "reject_acceptance"
	case "human_release_rejected_release_audit":
		return "reject_release_audit"
	case "human_release_aborted":
		return "abort"
	case "human_abort_approved":
		return "abort"
	default:
		return ""
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func phaseValue(value any) string {
	if value == nil {
		return ""
	}
	return stringValue(value)
}

func toStringSlice(value any) []string {
	switch values := value.(type) {
	case []any:
		out := make([]string, 0, len(values))
		for _, raw := range values {
			if s, _ := raw.(string); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{values}
	}
	return nil
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

// updateBoundREQ swaps the amended baseline into the running cycle: the new
// locked REQ becomes bound_req with a current-generation documents entry,
// while the superseded entry stays as locked history (the hook protects
// every locked req generation — see the hookctx loader). Runs after
// increment_baseline_generation, which already bumped the generation.
func updateBoundREQ(root string, state map[string]any, request Request, occurredAt time.Time) error {
	req := request.REQ
	if req.ID == "" || req.Path == "" || req.Version == "" || req.SHA256 == "" ||
		req.ApprovedBy == "" || req.ApprovedAt == "" {
		return fmt.Errorf("amended REQ metadata is incomplete")
	}
	data, err := os.ReadFile(filepath.Join(root, req.Path))
	if err != nil {
		return fmt.Errorf("read amended REQ: %w", err)
	}
	if SHA256(data) != req.SHA256 {
		return fmt.Errorf("amended REQ fingerprint mismatch")
	}
	status := ParseMarkdownField(string(data), "状态", "Status")
	if !strings.EqualFold(status, "locked") {
		return fmt.Errorf("amended REQ %s is not locked (status=%q)", req.ID, status)
	}
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil {
		return fmt.Errorf("runtime has no bound REQ to amend")
	}
	oldID, _ := bound["id"].(string)
	if oldID != "" && req.ID != oldID {
		return fmt.Errorf("amended REQ %q does not match the bound REQ %q — changing the target is an unbind + rebind, not an amendment", req.ID, oldID)
	}
	oldVersion, _ := bound["version"].(string)
	greater, parseable := versionStrictlyGreater(req.Version, oldVersion)
	if !parseable {
		return fmt.Errorf("REQ versions must be dotted numeric (got amended %q, bound %q)", req.Version, oldVersion)
	}
	if !greater {
		return fmt.Errorf("amended REQ version %q must strictly exceed the bound version %q", req.Version, oldVersion)
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
	state["documents"] = appendDocument(state["documents"], map[string]any{
		"id":         req.ID,
		"kind":       "req",
		"path":       req.Path,
		"version":    req.Version,
		"sha256":     req.SHA256,
		"status":     "locked",
		"generation": integer(baseline["generation"]),
	})
	state["authorization"] = map[string]any{
		"mode":        "binding",
		"command":     "loop-harness req amend",
		"actor":       request.Actor,
		"occurred_at": occurredAt.UTC().Format(time.RFC3339Nano),
	}
	return nil
}

// versionStrictlyGreater compares dotted numeric versions (an optional "v"
// prefix is tolerated). parseable is false when either side is not dotted
// numeric — callers must reject instead of guessing an ordering.
func versionStrictlyGreater(a, b string) (greater, parseable bool) {
	parse := func(v string) ([]int, bool) {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if v == "" {
			return nil, false
		}
		parts := strings.Split(v, ".")
		nums := make([]int, 0, len(parts))
		for _, part := range parts {
			if part == "" {
				return nil, false
			}
			n := 0
			for _, r := range part {
				if r < '0' || r > '9' {
					return nil, false
				}
				if n > (1<<31-1)/10 {
					return nil, false // numeric field would overflow — not a real version
				}
				n = n*10 + int(r-'0')
			}
			nums = append(nums, n)
		}
		return nums, true
	}
	na, okA := parse(a)
	nb, okB := parse(b)
	if !okA || !okB {
		return false, false
	}
	for i := 0; i < len(na) || i < len(nb); i++ {
		var x, y int
		if i < len(na) {
			x = na[i]
		}
		if i < len(nb) {
			y = nb[i]
		}
		if x != y {
			return x > y, true
		}
	}
	return false, true
}

// parseUIImpact reads the `UI impact` field from a locked REQ and returns
// one of {none, changed, unknown}. The third value is part of the canonical
// SM-003 planning-phase contract (LOOP-STATE-MACHINE.md §15): bindREQ
// accepts `unknown`, but the planning cannot advance until the REQ's §D
// (待澄清问题) clarifies it. The guard that enforces "unknown → planning paused"
// lives in guardUIIImpactResolved (registered as `ui_impact_resolved`).
func parseUIImpact(content string) (string, error) {
	value, err := parseUIImpactField(content)
	if err != nil {
		return "", err
	}
	// The REQ template carries a second UI-impact declaration in §C (a
	// human-facing reflection of the top anchor field). A drifted §C value
	// would silently route a `changed` requirement through the `none` path
	// Refuse the mismatch and name both values.
	if echo := sectionCUIImpact(content); echo != "" && !strings.EqualFold(echo, value) {
		return "", fmt.Errorf("REQ UI impact is inconsistent: top anchor field says %q but the §C reflection says %q — align them (the top field is the machine anchor; §C only reflects it)", value, echo)
	}
	return value, nil
}

// ParseUIImpactForTest exposes parseUIImpact for the drift pin test.
func ParseUIImpactForTest(content string) (string, error) { return parseUIImpact(content) }

// parseUIImpactField reads only the top blockquote `UI impact` anchor.
func parseUIImpactField(content string) (string, error) {
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

// sectionCUIImpact extracts the UI-impact value declared inside the §C
// section. The REQ template's reflection is a markdown table row
// `| UI impact（引自顶部） | none / changed / unknown（…） |` — the value is
// the first token of the second cell that matches one of the three legal
// values. A colon-form `UI impact：value` line is also accepted (free-form
// REQs). Empty when §C declares neither. Without the three-value filter the
// template's own placeholder row ("none / changed / unknown（…）") would be
// read as a mismatch on every template-conformant REQ.
func sectionCUIImpact(content string) string {
	inC := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			inC = strings.Contains(trimmed, "§C")
			continue
		}
		if !inC {
			continue
		}
		for _, separator := range []string{"：", ":"} {
			parts := strings.SplitN(trimmed, separator, 2)
			if len(parts) == 2 && strings.Contains(strings.ToLower(parts[0]), "ui impact") {
				if value := firstLegalUIImpact(parts[1]); value != "" {
					return value
				}
			}
		}
		if strings.HasPrefix(trimmed, "|") && strings.Contains(strings.ToLower(trimmed), "ui impact") {
			cells := strings.Split(strings.Trim(trimmed, "|"), "|")
			for _, cell := range cells {
				if value := firstLegalUIImpact(cell); value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func firstLegalUIImpact(cell string) string {
	lowered := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cell), "**")))
	for _, legal := range []string{"none", "changed", "unknown"} {
		if strings.HasPrefix(lowered, legal) {
			return legal
		}
	}
	return ""
}

// appendDocument appends a document entry unless a same-id entry of the
// same baseline generation already exists — that one is replaced in place.
// Same-generation re-registration happens when S6→S5 rework re-locks a
// revised contract: stacking entries would break subject fingerprint matching
// forever (each stale sha poisons the manifest).
func appendDocument(value any, document map[string]any) []any {
	documents, _ := value.([]any)
	id, _ := document["id"].(string)
	generation := integer(document["generation"])
	for i, raw := range documents {
		existing, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if existingID, _ := existing["id"].(string); existingID == id && integer(existing["generation"]) == generation {
			documents[i] = document
			return documents
		}
	}
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

	documents := documentFingerprints(state)

	state["pause"] = map[string]any{
		"from_state":            fromState,
		"from_phase":            fromPhase,
		"phase_revision":        phaseRevision,
		"baseline_generation":   baselineGeneration,
		"review_round":          reviewRound,
		"reason":                resolved.Spec.Description,
		"required_human_action": pauseRequiredAction(fromState),
		"document_fingerprints": documents,
		"paused_at":             occurredAt.UTC().Format(time.RFC3339Nano),
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
			return fmt.Errorf("%w: cannot read %s: %w", ErrBaselineDrift, path, err)
		}
		actualSHA := fmt.Sprintf("%x", sha256.Sum256(data))
		if actualSHA != expectedSHA {
			return fmt.Errorf(
				"%w: %s fingerprint drifted (pause recorded %s, actual %s)",
				ErrBaselineDrift, path, expectedSHA[:12], actualSHA[:12])
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
// semantics as invalidateAllDownstream. It returns the number of evidence
// entries newly invalidated (RC-05: callers that accept an explicit "all"
// sweep must be able to report what actually changed).
func invalidateAllDownstreamAction(state map[string]any, source string) int {
	evidenceSlice, ok := state["evidence"].([]any)
	if !ok {
		return 0
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
	invalidated := impact.InvalidateEvidence(state, impacts, source)
	return len(invalidated)
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
