// Package transition implements the Loop Definition transition engine.
//
// The transition catalog (this file) is the single source of truth for which
// guards, actions, transitions, phase transitions and global transitions are
// legal. It is loaded once at startup from docs/loop-definition.json and is
// fail-closed: any declared identifier (guard, action, transition, phase,
// global, entity lifecycle transition, forbidden event) that does not have a
// registered implementation causes LoadCatalog to return an error.
//
// The registry maps (per BUG-001 §4b.2) cover:
//   - guardRegistry in guards.go        — one GuardFn per declared name
//   - actionRegistry in actions.go      — one ActionFn per declared name
//   - Transitions / PhaseTransitions    — keyed by transition ID
//   - GlobalTransitions                 — scanned at resolve time by (event, source state)
//
// The Catalog struct mirrors the loop-definition.json top-level keys so the
// runtime, doctor, and tests can introspect the spec exhaustively.
package transition

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// StateSpec is the per-state entry in loop-definition.json states map.
type StateSpec struct {
	Stage          string  `json:"stage"`
	Category       string  `json:"category"`
	Description    string  `json:"description"`
	PhaseMachine   *string `json:"phase_machine"`
	EntryCondition string  `json:"entry_condition"`
	ExitCondition  string  `json:"exit_condition"`
}

// TransitionSpec is the canonical transition spec used for top-level and phase
// transitions. Loop Definition JSON uses this exact shape for both.
type TransitionSpec struct {
	ID               string            `json:"id"`
	From             string            `json:"from"`
	FromPhase        string            `json:"from_phase,omitempty"`
	Selector         string            `json:"selector,omitempty"`
	Automation       *AutomationPolicy `json:"automation,omitempty"`
	Event            string            `json:"event"`
	To               string            `json:"to"`
	Actors           []string          `json:"actors"`
	Guards           []string          `json:"guards"`
	Actions          []string          `json:"actions"`
	RequiredEvidence []string          `json:"required_evidence"`
	OnGuardFailure   string            `json:"on_guard_failure,omitempty"`
	AutoTrigger      *AutoTriggerSpec  `json:"auto_trigger,omitempty"`
	Description      string            `json:"description"`
}

// AutoTriggerSpec is the canonical hook-trigger metadata attached to a
// transition. Events is retained only as a migration-reader field for early
// Loop Definition drafts; new definitions must use Event.
type AutoTriggerSpec struct {
	Enabled       bool     `json:"enabled"`
	Event         string   `json:"event"`
	Events        []string `json:"events,omitempty"`
	Actor         string   `json:"actor"`
	QualityGateID string   `json:"quality_gate_id"`
	MaxPerEvent   int      `json:"max_per_event"`
	HumanRequired bool     `json:"human_required"`
}

// AutomationPolicy declares whether a transition is eligible for the
// hook-controlled automatic path. Human-boundary transitions must declare the
// exclusion in the Loop Definition instead of relying on a Go ID deny-list.
type AutomationPolicy struct {
	Eligible      bool `json:"eligible"`
	HumanBoundary bool `json:"human_boundary"`
}

// GlobalTransitionSpec matches loop-definition.json global_transitions entries.
// FromStates is a list because GTRs are dispatchable from every listed source.
type GlobalTransitionSpec struct {
	ID               string            `json:"id"`
	FromStates       []string          `json:"from"`
	Selector         string            `json:"selector,omitempty"`
	Automation       *AutomationPolicy `json:"automation,omitempty"`
	Event            string            `json:"event"`
	To               string            `json:"to"`
	Actors           []string          `json:"actors"`
	Guards           []string          `json:"guards"`
	Actions          []string          `json:"actions"`
	RequiredEvidence []string          `json:"required_evidence"`
	OnGuardFailure   string            `json:"on_guard_failure,omitempty"`
	AutoTrigger      *AutoTriggerSpec  `json:"auto_trigger,omitempty"`
	Description      string            `json:"description"`
}

