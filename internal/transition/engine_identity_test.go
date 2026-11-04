package transition

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The committed transition identity names the responsibility and the intent,
// never the Runtime write count (G18 §1.1, §1.3).
//
// A transition legitimately happens more than once inside one responsibility:
// a planning rework re-enters the very same exit the first pass left. The
// intent therefore also carries the responsibility's own occurrence anchor —
// the transition that last committed inside this responsibility. The rework
// follows its own trigger, so the two occurrences are different domain things
// and keep different identities, while a replay of the same request sees the
// same committed history and keeps one identity.
func TestMutationEventIdentityNamesTheOccurrenceAnchor(t *testing.T) {
	from := map[string]any{"state": "planning", "phase": "design"}
	// The first design exit follows the bind: the responsibility has not
	// committed a transition yet.
	first, firstKey := mutationEventIdentity("PTR-PLAN-01", "loop-req", 1, from, 1, occurrenceAnchorNone)
	// The rework follows the repair trigger that sent the responsibility back
	// to planning.design.
	rework, reworkKey := mutationEventIdentity("PTR-PLAN-01", "loop-req", 1, from, 1, "TR-004")
	if first == rework || firstKey == reworkKey {
		t.Fatalf("a rework collided with the first pass: %q / %q vs %q / %q", first, firstKey, rework, reworkKey)
	}
	// The same intent retried produces the same event id and the same
	// idempotency key, so the journal deduplicates the retry (G18 §1.3).
	retry, retryKey := mutationEventIdentity("PTR-PLAN-01", "loop-req", 1, from, 1, "TR-004")
	if rework != retry || reworkKey != retryKey {
		t.Fatalf("the same intent must keep one identity: %q / %q vs %q / %q", rework, reworkKey, retry, retryKey)
	}
	// Two responsibilities never share an idempotency key.
	_, otherKey := mutationEventIdentity("PTR-PLAN-01", "loop-other", 1, from, 1, "TR-004")
	if otherKey == reworkKey {
		t.Fatal("two responsibilities share one idempotency key")
	}
}

// The identity of one intent does not depend on the Runtime write count. Two
// Runtime states that differ only in their revision produce the same identity,
// so the same intent always keeps one identity and the journal still
// deduplicates its replay (G18 §1.3).
func TestTransitionIdentityIsStableAcrossRuntimeRevisions(t *testing.T) {
	from := map[string]any{"state": "planning", "phase": "design"}
	first := map[string]any{
		"revision":        float64(0),
		"last_transition": map[string]any{"transition_id": "TR-001"},
	}
	later := map[string]any{
		"revision":        float64(7),
		"last_transition": map[string]any{"transition_id": "TR-001"},
	}
	firstID, firstKey := mutationEventIdentity("PTR-PLAN-01", "loop-req", 1, from, 1, responsibilityOccurrenceAnchor(first))
	laterID, laterKey := mutationEventIdentity("PTR-PLAN-01", "loop-req", 1, from, 1, responsibilityOccurrenceAnchor(later))
	if firstID != laterID || firstKey != laterKey {
		t.Fatalf("the identity changed with the Runtime revision: %q / %q vs %q / %q", firstID, firstKey, laterID, laterKey)
	}
	// The write count is not an input of the identity at all: it is the
	// retired revision-derived shape that must not come back.
	legacy := regexp.MustCompile(`-r\d+$`)
	if legacy.MatchString(firstID) || legacy.MatchString(firstKey) {
		t.Fatalf("the identity still carries a Runtime write count: %q / %q", firstID, firstKey)
	}
}

// The occurrence anchor names the transition that last committed inside the
// responsibility. It is a domain fact about the responsibility's own committed
// history — never a Runtime write count, a digest or a wall-clock value.
func TestResponsibilityOccurrenceAnchorNamesTheCommittedTransition(t *testing.T) {
	cases := []struct {
		name  string
		state map[string]any
		want  string
	}{
		{name: "no committed transition", state: map[string]any{}, want: occurrenceAnchorNone},
		{name: "nil last transition", state: map[string]any{"last_transition": nil}, want: occurrenceAnchorNone},
		{name: "committed transition", state: map[string]any{"last_transition": map[string]any{"transition_id": "TR-004"}}, want: "TR-004"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := responsibilityOccurrenceAnchor(test.state); got != test.want {
				t.Fatalf("occurrence anchor = %q, want %q", got, test.want)
			}
		})
	}
	// The anchor is never a number: a bare count would be a write count in
	// disguise.
	anchor := responsibilityOccurrenceAnchor(map[string]any{"last_transition": map[string]any{"transition_id": "TR-004"}})
	if _, err := strconv.Atoi(anchor); err == nil {
		t.Fatalf("the occurrence anchor is a bare number: %q", anchor)
	}
}

// The canonical intent names the baseline generation, the cursor the
// transition moves from, the lifecycle stage entry and the occurrence anchor —
// and nothing else. Only the generation and the stage entry are numbers, and
// both are domain coordinates of this occurrence.
func TestMutationEventIdentityIntentCarriesOnlyDomainCoordinates(t *testing.T) {
	from := map[string]any{"state": "planning", "phase": "design"}
	eventID, key := mutationEventIdentity("PTR-PLAN-01", "loop-req", 1, from, 1, "TR-004")
	prefix := "evt-ptr-plan-01-"
	if !strings.HasPrefix(eventID, prefix) {
		t.Fatalf("the event id does not carry the canonical domain prefix: %q", eventID)
	}
	intent := strings.TrimPrefix(eventID, prefix)
	parts := decodeIntentParts(t, intent)
	if len(parts) != 5 {
		t.Fatalf("the intent does not carry the five canonical coordinates: %+v", parts)
	}
	if parts[0] != "1" || parts[1] != "planning" || parts[2] != "design" || parts[3] != "1" || parts[4] != "TR-004" {
		t.Fatalf("the intent does not name the domain coordinates: %+v", parts)
	}
	numbers := 0
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err == nil {
			numbers++
		}
	}
	if numbers != 2 {
		t.Fatalf("the intent carries numeric segments beyond the generation and the stage entry: %+v", parts)
	}
	// The idempotency key names the responsibility coordinate and the same
	// intent.
	if !strings.HasPrefix(key, "runtime:ptr-plan-01:") {
		t.Fatalf("the idempotency key does not carry the canonical prefix: %q", key)
	}
	if !strings.HasSuffix(key, ":"+intent) {
		t.Fatalf("the idempotency key does not carry the event id's intent: %q", key)
	}
}

// decodeIntentParts decodes the production "<len>:<part>" encoding.
func decodeIntentParts(t *testing.T, encoded string) []string {
	t.Helper()
	parts := []string{}
	for encoded != "" {
		colon := strings.Index(encoded, ":")
		if colon < 1 {
			t.Fatalf("the intent is not length-prefixed: %q", encoded)
		}
		length, err := strconv.Atoi(encoded[:colon])
		if err != nil || length < 0 {
			t.Fatalf("the intent has an invalid length prefix: %q", encoded)
		}
		encoded = encoded[colon+1:]
		if len(encoded) < length {
			t.Fatalf("the intent is truncated: %q", encoded)
		}
		parts = append(parts, encoded[:length])
		encoded = encoded[length:]
	}
	return parts
}
