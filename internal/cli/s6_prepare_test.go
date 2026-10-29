package cli

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestS6TaskDoneWhenReadsNumberedClosingContractFromTaskTemplate(t *testing.T) {
	body, err := os.ReadFile("../../docs/dev/tasks/TASK-template.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	text = strings.Replace(text, "assert {contract clause} == satisfied", "assert BE-001 §1 == satisfied", 1)
	text = strings.Replace(text, "assert {verification command} == pass", "assert go test ./internal/example/... == pass", 1)
	if strings.Contains(text, "assert {contract clause}") || strings.Contains(text, "assert {verification command}") {
		t.Fatal("TASK template closing-contract placeholders changed; update this projection test")
	}

	got := s6TaskDoneWhen([]byte(text))
	want := []string{"BE-001 §1 == satisfied", "go test ./internal/example/... == pass", "actual changes stay within the reviewed TASK and assigned write scope", "completion_result explains each condition and any actual scope deviation"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("S6 done_when projection = %#v, want %#v", got, want)
	}
}
