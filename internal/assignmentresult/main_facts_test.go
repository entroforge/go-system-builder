package assignmentresult

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestMainFactsRequireEveryOriginalCheckAndActualReceiptBytes(t *testing.T) {
	state, record, resultRef, files := acceptedFixture(t)
	read := func(ref string) ([]byte, error) {
		data, ok := files[ref]
		if !ok {
			return nil, fmt.Errorf("missing %s", ref)
		}
		return data, nil
	}
	result, err := LoadAccepted(state, record, resultRef, read)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"internal/pkg/handler.go"}
	diffSHA, _ := ChangedPathsDigest(paths)
	mainRoot := "/original-main"
	logRef := "/original-main/.claude/evidence/checks/output.log"
	receiptRef := "/original-main/.claude/evidence/checks/receipt.json"
	head := "actual-tested-head"
	log := []byte("real fixture output bytes")
	receipt := map[string]any{"command": record.RequiredChecks[0], "status": "pass", "exit_code": 0, "cwd": mainRoot, "head": head, "head_before": head, "head_after": head, "output": logRef, "check_binding": checkexec.Binding{RuntimeID: record.RuntimeID, Generation: record.BaselineGeneration, AssignmentID: record.AssignmentID, Owner: "Main", Responsibility: "main_required_integration"}}
	body, _ := json.Marshal(receipt)
	files[logRef] = log
	files[receiptRef] = body
	sha, _ := record.Digest()
	facts := MainFacts{Binding: ResultBinding{AssignmentRecord: record.Reference(), AssignmentRecordSHA256: sha, ResultPath: resultRef, ResultSHA256: digestBytes(files[resultRef]), ResultRevision: result.ResultRevision, AnswerDigest: result.AnswerDigest}, MainRoot: mainRoot, SourceHead: "source", MergeBase: "base", TestedHead: head, TargetBranch: "development", MergeCommit: "non-squash-merge", ChangedPaths: paths, ChangedPathsSHA256: diffSHA, Checks: []MainCheckFact{{CheckIndex: 0, Command: record.RequiredChecks[0], Status: "pass", ReceiptRef: receiptRef, ReceiptSHA256: digestBytes(body), LogRef: logRef, LogSHA256: digestBytes(log), TestedHead: head}}}
	validate := func(value MainFacts) error {
		return ValidateMainFacts(record, result, resultRef, digestBytes(files[resultRef]), value, read)
	}
	if err := validate(facts); err != nil {
		t.Fatal(err)
	}
	missing := facts
	missing.Checks = nil
	if err := validate(missing); err == nil {
		t.Fatal("missing full RequiredCheck coverage accepted")
	}
	for _, field := range []string{"command", "head_before", "exit_code", "check_binding", "output"} {
		old := receipt[field]
		delete(receipt, field)
		files[receiptRef], _ = json.Marshal(receipt)
		invalid := facts
		invalid.Checks = append([]MainCheckFact{}, facts.Checks...)
		invalid.Checks[0].ReceiptSHA256 = digestBytes(files[receiptRef])
		if err := validate(invalid); err == nil {
			t.Fatalf("missing %s accepted", field)
		}
		receipt[field] = old
		files[receiptRef] = body
	}
	files[logRef] = []byte("changed log")
	if err := validate(facts); err == nil {
		t.Fatal("changed actual log accepted")
	}
	files[logRef] = log
	invalid := facts
	invalid.ChangedPaths = []string{"internal/other.go"}
	invalid.ChangedPathsSHA256, _ = ChangedPathsDigest(invalid.ChangedPaths)
	if err := validate(invalid); err == nil {
		t.Fatal("actual out-of-scope Git path accepted")
	}
}

func TestGitChangedPathDigestPreservesWhitespaceAndRejectsAmbiguousSets(t *testing.T) {
	paths, sha, err := CanonicalChangedPaths([]string{"internal/pkg/with\nnewline.go", "internal/pkg/with space.go"})
	if err != nil || len(paths) != 2 || sha == "" {
		t.Fatalf("Git paths=%v sha=%s err=%v", paths, sha, err)
	}
	for _, invalid := range [][]string{nil, {"../escape"}, {"b", "a"}, {"a", "a"}, {"/absolute"}} {
		if _, err := ChangedPathsDigest(invalid); err == nil {
			t.Fatalf("invalid actual path set accepted: %v", invalid)
		}
	}
}
