package repair

import "fmt"

// A prior session without issued responsibilities can be superseded without
// authenticating an unavailable repository baseline. Existing files and the
// original session remain untouched; this does not assert that no work exists.
func validateEmptySessionReplacement(root string, pointer map[string]any) error {
	for _, key := range []string{"result_refs", "plan_report_refs"} {
		if len(existingArtifactRefs(pointer[key])) > 0 {
			return fmt.Errorf("cannot replace RepairSession with recorded %s; resume the existing session", key)
		}
	}
	if len(stringMapField(pointer["assignment_owners"])) > 0 {
		return fmt.Errorf("cannot replace RepairSession with dispatched assignments; resume the existing session")
	}
	ref, err := pointerArtifact(pointer, "path", "current RepairSession")
	if err != nil {
		return err
	}
	_, err = ValidateRepairSession(root, ref)
	return err
}
