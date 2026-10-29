package assignmentresult

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestLoadRecordedAcceptedReadsOnlyBoundOriginalEnvelope(t *testing.T) {
	state, record, ref, files := acceptedFixture(t)
	reads := 0
	read := func(path string) ([]byte, error) {
		reads++
		if path != ref {
			t.Fatalf("projection revalidated closure source %s", path)
		}
		return files[path], nil
	}
	result, err := LoadRecordedAccepted(state, record, ref, read)
	if err != nil || result.Answer.Summary == "" || reads != 1 {
		t.Fatalf("recorded projection: %+v %d %v", result, reads, err)
	}
	if _, err := LoadRecordedAccepted(state, record, ref, func(path string) ([]byte, error) { return nil, fmt.Errorf("missing actual source %s", path) }); err == nil {
		t.Fatal("missing source accepted")
	}
	files[ref] = append(files[ref], ' ')
	if _, err := LoadRecordedAccepted(state, record, ref, read); err == nil {
		t.Fatal("changed source accepted")
	}
	state, record, ref, files = acceptedFixture(t)
	var envelope AcceptedResult
	if err := json.Unmarshal(files[ref], &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.ProducerAgentID = "another-builder"
	files[ref], _ = json.Marshal(envelope)
	state["evidence"].([]any)[0].(map[string]any)["sha256"] = digestBytes(files[ref])
	if _, err := LoadRecordedAccepted(state, record, ref, read); err == nil {
		t.Fatal("different original owner accepted")
	}
	state, record, ref, files = acceptedFixture(t)
	envelope = AcceptedResult{}
	if err := json.Unmarshal(files[ref], &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Answer.Summary = "edited professional answer"
	files[ref], _ = json.Marshal(envelope)
	state["evidence"].([]any)[0].(map[string]any)["sha256"] = digestBytes(files[ref])
	if _, err := LoadRecordedAccepted(state, record, ref, read); err == nil {
		t.Fatal("changed answer digest accepted")
	}
	state, record, ref, files = acceptedFixture(t)
	record.Revision++
	if _, err := LoadRecordedAccepted(state, record, ref, read); err == nil {
		t.Fatal("non-current Record accepted")
	}
}
