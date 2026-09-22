package repair

import "fmt"

// Missing receipts are not proof that no work was done. Preserve the original
// baseline whenever results, dispatched owners or implementation changes exist.
func validateEmptySessionReplacement(root string, pointer map[string]any) error {
	for _, key := range []string{"result_refs", "plan_report_refs"} {
		if len(existingArtifactRefs(pointer[key])) > 0 {
			return fmt.Errorf("cannot replace RepairSession with recorded %s; resume the existing session", key)
		}
	}
	if len(stringMapField(pointer["assignment_owners"])) > 0 {
		return fmt.Errorf("cannot replace RepairSession with dispatched assignments; resume the existing session")
	}
	ref, err := pointerArtifact(pointer, "path", "sha256", "current RepairSession")
	if err != nil {
		return err
	}
	session, err := ValidateRepairSession(root, ref)
	if err != nil {
		return err
	}
	changes, err := ComputeSessionChangeset(root, session)
	if err != nil {
		return err
	}
	if len(changes) > 0 {
		return fmt.Errorf("cannot replace RepairSession: %d implementation changes already exist (first: %s); preserve its original baseline and resume", len(changes), changes[0].Path)
	}
	return nil
}