// EntityLifecycleSpec is the per-entity-lifecycle descriptor.
type EntityLifecycleSpec struct {
	InitialState   string           `json:"initial_state"`
	TerminalStates []string         `json:"terminal_states"`
	States         []string         `json:"states"`
	Transitions    []TransitionSpec `json:"transitions"`
}

// EntityTransition is a named entity-lifecycle transition (used as the
// resolved-transition row in conformance tests).
type EntityTransition struct {
	Entity string
	Spec   TransitionSpec
}

// LoopDefinition mirrors the loop-definition.json top-level shape. Only the
// keys TASK-013 needs are decoded; unknown keys are tolerated because the
// upstream schema validates full structure.
type LoopDefinition struct {
	SchemaVersion       string                         `json:"schema_version"`
	DefinitionID        string                         `json:"definition_id"`
	Status              string                         `json:"status"`
	InitialState        string                         `json:"initial_state"`
	TerminalStates      []string                       `json:"terminal_states"`
	States              map[string]StateSpec           `json:"states"`
	PhaseMachines       map[string]PhaseMachineSpec    `json:"phase_machines"`
	EntityLifecycles    map[string]EntityLifecycleSpec `json:"entity_lifecycles"`
	Transitions         []TransitionSpec               `json:"transitions"`
	GlobalTransitions   []GlobalTransitionSpec         `json:"global_transitions"`
	ForbiddenEvents     []ForbiddenEventSpec           `json:"forbidden_events"`
	Invariants          []InvariantSpec                `json:"invariants"`
	QualityCycleTimeout string                         `json:"quality_cycle_timeout,omitempty"`
}

// PhaseMachineSpec is one entry in phase_machines.
type PhaseMachineSpec struct {
	OwnerState     string                    `json:"owner_state"`
	InitialPhase   string                    `json:"initial_phase"`
	EntryPhases    []string                  `json:"entry_phases"`
	TerminalPhases []string                  `json:"terminal_phases"`
	Phases         map[string]map[string]any `json:"phases"`
	Transitions    []TransitionSpec          `json:"transitions"`
}

// ForbiddenEventSpec matches loop-definition.json forbidden_events entries.
type ForbiddenEventSpec struct {
	Event       string `json:"event"`
	Enforcement string `json:"enforcement"`
	Reason      string `json:"reason"`
}

// InvariantSpec matches loop-definition.json invariants entries.
type InvariantSpec struct {
	ID          string `json:"id"`
	Statement   string `json:"statement"`
	Enforcement string `json:"enforcement"`
}

// Catalog is the compiled, fail-closed view of the Loop Definition. The
// registry fields (Guards, Actions) are package-level maps in guards.go and
// actions.go; we reference them through Lookup helpers instead of embedding
// here so the registry remains a singleton maintained in one place.
type Catalog struct {
	Definition          *LoopDefinition
	Transitions         map[string]TransitionSpec     // top-level, keyed by id
	PhaseTransitions    map[string]PhaseTransitionKey // keyed by "<owner>.<from>" for fast lookup
	PhaseTransitionSpec map[string]TransitionSpec     // keyed by transition id (for resolve)
	GlobalTransitions   []GlobalTransitionSpec
	EntityTransitions   map[string]EntityTransition // keyed by "<entity>:<id>"
	ForbiddenEvents     map[string]ForbiddenEventSpec
}

// PhaseTransitionKey identifies a phase transition by owner.from. The same id
// may appear in two phase machines, so we key by owner+from for the fast-path
// used by resolve().
type PhaseTransitionKey struct {
	Owner string
	From  string
}

// Cursor is the runtime state/phase pair used by the automatic selector.
// Phase is empty for states without a phase machine.
type Cursor struct {
	State string
	Phase string
}

// GateOutcome is a qualified fact supplied by the Quality Gate layer. This
// package consumes the result but does not evaluate evidence or write Runtime.
type GateOutcome struct {
	GateID string
	Status string
}

