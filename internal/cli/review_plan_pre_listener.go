package cli

import (
	"fmt"
	"os"

	"github.com/entroforge/go-system-builder/internal/policy"
)

// s7ReviewPlanObservation is a result for the original event renderer. It is
// not a second receipt or a request to suppress the rest of the Pre event.
type s7ReviewPlanObservation struct {
	Context    string
	Attempted  bool
	Changed    bool
	ReplaySame bool
}

// observeMainS7ReviewPlan listens to the actual original Main questionnaire.
// A natural observation is not a synthetic save: the original plan consumer
// owns first reception, replay and changes to an accepted plan.
func observeMainS7ReviewPlan(root string, request policy.Input) s7ReviewPlanObservation {
	if request.Event != "PreToolUse" || !mainQuestionnaireViewer(request) {
		return s7ReviewPlanObservation{}
	}
	reader, ok := contextForHook(request).Value(hookReaderKey{}).(*hookReader)
	if !ok || reader.Kind != "Main" || reader.snapshotErr != nil || reader.snapshot.State == nil {
		// The native route owns unavailable Runtime feedback. Never infer S7
		// from an unknown reader or perform another read on every ordinary Pre.
		return s7ReviewPlanObservation{}
	}
	snapshot := reader.snapshot
	if mapFieldCLI(snapshot.State, "lifecycle")["state"] != "verification" {
		return s7ReviewPlanObservation{}
	}
	path := reviewPlanQuestionnairePath(root, snapshot.State)
	if path == "" {
		return s7ReviewPlanObservation{}
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s7ReviewPlanObservation{} // Original preparer owns a missing draft.
	}
	if err != nil {
		return s7ReviewPlanObservation{Context: fmt.Sprintf("S7 ReviewPlan observation could not read %s: %v. Preserve the original answer; Main/system owns this source failure.", path, err)}
	}
	if err := contextForHook(request).Err(); err != nil {
		return s7ReviewPlanObservation{Context: fmt.Sprintf("S7 ReviewPlan observation was interrupted at %s: %v. Preserve the original answer and any accepted facts.", path, err)}
	}
	result := receiveReviewPlanQuestionnaire(root, request, snapshot, path, data)
	if result.ReplaySame {
		// The current stage guidance already projects this accepted fact. Avoid
		// repeating a receipt or refreshing state when no Writer was invoked.
		return s7ReviewPlanObservation{}
	}
	return result
}
