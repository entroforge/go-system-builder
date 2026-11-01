package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func registeredS5WithoutIssued(t *testing.T) (string, []workpacket.Packet) {
	t.Helper()
	root := newUnregisteredS5InputFixture(t)
	methodPath := filepath.Join(root, ".claude/skills/document-verification/SKILL.md")
	method, err := os.ReadFile(methodPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(methodPath); err != nil {
		t.Fatal(err)
	}
	s5IssueOutput(t, root, 1)
	if err := os.WriteFile(methodPath, method, 0600); err != nil {
		t.Fatal(err)
	}
	packets, err := runtime.S5Packets(root, stageSnapshot(t, root).State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("registered responsibilities before issuance: %d %v", len(packets), err)
	}
	return root, packets
}

func TestS5OriginalIssuePreservesUnknownOccupantsAndReceivesOwner(t *testing.T) {
	root, proposed := registeredS5WithoutIssued(t)
	unknown := []byte("unknown original; never overwrite\n")
	analysis := filepath.ToSlash(filepath.Join(".claude/work-packets/analysis", proposed[0].ID+".json"))
	for _, rel := range []string{analysis, proposed[0].ViewPath, proposed[0].AnswerPath} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, unknown, 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, diagnostic := s5IssueOutput(t, root, 0)
	issued := assertS5IssueDispatch(t, root, out, diagnostic)
	var target workpacket.Packet
	for _, packet := range issued {
		if packet.ID == proposed[0].ID {
			target = packet
		}
	}
	if target.ID == "" || target.ViewPath == proposed[0].ViewPath || target.AnswerPath == proposed[0].AnswerPath || target.TaskAnalysis == nil || target.TaskAnalysis.Path == analysis {
		t.Fatalf("unknown paths were reused instead of independently issued: %+v", target)
	}
	for _, rel := range []string{analysis, proposed[0].ViewPath, proposed[0].AnswerPath} {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(got, unknown) {
			t.Fatalf("unknown original changed %s: %q %v", rel, got, err)
		}
	}
	stageJSON(t, root, target.AnswerPath, completedDraft(target, "pass"))
	if code, _, diagnostic := nativeReviewEvent(t, root, "SubagentStop", "s5-collision-owner", filepath.Join(root, target.ViewPath)); code != 0 {
		t.Fatalf("original owner answer: %d %s", code, diagnostic)
	}
	if _, accepted, err := runtime.FindSubmission(stageSnapshot(t, root).State, target.ID); err != nil || !accepted {
		t.Fatalf("issued original was not received: accepted=%v err=%v", accepted, err)
	}
}

func TestS5ConcurrentIssueUsesRuntimeWinner(t *testing.T) {
	root, _ := registeredS5WithoutIssued(t)
	type outcome struct {
		code int
		out  string
		err  string
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			ready.Done()
			<-start
			service, err := submission.New(root)
			if err != nil {
				results <- outcome{code: 1, err: err.Error()}
				return
			}
			packets, err := service.Issue("")
			if err != nil {
				results <- outcome{code: 1, err: err.Error()}
				return
			}
			out, err := json.Marshal(packets)
			if err != nil {
				results <- outcome{code: 1, err: err.Error()}
				return
			}
			results <- outcome{out: string(out)}
		}()
	}
	ready.Wait()
	close(start)
	a, b := <-results, <-results
	if a.code != 0 || b.code != 0 {
		t.Fatalf("concurrent original issuance: first=%+v second=%+v", a, b)
	}
	var first, second []workpacket.Packet
	if err := json.Unmarshal([]byte(a.out), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(b.out), &second); err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("wrong issued responsibility count: %d %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID || first[i].ViewPath != second[i].ViewPath || first[i].AnswerPath != second[i].AnswerPath {
			t.Fatalf("issuers did not return Runtime's winner: %+v / %+v", first[i], second[i])
		}
	}
}