// TriggerFacts are the already-qualified requested events and gate outcomes
// observed for one control cycle.
type TriggerFacts struct {
	RequestedEvents []string
	GateOutcomes    []GateOutcome
}

// TriggerResolution contains zero or one selected transition. A selector
// conflict is represented by an error and never by a best-effort selection.
type TriggerResolution struct {
	Transition *TransitionSpec
}

// TriggerConflictCode is the stable caller-visible code for mutually valid
// automatic trigger candidates.
const TriggerConflictCode = "LOOP_TRIGGER_CONFLICT"

// TriggerConflictError reports all candidates that matched the current facts.
// CandidateIDs are diagnostic only; their ordering never determines a choice.
type TriggerConflictError struct {
	Cursor       Cursor
	CandidateIDs []string
}

func (e *TriggerConflictError) Error() string {
	return fmt.Sprintf("%s at %s.%s: candidates=%v", TriggerConflictCode, e.Cursor.State, e.Cursor.Phase, e.CandidateIDs)
}

// Code exposes the stable error code without requiring callers to parse text.
func (e *TriggerConflictError) Code() string { return TriggerConflictCode }

// LoadCatalog reads loop-definition.json and resolves every declared identifier
// against the registered guard and action registries. Returns an error if any
// declared guard, action, transition, or forbidden event is missing.
func LoadCatalog(root string) (*Catalog, error) {
	defPath := filepath.Join(root, "docs", "loop-definition.json")
	data, err := os.ReadFile(defPath)
	if err != nil {
		return nil, fmt.Errorf("read Loop Definition: %w", err)
	}
	var def LoopDefinition
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("decode Loop Definition: %w", err)
	}

	catalog := &Catalog{
		Definition:          &def,
		Transitions:         make(map[string]TransitionSpec, len(def.Transitions)),
		PhaseTransitions:    make(map[string]PhaseTransitionKey),
		PhaseTransitionSpec: make(map[string]TransitionSpec),
		GlobalTransitions:   append([]GlobalTransitionSpec(nil), def.GlobalTransitions...),
		EntityTransitions:   make(map[string]EntityTransition),
		ForbiddenEvents:     make(map[string]ForbiddenEventSpec, len(def.ForbiddenEvents)),
	}

	// Top-level transitions.
	for _, t := range def.Transitions {
		if _, dup := catalog.Transitions[t.ID]; dup {
			return nil, fmt.Errorf("duplicate transition id %q", t.ID)
		}
		catalog.Transitions[t.ID] = t
		if err := validateActors(t.ID, t.Actors); err != nil {
			return nil, err
		}
		if err := resolveTransitionSpec(t.ID, t.Guards, t.Actions); err != nil {
			return nil, err
		}
		if err := validateAutoTrigger(t.ID, t.From, t.FromPhase, t.To, t.Actors, t.Automation, t.AutoTrigger); err != nil {
			return nil, err
		}
	}

	// Phase transitions.
	for owner, machine := range def.PhaseMachines {
		for _, t := range machine.Transitions {
			if _, dup := catalog.PhaseTransitionSpec[t.ID]; dup {
				return nil, fmt.Errorf("duplicate transition id %q", t.ID)
			}
			catalog.PhaseTransitionSpec[t.ID] = t
			catalog.PhaseTransitions[phaseKey(owner, t.From)] = PhaseTransitionKey{Owner: owner, From: t.From}
			if err := validateActors(t.ID, t.Actors); err != nil {
				return nil, err
			}
			if err := resolveTransitionSpec(t.ID, t.Guards, t.Actions); err != nil {
				return nil, err
			}
			if err := validateAutoTrigger(t.ID, owner, t.From, t.To, t.Actors, t.Automation, t.AutoTrigger); err != nil {
				return nil, err
			}
		}
	}
	if err := validateAutomaticCandidateSelectors(def); err != nil {
		return nil, err
	}

	// Entity-lifecycle transitions. These reference guard names that must
	// resolve against the same registry as top-level transitions.
	for entity, life := range def.EntityLifecycles {
		for _, t := range life.Transitions {
			catalog.EntityTransitions[entity+":"+t.ID] = EntityTransition{Entity: entity, Spec: t}
			if err := resolveTransitionSpec(entity+"."+t.ID, t.Guards, t.Actions); err != nil {
				return nil, err
			}
		}
	}

	// Global transitions.
	for _, t := range def.GlobalTransitions {
		if err := validateActors(t.ID, t.Actors); err != nil {
			return nil, err
		}
		if err := resolveTransitionSpec(t.ID, t.Guards, t.Actions); err != nil {
			return nil, err
		}
		if err := validateGlobalAutoTrigger(t); err != nil {
			return nil, err
		}
	}

	// Forbidden events.
	for _, fe := range def.ForbiddenEvents {
		catalog.ForbiddenEvents[fe.Event] = fe
	}

	return catalog, nil
}

