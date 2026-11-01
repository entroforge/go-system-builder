package recovery

import (
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// BuildPlan creates a conservative plan using the original recovery inputs.
func BuildPlan(inventory Inventory) (Plan, error) {
	return BuildPlanForCursor(inventory, PlanSeedCursor, PlanConfidenceConservative)
}

// BuildPlanForCursor records a cursor reached by production Controller
// replay. Callers must not use it for file-existence inference; the recovery
// package cannot independently prove a transition result.
func BuildPlanForCursor(inventory Inventory, targetCursor, confidence string) (Plan, error) {
	if err := validateInventory(inventory); err != nil {
		return Plan{}, err
	}
	targetCursor = strings.TrimSpace(targetCursor)
	if targetCursor == "" || strings.ContainsAny(targetCursor, " \t\r\n/\\") {
		return Plan{}, &ValidationError{Code: ErrInvalidInventory, Field: "target_cursor", Reason: "target cursor is invalid"}
	}
	if confidence != PlanConfidenceConservative && confidence != PlanConfidenceFormalReplay && confidence != PlanConfidenceOriginalPair {
		return Plan{}, &ValidationError{Code: ErrInvalidInventory, Field: "confidence", Reason: "recovery confidence is invalid"}
	}
	inputs := append([]InventoryInput(nil), inventory.Inputs...)
	req := inventory.REQ
	sort.Slice(inputs, func(i, j int) bool {
		return inputs[i].Path < inputs[j].Path
	})
	plan := Plan{
		SchemaVersion: SchemaVersion,
		REQ:           req,
		Inputs:        inputs,
		BaseMode:      PlanBaseConservativeSeed,
		TargetCursor:  targetCursor,
		Confidence:    confidence,
	}
	if confidence == PlanConfidenceFormalReplay {
		plan.BaseMode = PlanBaseArtifactReconstruction
	}
	if confidence == PlanConfidenceOriginalPair {
		plan.BaseMode = PlanBaseCheckpointRestoration
	}
	return plan, nil
}

func validateInventory(inventory Inventory) error {
	if inventory.SchemaVersion != "" && inventory.SchemaVersion != SchemaVersion {
		return &ValidationError{Code: ErrInvalidInventory, Field: "schema_version", Reason: "unsupported inventory schema version"}
	}
	if inventory.REQ.ID == "" || inventory.REQ.Path == "" || inventory.REQ.Status == "" || inventory.REQ.Version == "" {
		return &ValidationError{Code: ErrInvalidInventory, Field: "req", Reason: "req binding is incomplete"}
	}
	if !strings.EqualFold(inventory.REQ.Status, "locked") {
		return &ValidationError{Code: ErrInvalidInventory, Field: "req.status", Reason: "req status must be locked"}
	}
	if err := validateRelativePath(inventory.REQ.Path); err != nil {
		return &ValidationError{Code: ErrInvalidInventory, Field: "req.path", Reason: "req path must be repository-relative", Cause: err}
	}
	seen := make(map[string]struct{}, len(inventory.Inputs))
	reqFound := false
	for _, input := range inventory.Inputs {
		if err := validateRelativePath(input.Path); err != nil {
			return &ValidationError{Code: ErrInvalidInventory, Field: "inputs.path", Path: input.Path, Reason: "input path must be repository-relative", Cause: err}
		}
		if input.Kind == "" {
			return &ValidationError{Code: ErrInvalidInventory, Field: "inputs.kind", Path: input.Path, Reason: "input kind is required"}
		}
		if _, exists := seen[input.Path]; exists {
			return &ValidationError{Code: ErrInvalidInventory, Field: "inputs.path", Path: input.Path, Reason: "input path is duplicated"}
		}
		seen[input.Path] = struct{}{}
		if input.Path == inventory.REQ.Path {
			reqFound = true
			if input.Kind != InputKindREQ {
				return &ValidationError{Code: ErrInvalidInventory, Field: "inputs.kind", Path: input.Path, Reason: "selected req inventory input has the wrong kind"}
			}

		}
	}
	if !reqFound {
		return &ValidationError{Code: ErrInvalidInventory, Field: "inputs", Reason: "selected req is missing from inventory"}
	}
	return nil
}

func validateRelativePath(value string) error {
	if value == "" || filepath.IsAbs(filepath.FromSlash(value)) || path.IsAbs(value) || strings.Contains(value, "\\") {
		return ErrPathOutsideRepository
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ErrPathOutsideRepository
	}
	return nil
}
