package repair

import "fmt"
import "context"
import "github.com/entroforge/go-system-builder/internal/targetedauthoring"

const SharedTargetedAuthoringProtocol = targetedauthoring.SharedTargetedAuthoringProtocol

type TargetedAuthoring = targetedauthoring.TargetedAuthoring

func NewTargetedAuthoring(state map[string]any, original, performing, owner string) TargetedAuthoring {
	return targetedauthoring.FromState(state, original, performing, owner)
}

func UsesSharedTargetedAuthoring(root string, state map[string]any, original, performing, owner string, contexts ...context.Context) (bool, error) {
	if len(contexts) > 0 && contexts[0] != nil {
		if err := contexts[0].Err(); err != nil {
			return false, err
		}
	}
	if performing != VerificationAssignmentID(original) {
		return false, nil
	}
	owners := stringMapField(repairPointer(state)["assignment_owners"])
	if owners[original] == "" || owners[performing] != owner {
		return false, fmt.Errorf("Targeted original owner differs")
	}
	return targetedauthoring.Verify(root, state, NewTargetedAuthoring(state, original, performing, owner), ManifestVerificationAssignmentID(original), contexts...)
}

func TargetedSharedAnswerPath(root, legacy string) (string, error) {
	return targetedauthoring.TargetedSharedAnswerPath(root, legacy)
}
