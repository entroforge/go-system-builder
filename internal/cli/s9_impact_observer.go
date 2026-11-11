package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// observeMainS9Impact listens to the Main's original Impact under its existing
// Session on Pre and Stop. Post uses its existing event dispatch and the same
// byte consumer, so an unrelated Post is not swallowed. The same bytes go to
// the original consumer; a
// replay does not redo the handoff, and no event is turned into a fake Write.
func observeMainS9Impact(root string, request policy.Input, out io.Writer) (bool, int) {
	if request.Event != "PreToolUse" && request.Event != "Stop" {
		return false, 0
	}
	reader, ok := contextForHook(request).Value(hookReaderKey{}).(*hookReader)
	if !ok || reader.Kind != "Main" || reader.snapshotErr != nil || reader.snapshot.State == nil {
		return false, 0
	}
	snapshot := reader.snapshot
	if stringValue(repairPointerCLI(snapshot.State)["session_id"]) == "" {
		return false, 0
	}
	path, err := repairLeadQuestionnairePath(root, snapshot.State, "change-impact.json")
	if err != nil {
		return false, 0
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false, 0
	}
	data, err := workpacket.ReadFile(root, relative, 20<<20)
	if os.IsNotExist(err) {
		return false, 0
	}
	if err != nil {
		return true, renderS9ImpactObservation(out, request.Event, fmt.Sprintf("S9 original ChangeImpact is unavailable at %s: %v. Main/system owns this source issue; already received work remains preserved.", path, err))
	}
	if !json.Valid(data) {
		return false, 0 // A partial natural write remains an editable draft.
	}
	unchanged, err := s9ObservedQuestionnaireUnchangedContext(contextForHook(request), root, snapshot.State, observedQuestionnaire{path: path, s9Kind: "impact"}, data)
	if err != nil {
		return true, renderS9ImpactObservation(out, request.Event, fmt.Sprintf("S9 original ChangeImpact observation could not continue at %s: %v. Preserve the original answer and prior accepted work.", path, err))
	}
	if unchanged {
		return false, 0
	}
	var feedback bytes.Buffer
	_, _ = saveRepairImpactQuestionnaireBytes(root, snapshot, path, data, &feedback, contextForHook(request))
	var envelope struct {
		Output struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if json.Unmarshal(feedback.Bytes(), &envelope) != nil || envelope.Output.Context == "" {
		return true, renderS9ImpactObservation(out, request.Event, "S9 original ChangeImpact was observed; preserve its original answer and any accepted facts.")
	}
	return true, renderS9ImpactObservation(out, request.Event, envelope.Output.Context)
}

func renderS9ImpactObservation(out io.Writer, event, message string) int {
	if event == "Stop" {
		_ = json.NewEncoder(out).Encode(map[string]any{"systemMessage": message})
	} else {
		_ = json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": event, "additionalContext": message}})
	}
	return 0
}
