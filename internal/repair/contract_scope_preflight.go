package repair

import (
	"encoding/json"
	"fmt"
)

// ValidateContractUnitScopes applies the S9 scope rule before S8 approval.
// It does not infer which files the prose requires, nor change the contract.
func ValidateContractUnitScopes(data []byte) error {
	var contract struct {
		Units       []RepairUnit `json:"repair_units"`
		Prospective []string     `json:"prospective_scope"`
		Forbidden   []string     `json:"forbidden_scope"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		return err
	}
	for _, unit := range contract.Units {
		scope := unit.Scope
		if len(scope) == 0 {
			scope = contract.Prospective
		}
		for _, path := range scope {
			if err := scopeAllows(path, contract.Prospective, contract.Forbidden); err != nil {
				return fmt.Errorf("RepairContract unit %s scope: %w", unit.ID, err)
			}
		}
	}
	return nil
}

// ValidateContractExecutionMap catches mechanically knowable S9 compilation
// gaps before a delegated approval spends authority. This is not a semantic
// proof that every business path or necessary source file was listed.
func ValidateContractExecutionMap(data []byte) error {
	var c struct {
		Units   []RepairUnit `json:"repair_units"`
		Symptom []string     `json:"symptom_assertions"`
		Root    []string     `json:"root_invariant_assertions"`
		Gap     []string     `json:"detection_gap_assertions"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return err
	}
	known := map[string]bool{}
	covered := map[string]bool{}
	for prefix, values := range map[string][]string{"symptom": c.Symptom, "root": c.Root, "gap": c.Gap} {
		for i := range values {
			known[fmt.Sprintf("%s-%d", prefix, i+1)] = true
		}
	}
	units := map[string]RepairUnit{}
	for _, u := range c.Units {
		if u.ID == "" {
			return fmt.Errorf("repair unit id is missing")
		}
		if _, ok := units[u.ID]; ok {
			return fmt.Errorf("duplicate repair unit %s", u.ID)
		}
		units[u.ID] = u
		if len(u.AssertionIDs) == 0 {
			return fmt.Errorf("repair unit %s requires explicit assertion_ids before delegated approval", u.ID)
		}
		if len(u.AssertionIDs) == 1 && u.AssertionIDs[0] == "all" {
			for id := range known {
				covered[id] = true
			}
			continue
		}
		for _, id := range u.AssertionIDs {
			if !known[id] {
				return fmt.Errorf("repair unit %s references unknown assertion %s", u.ID, id)
			}
			covered[id] = true
		}
	}
	for id := range known {
		if !covered[id] {
			return fmt.Errorf("repair assertion %s has no owning unit", id)
		}
	}
	for _, u := range c.Units {
		for _, dep := range u.DependsOn {
			if _, ok := units[dep]; !ok {
				return fmt.Errorf("repair unit %s depends on unknown unit %s", u.ID, dep)
			}
		}
	}
	if cycle := repairPlanDependencyCycle(units); cycle != "" {
		return fmt.Errorf("repair dependency cycle at %s", cycle)
	}
	return nil
}