var registeredQualityGates = map[string]struct{}{
	"GATE-PLANNING-DESIGN-COMPLETE":         {},
	"GATE-PLANNING-CONTRACTS-COMPLETE":      {},
	"GATE-PLANNING-TASKS-COMPLETE":          {},
	"GATE-DOCUMENT-PASS":                    {},
	"GATE-DOCUMENT-FIX-REQUIRED":            {},
	"GATE-BUILDER-BATCH-READY":              {},
	"GATE-EXECUTION-SPEC-CHANGE-REQUIRED":   {},
	"GATE-VERIFY-BLOCKING-FINDING":          {},
	"GATE-VERIFY-CLEAN-ROUND-PASSED":        {},
	"GATE-VERIFY-REQ-CHANGE-REQUIRED":       {},
	"GATE-VERIFY-RELEASE-BLOCKED":           {},
	"GATE-VERIFY-DELIVERY-PASS":             {},
	"GATE-VERIFY-QA-PASS":                   {},
	"GATE-VERIFY-E2E-PASS":                  {},
	"GATE-VERIFY-CLEAN-ROUND-VALID":         {},
	"GATE-CLEAN-ROUND-INCOMPLETE":           {},
	"GATE-TARGETED-REVERIFICATION-COMPLETE": {},
	"GATE-REPAIR-SPEC-CHANGE-REQUIRED":      {},
	"GATE-REPAIR-REQ-CHANGE-REQUIRED":       {},
	"GATE-ACCEPTANCE-COMPLETE":              {},
	"GATE-ACCEPTANCE-REVIEW-REQUIRED":       {},
	"GATE-RELEASE-AUDIT-APPROVED":           {},
	"GATE-RELEASE-AUDIT-BLOCKED":            {},
	"GATE-NO-REPAIR-REMAINS":                {},
	"GATE-FINDING-SPEC-CHANGE-REQUIRED":     {},
	"GATE-FINDING-REQ-CHANGE-REQUIRED":      {},
	"GATE-BUG-DRAFTS-READY":                 {},
	"GATE-CANONICAL-BUGS-ACCEPTED":          {},
	"GATE-BUG-REPORTS-REJECTED":             {},
	"GATE-REPAIR-BUILDERS-ACTIVATED":        {},
	"GATE-REPAIR-BATCH-REPORTED":            {},
	"GATE-TARGETED-REVERIFICATION-PASS":     {},
	"GATE-TARGETED-REVERIFICATION-FAIL":     {},
}

var canonicalActors = map[string]struct{}{
	"user": {}, "orchestrator": {}, "document_verifier": {},
	"frontend_builder": {}, "backend_builder": {}, "test_builder": {},
	"delivery_verifier": {}, "qa": {}, "e2e_tester": {},
	"release_auditor": {}, "hook": {}, "hook_controller": {}, "system": {},
}

func validateActors(id string, actors []string) error {
	if len(actors) == 0 {
		return fmt.Errorf("transition %s actors must not be empty", id)
	}
	for _, actor := range actors {
		if _, ok := canonicalActors[actor]; !ok {
			return fmt.Errorf("transition %s declares unregistered actor %q", id, actor)
		}
	}
	return nil
}

