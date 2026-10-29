package controlproof

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/workstate"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/productbaseline"
)

// ReadJournalForState reads all original rotation segments and the active
// journal, then proves the complete cursor matches the supplied state. It
// deliberately does not acquire a Runtime lock; mutation callers invoke it
// only while their existing Writer owns that lock.
func ReadJournalForState(root string, state map[string]any) ([]byte, error) {
	return ReadJournalForStateContext(context.Background(), root, state)
}

func ReadJournalForStateContext(ctx context.Context, root string, state map[string]any) ([]byte, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("journal read requires the Runtime authority root")
	}
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	return ReadJournalForStateAtContext(ctx, journalPath, state)
}

// ReadJournalForStateAt uses the same complete-journal proof at an explicit
// Store coordinate. Lifecycle writers already hold their original state lock.
func ReadJournalForStateAt(journalPath string, state map[string]any) ([]byte, error) {
	return ReadJournalForStateAtContext(context.Background(), journalPath, state)
}

func ReadJournalForStateAtContext(ctx context.Context, journalPath string, state map[string]any) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	segments, err := JournalSegmentPaths(journalPath)
	if err != nil {
		return nil, err
	}
	segments = append(segments, journalPath)
	var combined bytes.Buffer
	for _, segment := range segments {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(segment)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read Runtime journal segment %s: %w", filepath.Base(segment), err)
		}
		if combined.Len()+len(data)+1 > JournalLimit {
			return nil, fmt.Errorf("complete Runtime journal exceeds %d bytes", JournalLimit)
		}
		combined.Write(data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			combined.WriteByte('\n')
		}
	}
	inspection, err := InspectJournalSegmentDataFromContext(ctx, combined.Bytes(), 1)
	if err != nil {
		return nil, fmt.Errorf("inspect complete Runtime journal: %w", err)
	}
	if err := ValidateStateJournalPair(state, inspection); err != nil {
		return nil, fmt.Errorf("Runtime journal does not match locked state: %w", err)
	}
	return combined.Bytes(), nil
}

// HasCommittedTransitionForGeneration searches a fully validated Runtime
// journal for the original successful transition at one generation.
func HasCommittedTransitionForGeneration(state map[string]any, journalBytes []byte, transitionID string, generation int) (bool, error) {
	inspection, err := InspectJournalData(journalBytes)
	if err != nil {
		return false, err
	}
	if err := ValidateStateJournalPair(state, inspection); err != nil {
		return false, err
	}
	runtimeID := stringValue(state["runtime_id"])
	for _, event := range inspection.Events {
		eventGeneration, generationErr := integerField(event, "baseline_generation")
		if event["runtime_id"] == runtimeID && event["transition_id"] == transitionID && generationErr == nil && eventGeneration == generation && event["event"] == "transition_committed" && event["outcome"] == "committed" {
			return true, nil
		}
	}
	return false, nil
}

// ValidateS6MainStartReceipts verifies any persisted common Main baselines.
// Absence is allowed for old runtimes; canonical consumers should also call
// RequireCurrentS6MainStart.
func ValidateS6MainStartReceipts(state map[string]any, journalBytes []byte) error {
	return ValidateS6MainStartReceiptsContext(context.Background(), state, journalBytes)
}

