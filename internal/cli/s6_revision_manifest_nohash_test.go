package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestS6RevisionDraftOmitsOnlyHistoricalMachineDigests(t *testing.T) {
	original := []byte(`{"documents":[{"id":"TASK-1","path":"task.md","version":"v1","sha256":{"old":true},"author_note":{"sha256":"business text"}}],"description":"The business description mentions SHA and hash.","assignments":[{"assignment_id":"assignment-1","agent_id":"old-owner","check_profiles":[{"description":"Keep SHA in professional prose","dependencies":[{"path":"source","sha256":7,"author_note":{"hash":"business detail"}}],"fixtures":[{"path":"fixture","sha256":"old-fixture"}]}],"closing_contract":{"worker_checks":[{"profile":{"dependencies":[{"path":"worker-source","sha256":null,"author_note":["professional SHA note"]}],"fixtures":[]}}]}}]}`)
	before := append([]byte(nil), original...)
	data, err := s6CloneRevisionManifest(original, "manifest-new", "workgroup-new", "team-new", "new-owner", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, before) {
		t.Fatal("original manifest bytes changed")
	}
	var draft map[string]any
	if err := json.Unmarshal(data, &draft); err != nil {
		t.Fatal(err)
	}
	if draft["description"] != "The business description mentions SHA and hash." {
		t.Fatal("professional text changed")
	}
	document := draft["documents"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(document["author_note"], map[string]any{"sha256": "business text"}) {
		t.Fatal("document professional extension lost")
	}
	if _, exists := document["sha256"]; exists {
		t.Fatal("document digest copied into new draft")
	}
	assignment := draft["assignments"].([]any)[0].(map[string]any)
	profile := assignment["check_profiles"].([]any)[0].(map[string]any)
	if profile["description"] != "Keep SHA in professional prose" {
		t.Fatal("profile prose changed")
	}
	if !reflect.DeepEqual(profile["dependencies"].([]any)[0].(map[string]any)["author_note"], map[string]any{"hash": "business detail"}) {
		t.Fatal("dependency professional extension lost")
	}
	for _, field := range []string{"dependencies", "fixtures"} {
		if _, exists := profile[field].([]any)[0].(map[string]any)["sha256"]; exists {
			t.Fatalf("%s digest copied", field)
		}
	}
	worker := assignment["closing_contract"].(map[string]any)["worker_checks"].([]any)[0].(map[string]any)["profile"].(map[string]any)
	if !reflect.DeepEqual(worker["dependencies"].([]any)[0].(map[string]any)["author_note"], []any{"professional SHA note"}) {
		t.Fatal("worker professional extension lost")
	}
	if _, exists := worker["dependencies"].([]any)[0].(map[string]any)["sha256"]; exists {
		t.Fatal("worker digest copied")
	}
}