func validateAutoTrigger(id, owner, fromPhase, to string, actors []string, automation *AutomationPolicy, auto *AutoTriggerSpec) error {
	if auto == nil {
		return nil
	}
	if auto.Event == "" {
		if len(auto.Events) != 1 {
			return fmt.Errorf("transition %s auto trigger must use canonical event", id)
		}
		auto.Event = auto.Events[0]
		auto.Events = nil
	} else if len(auto.Events) != 0 {
		return fmt.Errorf("transition %s auto trigger cannot declare both event and events", id)
	}
	if auto.Event != "PreToolUse" {
		return fmt.Errorf("transition %s auto trigger event %q is not PreToolUse", id, auto.Event)
	}
	if auto.Actor == "" {
		return fmt.Errorf("transition %s auto trigger actor is required", id)
	}
	if auto.Actor != "hook_controller" {
		return fmt.Errorf("transition %s auto trigger actor must be canonical hook_controller, got %q", id, auto.Actor)
	}
	if !containsString(actors, auto.Actor) {
		return fmt.Errorf("transition %s auto trigger actor %q is not declared in actors", id, auto.Actor)
	}
	if _, ok := registeredQualityGates[auto.QualityGateID]; !ok {
		return fmt.Errorf("transition %s declares unregistered quality gate %q", id, auto.QualityGateID)
	}
	if auto.HumanRequired {
		return fmt.Errorf("transition %s is human-required and cannot be automatic", id)
	}
	if automation == nil {
		return fmt.Errorf("transition %s auto trigger requires automation eligibility declaration", id)
	}
	if !automation.Eligible || automation.HumanBoundary {
		return fmt.Errorf("transition %s auto trigger violates automation eligibility", id)
	}
	if auto.MaxPerEvent != 1 {
		return fmt.Errorf("transition %s auto trigger max_per_event must be 1, got %d", id, auto.MaxPerEvent)
	}
	if owner == "" || to == "" {
		return fmt.Errorf("transition %s auto trigger has incomplete cursor", id)
	}
	return nil
}

func validateGlobalAutoTrigger(t GlobalTransitionSpec) error {
	if t.AutoTrigger == nil {
		return nil
	}
	return validateAutoTrigger(t.ID, "global", t.Event, t.To, t.Actors, t.Automation, t.AutoTrigger)
}

