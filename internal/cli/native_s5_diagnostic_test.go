package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestNativeS5ReturnPreservesDistinctStopDiagnosticsWithoutReadingAnswers(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	ref := filepath.Join(f.root, p.ViewPath)
	before := stageSnapshot(t, f.root).Revision
	var previous string
	for _, tc := range []struct {
		pointer string
		edit    func(*workpacket.Draft)
	}{
		{"/answer/conclusion", func(d *workpacket.Draft) { d.Answer.Conclusion = nil }},
		{"/answer/findings", func(d *workpacket.Draft) {
			d.Answer.Findings = []workpacket.Finding{{Path: p.Inputs[0].Path, Expected: "required", Observed: "missing"}}
		}},
	} {
		draft := completedDraft(p, "pass")
		tc.edit(&draft)
		stageJSON(t, f.root, p.AnswerPath, draft)
		code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "s5-diagnostic-reviewer", ref)
		if code != 0 || !strings.Contains(diagnostic, tc.pointer) {
			t.Fatalf("original Stop did not preserve its failure: %d %s", code, diagnostic)
		}
		// Main must get the receiver's reason even when the private artifact
		// is no longer readable. It does not repeat field or source checks.
		if err := os.Remove(filepath.Join(f.root, p.AnswerPath)); err != nil {
			t.Fatal(err)
		}
		// A previous Stop failure does not prove that a later status-less
		// observation or asynchronous launch is a completed return.
		for _, observation := range []struct {
			response map[string]any
			want     []string
		}{
			{map[string]any{"content": []any{map[string]any{"type": "text", "text": "Finished."}}}, []string{"Platform execution is unknown here", "Do not infer a running or returned Worker, a failed Stop, or a need to re-dispatch"}},
			{map[string]any{"status": "async_launched", "isAsync": true}, []string{"launch acknowledgement", "not a Worker return; it requests no reception repair"}},
		} {
			text := mainNativeReturnResponse(t, f.root, ref, observation.response)
			for _, want := range append(observation.want, tc.pointer, p.Binding.Owner, filepath.Join(f.root, p.AnswerPath), `"accepted":false`, `"work_complete":false`) {
				if !strings.Contains(text, want) {
					t.Fatalf("execution observation lost %q: %s", want, text)
				}
			}
			for _, bad := range []string{"Native return for ", "last attempted reception", "续跑原责任修正具体字段", "No readable original Stop diagnostic"} {
				if strings.Contains(text, bad) {
					t.Fatalf("execution observation inferred completion or a new recovery duty %q: %s", bad, text)
				}
			}
		}
		text := mainNativeReturn(t, f.root, ref)
		for _, want := range []string{tc.pointer, p.Binding.Owner, filepath.Join(f.root, p.AnswerPath), `"accepted":false`, "last attempted reception"} {
			if !strings.Contains(text, want) {
				t.Fatalf("Main lost %q: %s", want, text)
			}
		}
		if text == previous {
			t.Fatal("different original failures collapsed into the same Main context")
		}
		previous = text
	}
	peer := mainNativeReturnResponse(t, f.root, filepath.Join(f.root, f.packets[1].ViewPath), map[string]any{"status": "unknown"})
	if !strings.Contains(peer, "Platform execution is unknown here") || strings.Contains(peer, "Last S5 native Stop "+p.ID) {
		t.Fatalf("another responsibility inherited this Stop error: %s", peer)
	}
	snapshot := stageSnapshot(t, f.root)
	if snapshot.Revision != before || snapshot.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Fatal("failed Stop or Main observation advanced Runtime")
	}
	if _, accepted, err := runtime.FindSubmission(snapshot.State, p.ID); err != nil || accepted {
		t.Fatalf("diagnostic became reception: %v", err)
	}
}

func TestNativeS5ReturnRetainsSourceFailureAndClearsItOnReception(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	stageJSON(t, f.root, p.AnswerPath, completedDraft(p, "pass"))
	analysis := filepath.Join(f.root, p.TaskAnalysis.Path)
	data, err := os.ReadFile(analysis)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(analysis); err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(f.root, p.ViewPath)
	code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "s5-source-reviewer", ref)
	if code != 0 || !strings.Contains(diagnostic, "S5 reception incomplete") {
		t.Fatalf("source blocker was not handed off: %d %s", code, diagnostic)
	}
	text := mainNativeReturn(t, f.root, ref)
	if !strings.Contains(text, p.TaskAnalysis.Path) || !strings.Contains(text, `"accepted":false`) {
		t.Fatalf("source reason did not reach Main: %s", text)
	}
	var sourceOut, sourceErr bytes.Buffer
	sourceBefore := dryRunTree(t, f.root)
	code = cli.Run([]string{"runtime", "stage-answer", "receipt", "--root", f.root, "--agent", p.Binding.Owner, "--path", filepath.Join(f.root, p.AnswerPath)}, nil, &sourceOut, &sourceErr)
	if code == 0 || !strings.Contains(sourceOut.String(), p.TaskAnalysis.Path) || !strings.Contains(sourceOut.String(), `"accepted":false`) {
		t.Fatalf("explicit original diagnosis missed source failure: %d %s %s", code, &sourceOut, &sourceErr)
	}
	if !reflect.DeepEqual(sourceBefore, dryRunTree(t, f.root)) {
		t.Fatal("explicit source diagnosis changed project files")
	}
	if err := os.WriteFile(analysis, data, 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "s5-source-reviewer", ref); code != 0 {
		t.Fatal(diagnostic)
	}
	if err := os.Remove(filepath.Join(f.root, p.AnswerPath)); err != nil {
		t.Fatal(err)
	}
	text = mainNativeReturn(t, f.root, ref)
	if !strings.Contains(text, `"accepted":true`) || strings.Contains(text, "Last S5 native Stop") {
		t.Fatalf("accepted history inherited old failure or required private answer: %s", text)
	}
	if got := submission.PlanningDiagnostic(f.root, stageSnapshot(t, f.root).State, "S5 native Stop "+p.ID); got != "" {
		t.Fatalf("successful reception did not clear last failure: %s", got)
	}
	// The explicit diagnosis entry also preserves accepted history after the
	// private answer is removed; it does not demand a second review.
	var out, errout bytes.Buffer
	before := dryRunTree(t, f.root)
	code = cli.Run([]string{"runtime", "stage-answer", "receipt", "--root", f.root, "--agent", p.Binding.Owner, "--path", filepath.Join(f.root, p.AnswerPath)}, nil, &out, &errout)
	if code != 0 || !strings.Contains(out.String(), `"accepted":true`) {
		t.Fatalf("explicit receipt rechecked accepted private answer: %d %s %s", code, &out, &errout)
	}
	if !reflect.DeepEqual(before, dryRunTree(t, f.root)) {
		t.Fatal("explicit accepted receipt changed project files")
	}
}

