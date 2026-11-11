package qualitygate

import (
	"fmt"
	"sort"

	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// GateSpec binds one registered Quality Gate to its declared transition.
type GateSpec struct {
	ID                   string
	TransitionID         string
	TransitionEvent      string
	CursorState          string
	CursorPhase          string
	SemanticVersion      string
	EvidenceRequirements []EvidenceRequirement
}

// EvidenceRequirement describes one qualified semantic record needed by a
// gate. The evaluator follows the current runtime, generation and Plan identity.
type EvidenceRequirement struct {
	Kind              string
	Responsibilities  []string
	Conclusions       []string
	MinCount          int
	CurrentReviewPlan bool
	RequestedEvent    string
	// ProducedByTransition marks evidence that is materialized by the
	// candidate transition itself (for example pause_record). It is a
	// postcondition, not a pre-transition gate input; treating it as a normal
	// prerequisite creates an evaluator/transition deadlock.
	ProducedByTransition bool
	// RoutingVerdict marks gates that wait for one rare routing verdict among
	// many ordinary results (S7 pause verdicts). A current-plan envelope
	// whose conclusion is a different verdict is a normal state, not a
	// naming error — mismatch never escalates to a conflict.
	RoutingVerdict bool
}

// Registry is the deterministic set of automatic Quality Gates declared by
// the Loop Definition catalog.
type Registry struct {
	gates map[string]GateSpec
}

// NewRegistry derives the gate registry from the authoritative transition
// catalog instead of maintaining another transition graph.
func NewRegistry(catalog *transition.Catalog) (*Registry, error) {
	if catalog == nil {
		return nil, fmt.Errorf("quality gate catalog is required")
	}

	registry := &Registry{gates: make(map[string]GateSpec)}
	add := func(transitionID, cursorState, cursorPhase string, spec transition.TransitionSpec) error {
		if spec.AutoTrigger == nil || !spec.AutoTrigger.Enabled {
			return nil
		}
		gateID := spec.AutoTrigger.QualityGateID
		if existing, ok := registry.gates[gateID]; ok {
			return fmt.Errorf("quality gate %s is declared by both %s and %s", gateID, existing.TransitionID, transitionID)
		}
		declared, defined := semanticRequirements[gateID]
		requirements := cloneRequirements(declared)
		// Empty evidence requirements are intentional for state-based gates.
		// An unrelated unimplemented gate must not prevent evaluation of the
		// current cursor; Evaluate reports it if it becomes a candidate.
		version := ""
		if defined && (len(requirements) > 0 || isReviewRoundGate(gateID)) {
			version = "1.0.0"
		}
		catalog := evidence.DefaultCatalog()
		for _, requirement := range requirements {
			if err := catalog.ValidateSlots([]string{requirement.Kind}); err != nil {
				return fmt.Errorf("quality gate %s evidence contract: %w", gateID, err)
			}
		}
		for index := range requirements {
			if _, generated := catalog.Generator(requirements[index].Kind); generated {
				requirements[index].ProducedByTransition = true
			}
		}
		registry.gates[gateID] = GateSpec{
			ID:                   gateID,
			TransitionID:         transitionID,
			TransitionEvent:      spec.Event,
			CursorState:          cursorState,
			CursorPhase:          cursorPhase,
			SemanticVersion:      version,
			EvidenceRequirements: cloneRequirements(requirements),
		}
		return nil
	}
	for id, spec := range catalog.Transitions {
		if err := add(id, spec.From, spec.FromPhase, spec); err != nil {
			return nil, err
		}
	}
	for owner, machine := range catalog.Definition.PhaseMachines {
		for _, spec := range machine.Transitions {
			if err := add(spec.ID, owner, spec.From, spec); err != nil {
				return nil, err
			}
		}
	}
	return registry, nil
}

// ValidateEvidenceCatalog loads the repository's transition catalog and
// proves every automatic Quality Gate requirement is closed by the shared
// evidence catalog. It is used by doctor/validate so a gate-only compatibility
// drift is reported before an operator attempts a transition.
func ValidateEvidenceCatalog(root string) error {
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		return fmt.Errorf("quality gate evidence catalog: load transitions: %w", err)
	}
	registry, err := NewRegistry(catalog)
	if err != nil {
		return fmt.Errorf("quality gate evidence catalog: %w", err)
	}
	for _, id := range registry.IDs() {
		spec, _ := registry.Lookup(id)
		if spec.SemanticVersion == "" {
			return fmt.Errorf("quality gate evidence catalog: quality gate %s has no semantic definition", id)
		}
	}
	return nil
}

// Lookup returns the registered gate definition.
func (r *Registry) Lookup(id string) (GateSpec, bool) {
	if r == nil {
		return GateSpec{}, false
	}
	spec, ok := r.gates[id]
	if !ok {
		return GateSpec{}, false
	}
	return cloneGateSpec(spec), true
}

