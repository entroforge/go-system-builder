package workpacket

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

func reviewResultTestBinding() ReviewResultBinding {
	return ReviewResultBinding{
		RuntimeID: "loop-REQ-041", BaselineGeneration: 3,
		ReviewPlanID: "review-plan-041-r1", ReviewPlanRef: ".claude/review/plan.json",
		ReviewPlanRevision: 2,
		AssignmentID:       "assignment-qa-1",
		OwnerAgentID:       "reviewer-qa-1", ClaimIDs: []string{"claim-login", "claim-reset"},
	}
}

func reviewResultTestInputs() ReviewResultInputs {
	return ReviewResultInputs{
		Claims: json.RawMessage(`[
            {"claim_id":"claim-login","lens":"qa","target":"login","assertion":"login works","oracle":"visible home","method":"submit credentials","applicability":"required","source_refs":["REQ-041"]},
            {"claim_id":"claim-reset","lens":"qa","target":"reset","assertion":"reset works","oracle":"email sent","method":"request reset","applicability":"required","source_refs":["REQ-041"]}
        ]`),
		FrozenSubjects: json.RawMessage(`[{"path":"docs/requirements/REQ-041.md"}]`),
	}
}

func TestBuildReviewResultPacketIsStableAndSchemasCompile(t *testing.T) {
	p, err := BuildReviewResult(reviewResultTestBinding(), reviewResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	again, err := BuildReviewResult(reviewResultTestBinding(), reviewResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustReviewPacketJSON(t, p), mustReviewPacketJSON(t, again)) {
		t.Fatal("same binding and frozen questions must produce the same packet")
	}
	if p.TemplateID != "s7-review-result-v1" || p.ResultID != "review-result-"+strings.TrimPrefix(p.ID, "packet-") || p.IssuanceEventID != "evt-review-result-packet-"+p.ID {
		t.Fatalf("derived S7 identity mismatch: %+v", p)
	}
	if !strings.Contains(p.ViewPath, "/S7/") || !ReviewResultUsesNormalAuthoring(p) || !strings.HasSuffix(p.AnswerPath, "/reviewer-qa-1/answer.json") {
		t.Fatalf("S7 paths do not isolate this owner: view=%s answer=%s", p.ViewPath, p.AnswerPath)
	}
	for _, test := range []struct {
		schema string
		data   any
	}{
		{"review-result-work-packet.schema.json", p},
		{"stage-answer-s7.schema.json", p.Draft},
	} {
		data, err := json.Marshal(test.data)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes(test.schema, data); err != nil {
			t.Fatalf("%s: %v", test.schema, err)
		}
	}
	stateBytes, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = "1.2.0"
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	entities := state["entities"].(map[string]any)
	entities["work_packets"] = []any{p}
	entities["submissions"] = []any{}
	stateBytes, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("loop-state.schema.json", stateBytes); err != nil {
		t.Fatalf("v1.2 state work_packets oneOf must resolve the typed S7 schema: %v", err)
	}
	pretty, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var restored ReviewResultPacket
	if err := json.Unmarshal(pretty, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, restored) {
		t.Fatal("packet raw JSON fields should decode to canonical stable bytes")
	}
	otherInputs := reviewResultTestInputs()
	otherInputs.FrozenSubjects = json.RawMessage(`[ { "path" : "docs/requirements/REQ-041.md" } ]`)
	other, err := BuildReviewResult(reviewResultTestBinding(), otherInputs)
	if err != nil || other.ID != p.ID {
		t.Fatalf("JSON object order/spacing should not change packet ID: id=%s err=%v", other.ID, err)
	}
}

func TestValidateReviewResultAnswerAcceptsOnlyCompleteProfessionalAnswer(t *testing.T) {
	p, err := BuildReviewResult(reviewResultTestBinding(), reviewResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	draftBytes, _ := json.Marshal(p.Draft)
	if _, fields := ValidateReviewResultAnswer(p, draftBytes); len(fields) == 0 || fields[0].Pointer != "/answer/verdict" {
		t.Fatalf("blank draft must not be received: %#v", fields)
	}
	var draft map[string]any
	if err := json.Unmarshal(draftBytes, &draft); err != nil {
		t.Fatal(err)
	}
	draft["answer"] = map[string]any{"verdict": "pass"}
	complete, _ := json.Marshal(draft)
	got, fields := ValidateReviewResultAnswer(p, complete)
	if len(fields) != 0 {
		t.Fatalf("original overall verdict rejected: %#v", fields)
	}
	var projection map[string]any
	if err := json.Unmarshal(got, &projection); err != nil {
		t.Fatal(err)
	}
	if projection["verdict"] != "pass" {
		t.Fatalf("validator must retain professional verdict: %#v", projection)
	}

	bound := reviewResultTestBinding()
	boundPacket, err := BuildReviewResult(bound, reviewResultTestInputs())
	if err != nil {
		t.Fatal(err)
	}
	boundBytes, _ := json.Marshal(boundPacket.Draft)
	var boundDraft map[string]any
	_ = json.Unmarshal(boundBytes, &boundDraft)
	boundAnswer := boundDraft["answer"].(map[string]any)
	if _, exists := boundAnswer["verification_artifact_digest"]; exists {
		t.Fatalf("historical digest was copied into new author work: %#v", boundAnswer)
	}
	boundDraft["answer"] = map[string]any{"verdict": "pass"}
	boundAnswer = boundDraft["answer"].(map[string]any)
	boundBytes, _ = json.Marshal(boundDraft)
	if _, fields := ValidateReviewResultAnswer(boundPacket, boundBytes); len(fields) != 0 {
		t.Fatalf("digest-free original answer rejected: %#v", fields)
	}
	boundAnswer["verification_artifact_digest"] = "historical-value-not-a-sha"
	boundBytes, _ = json.Marshal(boundDraft)
	if _, fields := ValidateReviewResultAnswer(boundPacket, boundBytes); len(fields) != 0 {
		t.Fatalf("historical answer digest must not qualify reception: %#v", fields)
	}

	mutated := map[string]any{}
	_ = json.Unmarshal(complete, &mutated)
	mutated["title"] = "edited machine title"
	mutatedBytes, _ := json.Marshal(mutated)
	if _, fields := ValidateReviewResultAnswer(p, mutatedBytes); len(fields) != 0 {
		t.Fatalf("title change must not authenticate the professional answer: %#v", fields)
	}

	invalidVerdict := bytes.Replace(complete, []byte(`"verdict":"pass"`), []byte(`"verdict":"invalid"`), 1)
	if _, fields := ValidateReviewResultAnswer(p, invalidVerdict); len(fields) == 0 {
		t.Fatal("invalid professional verdict accepted")
	}
	wrongClaim := bytes.Replace(complete, []byte(`"verdict":"pass"`), []byte(`"claim_results":[{"claim_id":"claim-other","conclusion":"pass"}],"verdict":"pass"`), 1)
	if _, fields := ValidateReviewResultAnswer(p, wrongClaim); len(fields) != 0 {
		t.Fatalf("cross-responsibility observation rejected: %#v", fields)
	}
	duplicate := bytes.Replace(complete, []byte(`"verdict":"pass"`), []byte(`"verdict":"pass","verdict":"finding"`), 1)
	if _, fields := ValidateReviewResultAnswer(p, duplicate); len(fields) == 0 {
		t.Fatal("duplicate JSON key accepted")
	}
}

func TestBuildReviewResultRejectsClaimDriftAndInvalidBinding(t *testing.T) {
	binding := reviewResultTestBinding()
	inputs := reviewResultTestInputs()
	inputs.Claims = json.RawMessage(strings.Replace(string(inputs.Claims), "claim-reset", "claim-other", 1))
	if _, err := BuildReviewResult(binding, inputs); err == nil {
		t.Fatal("Claim question outside the Assignment binding accepted")
	}
	inputs = reviewResultTestInputs()
	binding.ClaimIDs = []string{"claim-reset", "claim-login"}
	if _, err := BuildReviewResult(binding, inputs); err == nil {
		t.Fatal("Claim order different from the frozen Assignment accepted")
	}
	binding = reviewResultTestBinding()
	binding.OwnerAgentID = "../another-owner"
	if _, err := BuildReviewResult(binding, reviewResultTestInputs()); err == nil {
		t.Fatal("unsafe reviewer identity accepted")
	}
	inputs = reviewResultTestInputs()
	inputs.Claims = json.RawMessage(`[{"claim_id":"claim-login","claim_id":"claim-reset"}]`)
	if _, err := BuildReviewResult(reviewResultTestBinding(), inputs); err == nil {
		t.Fatal("duplicate JSON key in frozen input accepted")
	}
}

func mustReviewPacketJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
