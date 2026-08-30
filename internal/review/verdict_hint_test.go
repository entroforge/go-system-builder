package review

// verdict_hint_test.go — regression tests for the verdict=fail guidance
// hook (L3-S7 §3.5). The fix wraps the schema-validator's "value must be
// one of …" rejection with an actionable pointer to the verdict=finding
// + findings[] pattern, so Reviewers hitting the gate know the right
// shape on the first failure rather than guessing between verdict
// variants.
//
// verdictHint is a pure helper (no FS, no state) so the tests exercise
// it directly with synthesized JSON bytes.

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestVerdictHintRejectsVerdictFail is the headline regression: a payload
// with verdict=fail must surface the verdict=finding + findings[]
// redirect before the schema validator buries the user in
// "value must be one of …".
func TestVerdictHintRejectsVerdictFail(t *testing.T) {
	body := []byte(`{
		"verdict": "fail",
		"assignment_id": "assignment-1",
		"result_id": "review-result-1"
	}`)
	hint, ok := verdictHint(body)
	if !ok {
		t.Fatal("expected verdictHint to fire on verdict=fail")
	}
	msg := hint.Error()
	for _, keyword := range []string{
		"verdict=\"fail\"",           // names the offending value
		"\"finding\"",                // redirect to the right value
		"findings[]",                 // names the required payload field
		"claim_results[].conclusion", // names where per-Claim failures live
		"pass",                       // enumerates valid values
		"finding",                    // enumerates valid values
		"req_change_required",        // enumerates valid values
		"release_blocked",            // enumerates valid values
		"L3-S7 §3.5",                 // anchors the spec section
	} {
		if !strings.Contains(msg, keyword) {
			t.Errorf("hint must contain %q, got:\n%s", keyword, msg)
		}
	}
}

// TestVerdictHintAllowsValidVerdicts guards the short-circuit: verdictHint
// must return ok=false for every value in the schema's enum, otherwise
// valid submissions would trip our early-detection path before reaching
// the real schema validator.
func TestVerdictHintAllowsValidVerdicts(t *testing.T) {
	for _, verdict := range []string{"pass", "finding", "req_change_required", "release_blocked"} {
		t.Run(verdict, func(t *testing.T) {
			body := []byte(`{"verdict":"` + verdict + `","result_id":"review-result-1"}`)
			hint, ok := verdictHint(body)
			if ok {
				t.Fatalf("verdict=%q must not trigger the hint, got %v", verdict, hint)
			}
		})
	}
}

// TestVerdictHintIgnoresMissingOrNonStringVerdict ensures the hint is
// purely advisory: payloads without a verdict field (or with a non-string
// verdict) must NOT short-circuit; the schema validator owns that gate.
func TestVerdictHintIgnoresMissingOrNonStringVerdict(t *testing.T) {
	cases := map[string][]byte{
		"missing verdict":     []byte(`{"result_id":"review-result-1"}`),
		"null verdict":        []byte(`{"verdict":null,"result_id":"review-result-1"}`),
		"numeric verdict":     []byte(`{"verdict":1,"result_id":"review-result-1"}`),
		"boolean verdict":     []byte(`{"verdict":true,"result_id":"review-result-1"}`),
		"unparseable payload": []byte(`{not json`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := verdictHint(body); ok {
				t.Fatalf("%s must not trigger the hint", name)
			}
		})
	}
}

// TestVerdictHintOnlyFiresOnStringFail guards against an unrelated
// downstream string (e.g. a future verdict named "fail_silent") being
// caught by accident. The gate is strict: only "fail" triggers.
func TestVerdictHintOnlyFiresOnStringFail(t *testing.T) {
	cases := map[string][]byte{
		"verdict=failure": []byte(`{"verdict":"failure"}`),
		"verdict=FAIL":    []byte(`{"verdict":"FAIL"}`),
		"verdict=passed":  []byte(`{"verdict":"passed"}`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := verdictHint(body); ok {
				t.Fatalf("%s must not trigger the hint", name)
			}
		})
	}
}

// TestVerdictHintMarshalledRoundTrip documents that verdictHint only
// inspects JSON-decodable payloads. A JSON number-style verdict (which
// is also rejected by the schema) falls through to the schema validator
// rather than tripping our hint, keeping the responsibilities split.
func TestVerdictHintMarshalledRoundTrip(t *testing.T) {
	body, err := json.Marshal(map[string]any{"verdict": "fail"})
	if err != nil {
		t.Fatal(err)
	}
	hint, ok := verdictHint(body)
	if !ok {
		t.Fatal("marshalled verdict=fail payload must trigger the hint")
	}
	if !strings.Contains(hint.Error(), "\"finding\"") {
		t.Fatalf("marshalled payload must still carry the redirect, got: %v", hint)
	}
}