// IDs returns all registered gate IDs in deterministic order.
func (r *Registry) IDs() []string {
	if r == nil {
		return nil
	}
	ids := make([]string, 0, len(r.gates))
	for id := range r.gates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (r *Registry) specsForCursor(state, phase string) []GateSpec {
	if r == nil {
		return nil
	}
	var specs []GateSpec
	for _, spec := range r.gates {
		if spec.CursorState == state && (spec.CursorPhase == "" || spec.CursorPhase == phase) {
			specs = append(specs, cloneGateSpec(spec))
		}
	}
	sort.Slice(specs, func(i, j int) bool {
		return specs[i].TransitionID < specs[j].TransitionID
	})
	return specs
}

func cloneGateSpec(source GateSpec) GateSpec {
	source.EvidenceRequirements = cloneRequirements(source.EvidenceRequirements)
	return source
}

var semanticRequirements = map[string][]EvidenceRequirement{
	"GATE-PLANNING-DESIGN-COMPLETE": {
		requirement("planning_design_record", []string{"Architect", "Orchestrator"}, []string{"pass"}),
	},
	"GATE-PLANNING-CONTRACTS-COMPLETE": {
		requirement("planning_contract_record", []string{"Contract Planner", "Orchestrator"}, []string{"pass"}),
	},
	"GATE-PLANNING-TASKS-COMPLETE": {
		requirement("planning_task_record", []string{"Task Planner", "Orchestrator"}, []string{"pass"}),
	},
	"GATE-DOCUMENT-PASS": {
		currentPlanRequirement("document_review_record", []string{"DV-SPEC-CONSISTENCY"}, []string{"pass"}),
		currentPlanRequirement("document_review_record", []string{"DV-TASK-EXECUTABILITY"}, []string{"pass"}),
	},
	"GATE-DOCUMENT-FIX-REQUIRED": {
		requestedRequirement("document_review_record", []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"}, []string{"fix_required"}, "document_fix_required"),
	},
	"GATE-BUILDER-BATCH-READY": {
		// L3-S6 §8.3: the S6 exit no longer demands team_manifest_record —
		// the S7 workgroup can only be registered after TR-006 lands in
		// verification.delivery, so requiring its evidence here forced
		// placeholder records. Verification planning now starts from the
		// real integrated diff at the S7 entry.
		requirement("completion_report", []string{"BUILD-WORK-PACKAGE", "Builder"}, []string{"completed"}),
	},
	"GATE-EXECUTION-SPEC-CHANGE-REQUIRED": {
		requestedRequirement("change_impact_record", []string{"Builder", "BUILD-WORK-PACKAGE"}, []string{"spec_change_required"}, "execution_spec_change_required"),
	},
	// These exits consume accepted Findings and required responsibility
	// progress directly; neither needs an additional evidence envelope.
	"GATE-VERIFY-BLOCKING-FINDING":   {},
	"GATE-VERIFY-CLEAN-ROUND-PASSED": {},
	"GATE-VERIFY-REQ-CHANGE-REQUIRED": {
		// L3-S7 P0: the verdict transaction already created the single
		// authoritative pause checkpoint; the gate consumes the ReviewResult
		// verdict alone (no dual pause carrier). RoutingVerdict: ordinary
		// pass/finding results are a normal state, not a naming conflict.
		currentPlanRoutingRequirement("review_result_record", []string{"Delivery Verifier", "QA", "E2E Browser"}, []string{"req_change_required"}),
	},
	"GATE-VERIFY-RELEASE-BLOCKED": {
		currentPlanRoutingRequirement("review_result_record", []string{"Delivery Verifier", "QA", "E2E Browser"}, []string{"release_blocked"}),
	},
	"GATE-TARGETED-REVERIFICATION-COMPLETE": {
		currentPlanRequirement("targeted_reverification_record", []string{"Original Finder"}, []string{"pass"}),
		requirement("change_impact_record", []string{"Builder", "BUILD-WORK-PACKAGE"}, []string{"recorded"}),
	},
	"GATE-REPAIR-SPEC-CHANGE-REQUIRED": {
		requestedRequirement("change_impact_record", []string{"Builder", "BUILD-WORK-PACKAGE"}, []string{"spec_change_required"}, "repair_spec_change_required"),
		requirement("repair_record", []string{"Builder", "BUILD-WORK-PACKAGE"}, []string{"reported"}),
	},
	"GATE-REPAIR-REQ-CHANGE-REQUIRED": {
		requestedRequirement("repair_record", []string{"Builder", "BUILD-WORK-PACKAGE"}, []string{"req_change_required"}, "repair_req_change_required"),
		requirement("pause_record", []string{"Orchestrator"}, []string{"recorded"}),
	},
	"GATE-ACCEPTANCE-COMPLETE": {
		requirement("acceptance_record", []string{"Acceptance", "Orchestrator"}, []string{"pass"}),
	},
	"GATE-ACCEPTANCE-REVIEW-REQUIRED": {
		requestedRequirement("acceptance_record", []string{"Acceptance", "Orchestrator"}, []string{"review_required"}, "acceptance_review_required"),
		requirement("change_impact_record", []string{"Acceptance", "Orchestrator"}, []string{"recorded"}),
	},
	"GATE-RELEASE-AUDIT-REVIEW-REQUIRED": {
		requestedRequirement("acceptance_record", []string{"Acceptance", "Orchestrator"}, []string{"review_required"}, "acceptance_review_required"),
		requirement("change_impact_record", []string{"Acceptance", "Orchestrator"}, []string{"recorded"}),
	},
	"GATE-RELEASE-AUDIT-APPROVED": {
		requirement("release_audit_record", []string{"Release Auditor"}, []string{"approved", "approved_with_risk"}),
		requirement("acceptance_record", []string{"Acceptance", "Orchestrator"}, []string{"pass"}),
	},
	"GATE-RELEASE-AUDIT-BLOCKED": {
		requestedRequirement("release_audit_record", []string{"Release Auditor"}, []string{"blocked"}, "release_audit_blocked"),
		requirement("pause_record", []string{"Orchestrator"}, []string{"recorded"}),
	},
	"GATE-NO-REPAIR-REMAINS": {
		requirement("bug_batch_record", []string{"Orchestrator"}, []string{"no_repair"}),
	},
	"GATE-FINDING-SPEC-CHANGE-REQUIRED": {
		requestedRequirement("bug_batch_record", []string{"Orchestrator"}, []string{"spec_change_required"}, "finding_spec_change_required"),
		requirement("change_impact_record", []string{"Orchestrator"}, []string{"recorded"}),
	},
	"GATE-FINDING-REQ-CHANGE-REQUIRED": {
		requestedRequirement("bug_batch_record", []string{"Orchestrator"}, []string{"req_change_required"}, "finding_req_change_required"),
		requirement("pause_record", []string{"Orchestrator"}, []string{"recorded"}),
	},
	"GATE-REPAIR-BATCH-REPORTED": {
		requirement("repair_record", []string{"Builder", "BUILD-WORK-PACKAGE"}, []string{"reported"}),
		requirement("change_impact_record", []string{"Builder", "BUILD-WORK-PACKAGE"}, []string{"recorded"}),
	},
	"GATE-TARGETED-REVERIFICATION-PASS": {
		currentPlanRequirement("targeted_reverification_record", []string{"Original Finder"}, []string{"pass"}),
	},
	"GATE-TARGETED-REVERIFICATION-FAIL": {
		currentPlanRequirement("targeted_reverification_record", []string{"Original Finder"}, []string{"fail"}),
	},
}

func requirement(kind string, responsibilities, conclusions []string) EvidenceRequirement {
	return EvidenceRequirement{
		Kind:             kind,
		Responsibilities: responsibilities,
		Conclusions:      conclusions,
		MinCount:         1,
	}
}

func currentPlanRequirement(kind string, responsibilities, conclusions []string) EvidenceRequirement {
	result := requirement(kind, responsibilities, conclusions)
	result.CurrentReviewPlan = true
	return result
}

func requestedRequirement(kind string, responsibilities, conclusions []string, event string) EvidenceRequirement {
	result := requirement(kind, responsibilities, conclusions)
	result.RequestedEvent = event
	return result
}

// currentPlanRoutingRequirement builds a routing-verdict requirement: the
// gate fires only when the rare verdict appears; ordinary results of the
// same kind are silently skipped.
func currentPlanRoutingRequirement(kind string, responsibilities, conclusions []string) EvidenceRequirement {
	result := currentPlanRequirement(kind, responsibilities, conclusions)
	result.RoutingVerdict = true
	return result
}

func currentPlanRequestedRequirement(kind string, responsibilities, conclusions []string, event string) EvidenceRequirement {
	result := requestedRequirement(kind, responsibilities, conclusions, event)
	result.CurrentReviewPlan = true
	return result
}

func cloneRequirements(source []EvidenceRequirement) []EvidenceRequirement {
	result := make([]EvidenceRequirement, len(source))
	for index, requirement := range source {
		result[index] = requirement
		result[index].Responsibilities = append([]string(nil), requirement.Responsibilities...)
		result[index].Conclusions = append([]string(nil), requirement.Conclusions...)
	}
	return result
}

// ProducerRequirements returns a defensive copy of the gate's producer
// contract for actionable diagnostics. It does not authorize a producer.
func ProducerRequirements(gateID string) []EvidenceRequirement {
	return cloneRequirements(semanticRequirements[gateID])
}
