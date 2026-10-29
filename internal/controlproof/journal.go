// Package controlproof contains dependency-neutral readers for the original
// Runtime journal and the S6 source receipts that it commits. It has no Writer
// or repair API; Runtime and downstream proof readers can share these exact
// validators without depending on one another.
package controlproof

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// JournalLimit is the original complete-journal read budget used by Runtime.
const JournalLimit = 64 << 20

// JournalInspection is the validated projection of one complete journal or
// journal segment. Its fields intentionally match Runtime's original private
// journalInspection DTO.
type JournalInspection struct {
	Events       []map[string]any
	EventIndex   map[string]int
	RuntimeID    string
	TailSequence int
}

// ValidateJournalEvent performs the original embedded-schema validation.
func ValidateJournalEvent(event map[string]any) error {
	return validateJournalEventWith(schema.NewEmbeddedValidator(), event)
}

func validateJournalEventWith(validator *schema.Validator, event map[string]any) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode journal event: %w", err)
	}
	if err := validator.ValidateBytes("loop-event.schema.json", encoded); err != nil {
		return fmt.Errorf("journal event schema: %w", err)
	}
	return nil
}

// InspectJournalData validates a complete journal whose first sequence is 1.
func InspectJournalData(data []byte) (JournalInspection, error) {
	return InspectJournalSegmentDataFrom(data, 1)
}

// InspectJournalSegmentData validates one active/archive segment while
// allowing its first sequence to be greater than one.
func InspectJournalSegmentData(data []byte) (JournalInspection, error) {
	return InspectJournalSegmentDataFrom(data, 0)
}

// InspectJournalSegmentDataFrom is the original Runtime journal decoder and
// validator. Keep its Scanner limits, embedded schema check, sequence rules,
// and identity checks aligned with the Writer's established contract.
func InspectJournalSegmentDataFrom(data []byte, firstSequence int) (JournalInspection, error) {
	return InspectJournalSegmentDataFromContext(context.Background(), data, firstSequence)
}

func inspectJournalSegmentDataFromContext(ctx context.Context, data []byte, firstSequence int) (JournalInspection, error) {
	if err := ctx.Err(); err != nil {
		return JournalInspection{}, err
	}
	inspection := JournalInspection{
		Events:     make([]map[string]any, 0),
		EventIndex: make(map[string]int),
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	// The schema is immutable embedded code; reuse only its compilation for
	// this inspection, while validating every event and original cursor rule.
	validator := schema.NewEmbeddedValidator()
	expectedSequence := firstSequence
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return JournalInspection{}, err
		}
		line := scanner.Bytes()
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			return JournalInspection{}, fmt.Errorf("decode journal event: %w", err)
		}
		if err := validateJournalEventWith(validator, event); err != nil {
			return JournalInspection{}, err
		}
		sequence, err := integerField(event, "sequence")
		if expectedSequence == 0 {
			expectedSequence = sequence
		}
		if err != nil || sequence != expectedSequence {
			return JournalInspection{}, fmt.Errorf("journal sequence %d is not contiguous; expected %d", sequence, expectedSequence)
		}
		runtimeID, _ := event["runtime_id"].(string)
		if runtimeID == "" {
			return JournalInspection{}, errors.New("journal runtime_id is required")
		}
		if inspection.RuntimeID == "" {
			inspection.RuntimeID = runtimeID
		} else if runtimeID != inspection.RuntimeID {
			return JournalInspection{}, fmt.Errorf("journal runtime_id %q does not match journal runtime_id %q", runtimeID, inspection.RuntimeID)
		}
		eventID, _ := event["event_id"].(string)
		if eventID == "" {
			return JournalInspection{}, errors.New("journal event_id is required")
		}
		if _, exists := inspection.EventIndex[eventID]; exists {
			return JournalInspection{}, fmt.Errorf("journal event_id %q is duplicated", eventID)
		}
		inspection.EventIndex[eventID] = len(inspection.Events)
		inspection.Events = append(inspection.Events, event)
		inspection.TailSequence = sequence
		expectedSequence++
	}
	if err := scanner.Err(); err != nil {
		return JournalInspection{}, fmt.Errorf("scan journal: %w", err)
	}
	return inspection, nil
}