func validateAutomaticCandidateSelectors(def LoopDefinition) error {
	groups := make(map[string][]TransitionSpec)
	for _, t := range def.Transitions {
		if t.AutoTrigger == nil || !t.AutoTrigger.Enabled {
			continue
		}
		machine, hasMachine := def.PhaseMachines[t.From]
		if t.FromPhase != "" {
			groups[phaseKey(t.From, t.FromPhase)] = append(groups[phaseKey(t.From, t.FromPhase)], t)
		} else if hasMachine {
			for phase := range machine.Phases {
				groups[phaseKey(t.From, phase)] = append(groups[phaseKey(t.From, phase)], t)
			}
		} else {
			groups[phaseKey(t.From, "")] = append(groups[phaseKey(t.From, "")], t)
		}
	}
	for owner, machine := range def.PhaseMachines {
		for _, t := range machine.Transitions {
			if t.AutoTrigger == nil || !t.AutoTrigger.Enabled {
				continue
			}
			groups[phaseKey(owner, t.From)] = append(groups[phaseKey(owner, t.From)], t)
		}
	}
	for cursor, candidates := range groups {
		selector := candidates[0].Selector
		if selector == "" {
			return fmt.Errorf("automatic cursor %s has automatic candidate without selector", cursor)
		}
		for _, candidate := range candidates[1:] {
			if candidate.Selector != selector {
				return fmt.Errorf("automatic cursor %s has multiple selectors %q and %q", cursor, selector, candidate.Selector)
			}
		}
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// resolveTransitionSpec fails closed when any declared guard or action in the
// supplied transition spec is not registered. Used by LoadCatalog for every
// top-level, phase, entity, and global transition.
func resolveTransitionSpec(id string, guards, actions []string) error {
	for _, g := range guards {
		if _, ok := LookupGuard(g); !ok {
			return fmt.Errorf("transition %s declares unregistered guard %q", id, g)
		}
	}
	for _, a := range actions {
		if _, ok := LookupAction(a); !ok {
			return fmt.Errorf("transition %s declares unregistered action %q", id, a)
		}
	}
	return nil
}

// SortedTransitionIDs returns the declared top-level transition IDs in a
// deterministic order. Used by conformance tests and doctor output.
func (c *Catalog) SortedTransitionIDs() []string {
	ids := make([]string, 0, len(c.Transitions))
	for id := range c.Transitions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// SortedGlobalTransitionIDs returns the global transition IDs in a
// deterministic order.
func (c *Catalog) SortedGlobalTransitionIDs() []string {
	ids := make([]string, 0, len(c.GlobalTransitions))
	for _, t := range c.GlobalTransitions {
		ids = append(ids, t.ID)
	}
	sort.Strings(ids)
	return ids
}

// SortedForbiddenEvents returns the forbidden-event names in a deterministic
// order.
func (c *Catalog) SortedForbiddenEvents() []string {
	ids := make([]string, 0, len(c.ForbiddenEvents))
	for id := range c.ForbiddenEvents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (c *Catalog) automaticCandidates(cursor Cursor) []TransitionSpec {
	var candidates []TransitionSpec
	for _, spec := range c.Definition.Transitions {
		if spec.From != cursor.State || spec.AutoTrigger == nil || !spec.AutoTrigger.Enabled {
			continue
		}
		if spec.FromPhase != "" && spec.FromPhase != cursor.Phase {
			continue
		}
		candidates = append(candidates, spec)
	}
	if machine, ok := c.Definition.PhaseMachines[cursor.State]; ok && cursor.Phase != "" {
		for _, spec := range machine.Transitions {
			if spec.From != cursor.Phase || spec.AutoTrigger == nil || !spec.AutoTrigger.Enabled {
				continue
			}
			candidates = append(candidates, spec)
		}
	}
	return candidates
}

// ResolveAutomaticTransition applies one selector seam for a single control
// cycle. Qualified requested events and satisfied gate outcomes are inputs;
// evaluation and Runtime mutation belong to later tasks. It returns zero or
// one transition and never chooses by declaration order, sorting or priority.
func (c *Catalog) ResolveAutomaticTransition(cursor Cursor, facts TriggerFacts) (TriggerResolution, error) {
	requested := make(map[string]struct{}, len(facts.RequestedEvents))
	for _, event := range facts.RequestedEvents {
		requested[event] = struct{}{}
	}
	satisfiedGates := make(map[string]struct{}, len(facts.GateOutcomes))
	for _, outcome := range facts.GateOutcomes {
		if outcome.Status == "satisfied" {
			satisfiedGates[outcome.GateID] = struct{}{}
		}
	}

	var matches []TransitionSpec
	for _, candidate := range c.automaticCandidates(cursor) {
		_, eventMatch := requested[candidate.Event]
		_, gateMatch := satisfiedGates[candidate.AutoTrigger.QualityGateID]
		if eventMatch || gateMatch {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return TriggerResolution{}, nil
	}
	if len(matches) > 1 {
		ids := make([]string, 0, len(matches))
		for _, match := range matches {
			ids = append(ids, match.ID)
		}
		sort.Strings(ids) // deterministic diagnostic only; never a selection rule.
		return TriggerResolution{}, &TriggerConflictError{Cursor: cursor, CandidateIDs: ids}
	}
	selected := matches[0]
	return TriggerResolution{Transition: &selected}, nil
}

func phaseKey(owner, from string) string {
	return owner + "." + from
}