func TestNativeS5MissingDiagnosticNamesReadOnlyOriginalReceiptRecovery(t *testing.T) {
	for _, name := range []string{"missing", "damaged", "write_failure"} {
		t.Run(name, func(t *testing.T) {
			f := newStageAnswerFixture(t)
			p := f.packets[0]
			ref := filepath.Join(f.root, p.ViewPath)
			if name == "write_failure" {
				parent := filepath.Join(f.root, ".claude/diagnostics")
				if err := os.MkdirAll(parent, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(parent, "planning"), []byte("not a diagnostic directory"), 0600); err != nil {
					t.Fatal(err)
				}
				if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "s5-recovery-reviewer", ref); code != 0 || !strings.Contains(diagnostic, "/answer/conclusion") {
					t.Fatalf("display write failure blocked honest handoff: %d %s", code, diagnostic)
				}
			}
			if name == "damaged" {
				if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "s5-recovery-reviewer", ref); code != 0 {
					t.Fatal(diagnostic)
				}
				files, err := filepath.Glob(filepath.Join(f.root, ".claude/diagnostics/planning/*.json"))
				if err != nil || len(files) == 0 {
					t.Fatalf("missing diagnostic fixture: %v", err)
				}
				for _, path := range files {
					if err := os.WriteFile(path, []byte("damaged display cache"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			text := mainNativeReturn(t, f.root, ref)
			for _, want := range []string{"No readable original Stop diagnostic", "runtime stage-answer receipt", p.Binding.Owner, filepath.Join(f.root, p.AnswerPath), `"accepted":false`} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing precise diagnostic recovery %q: %s", want, text)
				}
			}
			before := dryRunTree(t, f.root)
			var out, diagnostic bytes.Buffer
			code := cli.Run([]string{"runtime", "stage-answer", "receipt", "--root", f.root, "--agent", p.Binding.Owner, "--path", filepath.Join(f.root, p.AnswerPath)}, nil, &out, &diagnostic)
			var receipt submission.Receipt
			if err := json.Unmarshal(out.Bytes(), &receipt); err != nil || code == 0 || receipt.Accepted || len(receipt.FieldErrors) == 0 || receipt.FieldErrors[0].Pointer != "/answer/conclusion" {
				t.Fatalf("explicit original diagnosis did not expose exact field: code=%d receipt=%+v err=%v %s", code, receipt, err, &diagnostic)
			}
			if !reflect.DeepEqual(before, dryRunTree(t, f.root)) {
				t.Fatal("explicit diagnosis received an answer or wrote project files")
			}
		})
	}
}

func TestNativeS5HistoricalReturnDoesNotResumeUnreceivedReview(t *testing.T) {
	f := newStageAnswerFixture(t)
	pending, completed := f.packets[0], f.packets[1]
	ref := filepath.Join(f.root, pending.ViewPath)
	if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "pending-reviewer", ref); code != 0 {
		t.Fatal(diagnostic)
	}
	stageJSON(t, f.root, completed.AnswerPath, completedDraft(completed, "fix_required"))
	if code, _, diagnostic := nativeReviewEvent(t, f.root, "SubagentStop", "finding-reviewer", filepath.Join(f.root, completed.ViewPath)); code != 0 {
		t.Fatal(diagnostic)
	}
	if stageSnapshot(t, f.root).State["lifecycle"].(map[string]any)["state"] == "document_verification" {
		t.Fatal("the accepted finding did not take the original planning rework route")
	}
	text := mainNativeReturn(t, f.root, ref)
	if !strings.Contains(text, "This S5 reference is historical") || !strings.Contains(text, `"accepted":false`) || !strings.Contains(text, "不恢复这个旧 attempt") {
		t.Fatalf("late unreceived return lost its historical boundary: %s", text)
	}
	for _, stale := range []string{"Last S5 native Stop", "runtime stage-answer receipt", "续跑原责任修正具体字段"} {
		if strings.Contains(text, stale) {
			t.Fatalf("historical return revived old diagnostic work %q: %s", stale, text)
		}
	}
}
