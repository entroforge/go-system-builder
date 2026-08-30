package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type planHashInput struct {
	SchemaVersion string           `json:"schema_version"`
	REQ           REQBinding       `json:"req"`
	Inputs        []InventoryInput `json:"inputs"`
	BaseMode      string           `json:"base_mode"`
	TargetCursor  string           `json:"target_cursor"`
	Confidence    string           `json:"confidence"`
}

// BuildPlan creates a deterministic conservative plan from a validated
// inventory. The plan contains only repository-relative paths and hashes, so
// identical repository content produces the same plan hash at any root.
func BuildPlan(inventory Inventory) (Plan, error) {
	return BuildPlanForCursor(inventory, PlanSeedCursor, PlanConfidenceConservative)
}

// BuildPlanForCursor fingerprints a cursor reached by production Controller
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
	if confidence != PlanConfidenceConservative && confidence != PlanConfidenceFormalReplay {
		return Plan{}, &ValidationError{Code: ErrInvalidInventory, Field: "confidence", Reason: "recovery confidence is invalid"}
	}
	inputs := append([]InventoryInput(nil), inventory.Inputs...)
	sort.Slice(inputs, func(i, j int) bool {
		return inputs[i].Path < inputs[j].Path
	})
	plan := Plan{
		SchemaVersion: SchemaVersion,
		REQ:           inventory.REQ,
		Inputs:        inputs,
		BaseMode:      PlanBaseConservativeSeed,
		TargetCursor:  targetCursor,
		Confidence:    confidence,
	}
	if confidence == PlanConfidenceFormalReplay {
		plan.BaseMode = PlanBaseArtifactReconstruction
	}
	payload, err := json.Marshal(planHashInput{
		SchemaVersion: plan.SchemaVersion,
		REQ:           plan.REQ,
		Inputs:        plan.Inputs,
		BaseMode:      plan.BaseMode,
		TargetCursor:  plan.TargetCursor,
		Confidence:    plan.Confidence,
	})
	if err != nil {
		return Plan{}, fmt.Errorf("marshal recovery plan hash input: %w", err)
	}
	sum := sha256.Sum256(payload)
	plan.PlanSHA256 = hex.EncodeToString(sum[:])
	return plan, nil
}

func validateInventory(inventory Inventory) error {
	if inventory.SchemaVersion != "" && inventory.SchemaVersion != SchemaVersion {
		return &ValidationError{Code: ErrInvalidInventory, Field: "schema_version", Reason: "unsupported inventory schema version"}
	}
	if inventory.REQ.ID == "" || inventory.REQ.Path == "" || inventory.REQ.Status == "" || inventory.REQ.Version == "" || inventory.REQ.SHA256 == "" {
		return &ValidationError{Code: ErrInvalidInventory, Field: "req", Reason: "req binding is incomplete"}
	}
	if !strings.EqualFold(inventory.REQ.Status, "locked") {
		return &ValidationError{Code: ErrInvalidInventory, Field: "req.status", Reason: "req status must be locked"}
	}
	if err := validateRelativePath(inventory.REQ.Path); err != nil {
		return &ValidationError{Code: ErrInvalidInventory, Field: "req.path", Reason: "req path must be repository-relative", Cause: err}
	}
	if err := validateSHA256(inventory.REQ.SHA256); err != nil {
		return &ValidationError{Code: ErrInvalidInventory, Field: "req.sha256", Reason: "req sha256 is invalid", Cause: err}
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
		if err := validateSHA256(input.SHA256); err != nil {
			return &ValidationError{Code: ErrInvalidInventory, Field: "inputs.sha256", Path: input.Path, Reason: "input sha256 is invalid", Cause: err}
		}
		if _, exists := seen[input.Path]; exists {
			return &ValidationError{Code: ErrInvalidInventory, Field: "inputs.path", Path: input.Path, Reason: "input path is duplicated"}
		}
		seen[input.Path] = struct{}{}
		if input.Path == inventory.REQ.Path {
			reqFound = true
			if input.SHA256 != inventory.REQ.SHA256 {
				return &ValidationError{Code: ErrInvalidInventory, Field: "req.sha256", Path: input.Path, Reason: "req binding sha256 does not match inventory input"}
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

func validateSHA256(value string) error {
	if len(value) != sha256.Size*2 {
		return fmt.Errorf("sha256 must contain %d hexadecimal characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("sha256 is not hexadecimal: %w", err)
	}
	return nil
}
