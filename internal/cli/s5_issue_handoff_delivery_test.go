package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func s5IssueOutput(t *testing.T, root string, wantCode int) (string, string) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	code := cli.Run([]string{"runtime", "stage-answer", "issue", "--root", root}, nil, &out, &diagnostic)
	if code != wantCode {
		t.Fatalf("issue code %d want %d: stdout=%s stderr=%s", code, wantCode, &out, &diagnostic)
	}
	return out.String(), diagnostic.String()
}

func assertS5IssueDispatch(t *testing.T, root, output, diagnostic string) []workpacket.Packet {
	t.Helper()
	var packets []workpacket.Packet
	if err := json.Unmarshal([]byte(output), &packets); err != nil || len(packets) != 2 {
		t.Fatalf("issue machine output must remain the original two-packet array: %v %s", err, output)
	}
	for _, p := range packets {
		for _, ref := range []string{p.Binding.Owner + "/" + p.Binding.Responsibility, filepath.Join(root, p.ViewPath), filepath.Join(root, p.AnswerPath), "source root " + root} {
			if !strings.Contains(diagnostic, ref) {
				t.Fatalf("same issue stderr omitted dispatch reference %q: %s", ref, diagnostic)
			}
		}
	}
	for _, instruction := range []string{"separate native Agent context", "SubagentStop reception", "Registration does not prove the platform has started", "real blocker returns to Main"} {
		if !strings.Contains(diagnostic, instruction) {
			t.Fatalf("same issue omitted dispatch boundary %q: %s", instruction, diagnostic)
		}
	}
	if strings.Contains(diagnostic, "原准备失败") {
		t.Fatalf("successful recovery retained failure: %s", diagnostic)
	}
	return packets
}

func TestS5IssueRecoveryDeliversDispatchAndPreservesJSONArray(t *testing.T) {
	root, p := s4HandoffFixture(t)
	missing := filepath.Join(root, ".claude/skills/document-verification/SKILL.md")
	method, err := os.ReadFile(missing)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
	accepted := stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--path", p.AnswerPath)
	if !strings.Contains(accepted, "stage-answer issue") || !strings.Contains(accepted, "document-verification") {
		t.Fatal("actual preparation failure lost its recovery", accepted)
	}
	failedOut, failedDiagnostic := s5IssueOutput(t, root, 1)
	if failedOut != "" || !strings.Contains(failedDiagnostic, "原准备失败") || !strings.Contains(failedDiagnostic, "document-verification") || !strings.Contains(failedDiagnostic, "stage-answer issue") {
		t.Fatalf("issue failure lost original diagnosis: %s %s", failedOut, failedDiagnostic)
	}
	if err := os.WriteFile(missing, method, 0600); err != nil {
		t.Fatal(err)
	}
	out, diagnostic := s5IssueOutput(t, root, 0)
	packets := assertS5IssueDispatch(t, root, out, diagnostic)
	before := stageSnapshot(t, root).Revision
	again, againDiagnostic := s5IssueOutput(t, root, 0)
	repeated := assertS5IssueDispatch(t, root, again, againDiagnostic)
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("idempotent issue added a responsibility or revision")
	}
	for i, p := range packets {
		if repeated[i].ID != p.ID {
			t.Fatal("repeated issue replaced packet identity")
		}
	}
}

func TestS5IssuePartialPublicationReportsFailureAndPreservesPeer(t *testing.T) {
	root := newUnregisteredS5InputFixture(t)
	out, diagnostic := s5IssueOutput(t, root, 0)
	packets := assertS5IssueDispatch(t, root, out, diagnostic)
	authored := []byte("{\"author_work\":\"preserve peer judgment\"}\n")
	peerAnswer := filepath.Join(root, packets[0].AnswerPath)
	if err := os.WriteFile(peerAnswer, authored, 0600); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(root, packets[1].ViewPath)
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	before := stageSnapshot(t, root).Revision
	failedOut, failedDiagnostic := s5IssueOutput(t, root, 1)
	for _, want := range []string{"原准备失败", "stage-answer issue", blocked, "registered file unavailable", filepath.Join(root, packets[0].ViewPath), peerAnswer} {
		if !strings.Contains(failedDiagnostic, want) {
			t.Fatalf("partial failure lost %q: %s", want, failedDiagnostic)
		}
	}
	if failedOut != "" || strings.Contains(failedDiagnostic, "原责任已准备") {
		t.Fatalf("failed preparation claimed success: %s %s", failedOut, failedDiagnostic)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	resumed, resumedDiagnostic := s5IssueOutput(t, root, 0)
	recovered := assertS5IssueDispatch(t, root, resumed, resumedDiagnostic)
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("publication recovery changed original responsibilities")
	}
	for i, p := range packets {
		if recovered[i].ID != p.ID {
			t.Fatal("partial recovery replaced packet identity")
		}
	}
	if actual, err := os.ReadFile(peerAnswer); err != nil || !bytes.Equal(actual, authored) {
		t.Fatalf("peer answer overwritten: %s %v", actual, err)
	}
	if _, err := os.Stat(blocked); err != nil {
		t.Fatal("missing view not restored", err)
	}
	again, againDiagnostic := s5IssueOutput(t, root, 0)
	assertS5IssueDispatch(t, root, again, againDiagnostic)
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("repeat recovery changed original responsibilities")
	}
}