// ValidateStateJournalPair verifies a Runtime identity and its journal cursor
// against a validated journal inspection.
func ValidateStateJournalPair(state map[string]any, inspection JournalInspection) error {
	runtimeID, _ := state["runtime_id"].(string)
	if strings.TrimSpace(runtimeID) == "" {
		return errors.New("state runtime_id is required")
	}
	if inspection.RuntimeID != "" && inspection.RuntimeID != runtimeID {
		return fmt.Errorf("journal runtime_id %q does not match state runtime_id %q", inspection.RuntimeID, runtimeID)
	}
	stateJournal, err := objectField(state, "journal")
	if err != nil {
		return fmt.Errorf("state journal: %w", err)
	}
	return ValidateStateJournalCursor(stateJournal, inspection)
}

// ValidateStateJournalCursor matches Runtime's stored last sequence and event
// against the validated journal tail.
func ValidateStateJournalCursor(stateJournal map[string]any, inspection JournalInspection) error {
	stateSequence, err := integerField(stateJournal, "last_sequence")
	if err != nil {
		return fmt.Errorf("state last_sequence: %w", err)
	}
	if inspection.TailSequence != stateSequence {
		return fmt.Errorf("existing runtime journal tail sequence %d does not match state last_sequence %d", inspection.TailSequence, stateSequence)
	}
	stateEventID := stateJournal["last_event_id"]
	if inspection.TailSequence == 0 {
		if stateEventID == nil {
			return nil
		}
		if eventID, ok := stateEventID.(string); ok && strings.TrimSpace(eventID) == "" {
			return errors.New("state journal last_event_id must be null when the journal is empty")
		}
		return fmt.Errorf("state journal last_event_id %v must be null when the journal is empty", stateEventID)
	}
	if len(inspection.Events) == 0 {
		return errors.New("non-empty state journal cursor has no journal tail")
	}
	tailEventID, _ := inspection.Events[len(inspection.Events)-1]["event_id"].(string)
	if tailEventID == "" {
		return errors.New("journal tail event_id is required")
	}
	stateEventIDString, ok := stateEventID.(string)
	if !ok || stateEventIDString != tailEventID {
		return fmt.Errorf("state journal last_event_id %q does not match journal tail event_id %q", stateEventIDString, tailEventID)
	}
	return nil
}

// JournalSegmentPaths returns sorted archive segment paths whose archive
// sequence is a valid integer. Malformed archive names remain excluded.
func JournalSegmentPaths(path string) ([]string, error) {
	pattern := path + ".archive.*.jsonl"
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob journal archives: %w", err)
	}
	filtered := make([]string, 0, len(matches))
	for _, match := range matches {
		if _, ok := ParseArchiveSeq(match); !ok {
			continue
		}
		filtered = append(filtered, match)
	}
	sort.Slice(filtered, func(i, j int) bool {
		left, _ := ParseArchiveSeq(filtered[i])
		right, _ := ParseArchiveSeq(filtered[j])
		return left < right
	})
	return filtered, nil
}

// ParseArchiveSeq extracts the rotation sequence from an archive path of the
// form `<journal>.archive.<sequence>.jsonl`.
func ParseArchiveSeq(path string) (int, bool) {
	idx := strings.LastIndex(path, ".archive.")
	if idx < 0 {
		return 0, false
	}
	rest := path[idx+len(".archive."):]
	rest = strings.TrimSuffix(rest, ".jsonl")
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ExtractArchiveSeq preserves Runtime's sorting sentinel for malformed names.
func ExtractArchiveSeq(path string) int {
	sequence, ok := ParseArchiveSeq(path)
	if !ok {
		return math.MaxInt
	}
	return sequence
}

func integerField(object map[string]any, key string) (int, error) {
	switch value := object[key].(type) {
	case float64:
		return int(value), nil
	case int:
		return value, nil
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}

func objectField(object map[string]any, key string) (map[string]any, error) {
	value, ok := object[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return value, nil
}

func stringValue(value any) string {
	if value, ok := value.(string); ok {
		return value
	}
	return ""
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
