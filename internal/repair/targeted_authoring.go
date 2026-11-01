package repair

import "fmt"
import "encoding/json"
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
	// The original owner and issued TASK establish the authoring protocol.
	fields, err := targetedauthoring.RegisteredTask(root, state, stringField(repairPointer(state)["session_id"]), performing, owner, contexts...)
	if err != nil {
		return false, err
	}
	if fields == nil {
		return false, nil
	}
	var session, assignment string
	_ = json.Unmarshal(fields["session_id"], &session)
	_ = json.Unmarshal(fields["assignment_id"], &assignment)
	if session != stringField(repairPointer(state)["session_id"]) || assignment != performing {
		return false, nil
	}
	var declaration TargetedAuthoring
	if raw, ok := fields["targeted_authoring"]; ok {
		if err := json.Unmarshal(raw, &declaration); err != nil {
			return false, err
		}
		return declaration.Protocol == SharedTargetedAuthoringProtocol, nil
	}

	return false, nil
}

func TargetedSharedAnswerPath(root, legacy string) (string, error) {
	return targetedauthoring.TargetedSharedAnswerPath(root, legacy)
}
