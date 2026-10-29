package mainreceipt

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
)

func TestPreservedMergeIsUnverifiedWithoutBecomingSuccessfulProof(t *testing.T) {
	for _, name := range []string{"bound preserved merge", "wrong Result", "wrong execution", "wrong completion field", "wrong Main root", "wrong branch", "missing merge", "wrong merge base", "verified without tested head"} {
		t.Run(name, func(t *testing.T) {
			f := newReaderFixture(t)
			cp := f.checkpoint
			cp.State, cp.TestedHead, cp.VerifiedAt = "preserved", "", ""
			cp.ReportPath, cp.ReportSHA = "", ""
			cp.CanonicalMainFacts.TestedHead = ""
			cp.CanonicalMainFacts.Checks = nil
			switch name {
			case "wrong Result":
				cp.CanonicalMainFacts.Binding.ResultSHA256 = strings.Repeat("f", 64)
			case "wrong execution":
				cp.CanonicalMainFacts.ExecutionGeneration++
			case "wrong completion field":
				cp.ReportPath = "wrong-result.json"
			case "wrong Main root":
				cp.CanonicalMainFacts.MainRoot = t.TempDir()
			case "wrong branch":
				cp.TargetBranch, cp.CanonicalMainFacts.TargetBranch = "other", "other"
			case "missing merge":
				cp.MergeCommit, cp.CanonicalMainFacts.MergeCommit = strings.Repeat("f", 40), strings.Repeat("f", 40)
			case "wrong merge base":
				cp.MergeBase, cp.CanonicalMainFacts.MergeBase = f.sourceHead, f.sourceHead
			case "verified without tested head":
				cp.State = "verified"
			}
			writeCheckpointFixture(t, f, cp)
			path := assignmentrecord.IntegrationCheckpointPath(f.root, f.input.Record.Reference())
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			proof, err := Verify(context.Background(), f.input)
			if err == nil || proof.CheckpointSHA256() != "" || proof.MainFacts().TestedHead != "" {
				t.Fatalf("unverified attempt returned successful proof: %+v, err=%v", proof, err)
			}
			if got, want := errors.Is(err, ErrNotVerified), name == "bound preserved merge"; got != want {
				t.Fatalf("ErrNotVerified=%t, want %t; err=%v", got, want, err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(before, after) {
				t.Fatalf("read-only verification changed preserved checkpoint: %v", readErr)
			}
		})
	}
}

func TestPreMergeFailureRemainsUnverifiedWithoutInventingMainFacts(t *testing.T) {
	for _, name := range []string{"original pre-merge failure", "early before source", "early before target", "early before base", "malformed source", "target without source", "base without target", "wrong Record", "wrong branch", "claims verification", "claims merge", "claims report", "claims runner receipt", "claims changed paths"} {
		t.Run(name, func(t *testing.T) {
			f := newReaderFixture(t)
			cp := f.checkpoint
			ref := f.input.Record.Reference()
			cp.AssignmentRecord = &ref
			cp.AssignmentRecordSHA256, _ = f.input.Record.Digest()
			cp.CanonicalMainFacts = nil
			cp.State, cp.VerifiedAt, cp.TestedHead, cp.MergeCommit = "preserved", "", "", ""
			cp.ReportPath, cp.ReportSHA, cp.ChangedPathsSHA256 = "", "", ""
			cp.ChangedPaths, cp.CheckReceipts, cp.ToolCheckReceipts = nil, nil, nil
			switch name {
			case "early before source":
				cp.SourceHead, cp.TargetHead, cp.MergeBase = "", "", ""
			case "early before target":
				cp.TargetHead, cp.MergeBase = "", ""
			case "early before base":
				cp.MergeBase = ""
			case "malformed source":
				cp.SourceHead = "not-a-Git-object"
			case "target without source":
				cp.SourceHead, cp.MergeBase = "", ""
			case "base without target":
				cp.TargetHead = ""
			case "wrong Record":
				cp.AssignmentRecordSHA256 = strings.Repeat("f", 64)
			case "wrong branch":
				cp.TargetBranch = "unrelated"
			case "claims verification":
				cp.State = "verified"
			case "claims merge":
				cp.MergeCommit = f.mergeHead
			case "claims report":
				cp.ReportPath = f.input.ResultRef
			case "claims runner receipt":
				cp.CheckReceipts = []string{f.receipt}
			case "claims changed paths":
				cp.ChangedPaths = []string{"src/claimed.go"}
			}
			writeCheckpointFixture(t, f, cp)
			path := assignmentrecord.IntegrationCheckpointPath(f.root, ref)
			before, _ := os.ReadFile(path)
			proof, err := Verify(context.Background(), f.input)
			if err == nil || proof.CheckpointSHA256() != "" || proof.MainFacts().TestedHead != "" {
				t.Fatalf("failure became successful proof: %+v %v", proof, err)
			}
			if errors.Is(err, ErrNotVerified) != (name == "original pre-merge failure" || strings.HasPrefix(name, "early before ")) {
				t.Fatalf("classification %q: %v", name, err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(before, after) {
				t.Fatalf("verification rewrote failure history: %v", readErr)
			}
		})
	}
}