func ValidateS6MainStartReceiptsContext(ctx context.Context, state map[string]any, journalBytes []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	starts, err := productbaseline.ReadS6MainStarts(state)
	if err != nil {
		return fmt.Errorf("S6 Main start records: %w", err)
	}
	if len(starts) == 0 {
		return nil
	}
	inspection, err := InspectJournalSegmentDataFromContext(ctx, journalBytes, 1)
	if err != nil {
		return fmt.Errorf("S6 Main start journal: %w", err)
	}
	if err := ValidateStateJournalPair(state, inspection); err != nil {
		return fmt.Errorf("S6 Main start journal cursor: %w", err)
	}
	runtimeID := stringValue(state["runtime_id"])
	baseline, _ := state["baseline"].(map[string]any)
	currentGeneration, err := integerField(baseline, "generation")
	if err != nil {
		return fmt.Errorf("S6 Main start baseline generation: %w", err)
	}
	seenEvents := map[string]bool{}
	for i, start := range starts {
		if start.RuntimeID != runtimeID || start.BaselineGeneration > currentGeneration {
			return fmt.Errorf("S6 Main start[%d] runtime/generation binding differs", i)
		}
		index, ok := inspection.EventIndex[start.TransitionEventID]
		if !ok {
			return fmt.Errorf("S6 Main start[%d] original TR-003 event is absent", i)
		}
		if seenEvents[start.TransitionEventID] {
			return fmt.Errorf("S6 Main start records reuse an original transition event")
		}
		seenEvents[start.TransitionEventID] = true
		event := inspection.Events[index]
		actor, _ := event["actor"].(map[string]any)
		mainActor := actor["type"] == "orchestrator" && actor["id"] == "orchestrator"
		roleActor := actor["type"] == "agent" && (actor["id"] == "document_verifier" || actor["id"] == "hook_controller")
		if !mainActor && !roleActor {
			return fmt.Errorf("S6 Main start[%d] original TR-003 actor differs", i)
		}
		eventGeneration, generationErr := integerField(event, "baseline_generation")
		if event["runtime_id"] != start.RuntimeID || event["transition_id"] != "TR-003" || event["event"] != "transition_committed" || event["outcome"] != "committed" || generationErr != nil || eventGeneration != start.BaselineGeneration {
			return fmt.Errorf("S6 Main start[%d] original transition identity differs", i)
		}
		after, afterErr := integerField(event, "after_revision")
		before, beforeErr := integerField(event, "before_revision")
		sequence, sequenceErr := integerField(event, "sequence")
		if afterErr != nil || beforeErr != nil || sequenceErr != nil || after != start.TransitionAfterRevision || before+1 != after || sequence != start.TransitionSequence {
			return fmt.Errorf("S6 Main start[%d] original transition cursor differs", i)
		}
		if !transitionCursorMatches(event["from"], "document_verification") || !transitionCursorMatches(event["to"], "building") {
			return fmt.Errorf("S6 Main start[%d] original TR-003 lifecycle cursor differs", i)
		}
		if !hasS6MainStartActionReceipt(event, start) {
			return fmt.Errorf("S6 Main start[%d] original action receipt is missing or differs", i)
		}
	}
	return nil
}

// RequireCurrentS6MainStart is the strict read boundary for canonical S7
// consumers. It never synthesizes a missing start from the current branch.
func RequireCurrentS6MainStart(state map[string]any, journalBytes []byte) (productbaseline.S6MainStart, error) {
	return RequireCurrentS6MainStartContext(context.Background(), state, journalBytes)
}

func RequireCurrentS6MainStartContext(ctx context.Context, state map[string]any, journalBytes []byte) (productbaseline.S6MainStart, error) {
	if err := ctx.Err(); err != nil {
		return productbaseline.S6MainStart{}, err
	}
	if !workstate.SupportsAssignments(state["schema_version"]) {
		return productbaseline.S6MainStart{}, errors.New("common S6 Main baseline requires Runtime schema 1.2.0/1.3.0")
	}
	if err := ValidateS6MainStartReceiptsContext(ctx, state, journalBytes); err != nil {
		return productbaseline.S6MainStart{}, err
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return productbaseline.S6MainStart{}, err
	}
	start, found, err := productbaseline.FindS6MainStart(state, generation)
	if err != nil {
		return productbaseline.S6MainStart{}, err
	}
	if !found {
		return productbaseline.S6MainStart{}, fmt.Errorf("current generation %d has no original TR-003 common Main baseline; historical start cannot be inferred", generation)
	}
	return start, nil
}

func hasS6MainStartActionReceipt(event map[string]any, start productbaseline.S6MainStart) bool {
	actions, _ := event["action_results"].([]any)
	expected := fmt.Sprintf("registered %d complete task(s); s6_main_start_sha256=%s", len(start.TaskSet), start.ReceiptSHA256)
	found := 0
	for _, raw := range actions {
		row, _ := raw.(map[string]any)
		if row == nil || row["id"] != "register_execution_batch" {
			continue
		}
		found++
		if row["result"] != "committed" || row["detail"] != expected {
			return false
		}
	}
	return found == 1
}

func transitionCursorMatches(raw any, stateName string) bool {
	data, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	var cursor map[string]any
	if json.Unmarshal(data, &cursor) != nil {
		return false
	}
	return cursor["state"] == stateName && cursor["phase"] == nil
}
