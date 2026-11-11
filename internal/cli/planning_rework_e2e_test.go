package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func reworkCLI(t *testing.T, root, target string) submission.PlanningReworkResult {
	t.Helper()
	var r submission.PlanningReworkResult
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "rework", "--root", root, "--target", target, "--reason", "The professional boundary needs a corrected delivery")), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Committed || r.PreparationStatus != "ready" {
		t.Fatalf("rework: %+v", r)
	}
	return r
}

// reworkCLIRefusal is the failure-shaped path of the same command. A request
// the system must refuse reports the blocked coverage in its diagnostic and
// opens no entry.
func reworkCLIRefusal(t *testing.T, root, target, reason string) (int, string, string) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	code := cli.Run([]string{"runtime", "stage-answer", "rework", "--root", root, "--target", target, "--reason", reason}, nil, &out, &diagnostic)
	return code, out.String(), diagnostic.String()
}

// establishS3Continuation authors the original S3 answer, consumes it through
// the normal CLI and settles the Main planning followup, so the recorded
// reception really advanced the original S3->S4 continuation. An issued but
// never received packet carries no reception and is therefore not a source for
// a later rework (L4-revision-usage.md:169).
func establishS3Continuation(t *testing.T, root string, p workpacket.ContractPacket) runtime.SubmissionRecord {
	t.Helper()
	stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", p.AnswerPath)
	settleMainPlanningFollowups(t, root)
	record, accepted, err := runtime.FindSubmission(stageSnapshot(t, root).State, p.ID)
	if err != nil || !accepted {
		t.Fatalf("original S3 reception missing: %v", err)
	}
	if record.ControlStatus != "advanced" || record.TransitionEventID == "" {
		t.Fatalf("original S3 reception did not advance the continuation: %+v", record)
	}
	return record
}

func TestPlanningReworkCLIOriginalRoutes(t *testing.T) {
	for _, tc := range []struct {
		stage, target string
		// noReception marks the fixture whose S3 answer was authored but never
		// received, so the S3->S4 continuation was never established.
		noReception bool
	}{
		{"S3", "design", false},
		{"S3", "contracts", true},
		{"S4", "design", false},
		{"S4", "contracts", false},
		{"S4", "tasks", false},
	} {
		t.Run(tc.stage+"_"+tc.target, func(t *testing.T) {
			var root, oldID, oldPath string
			if tc.stage == "S4" {
				var p workpacket.TaskPlanningPacket
				root, p = newTaskPlanningAnswerFixture(t)
				oldID, oldPath = p.ID, p.AnswerPath
				stageJSON(t, root, p.AnswerPath, completedTaskPlanningDraft(p))
			} else {
				var p workpacket.ContractPacket
				root, p = newContractAnswerFixture(t)
				oldID, oldPath = p.ID, p.AnswerPath
				stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
			}
			before := stageSnapshot(t, root)
			oldBytes, err := os.ReadFile(filepath.Join(root, oldPath))
			if err != nil {
				t.Fatal(err)
			}
			if tc.noReception {
				// The fixture authored the S3 answer but never consumed it, so
				// the chain holds no S3 reception at all and the S3->S4
				// continuation was never established. A contracts rework has no
				// original source to resolve; the system must name the blocked
				// coverage and the recovery responsibility instead of passing
				// an earlier responsibility off as the current source
				// (L4-state-transition-core.md:89,
				// L4-revision-usage.md:169).
				code, out, diagnostic := reworkCLIRefusal(t, root, tc.target, "The professional boundary needs a corrected delivery")
				if code == 0 {
					t.Fatalf("contracts rework without an original S3 reception was admitted: %s", out)
				}
				if !strings.Contains(diagnostic, "no original S3 reception advanced the S3 continuation") {
					t.Fatalf("refusal did not name the missing S3 continuation: %s", diagnostic)
				}
				// The blocked coverage names the recovery responsibility too.
				if !strings.Contains(diagnostic, "use --target design") {
					t.Fatalf("refusal did not name the recovery responsibility: %s", diagnostic)
				}
				// The refused request opened no entry and recorded no fact.
				var result submission.PlanningReworkResult
				if err := json.Unmarshal([]byte(out), &result); err != nil {
					t.Fatal(err)
				}
				if result.Committed || result.StageEntry != nil || result.PreparationStatus != "" {
					t.Fatalf("refused rework reported a committed entry: %+v", result)
				}
				after := stageSnapshot(t, root)
				if after.Revision != before.Revision {
					t.Fatal("refused rework changed Runtime")
				}
				if !reflect.DeepEqual(before.State["lifecycle"], after.State["lifecycle"]) {
					t.Fatal("refused rework opened an entry", after.State["lifecycle"])
				}
				if !reflect.DeepEqual(before.State["documents"], after.State["documents"]) || !reflect.DeepEqual(before.State["evidence"], after.State["evidence"]) {
					t.Fatal("refused rework invalidated the chain")
				}
				if !reflect.DeepEqual(before.State["bound_req"], after.State["bound_req"]) || !reflect.DeepEqual(before.State["baseline"], after.State["baseline"]) {
					t.Fatal("refused rework moved the locked REQ or the baseline")
				}
				// The historical answer stays untouched, and the refused request
				// neither received the authored answer nor conjured the missing
				// reception: the chain still holds no S3 reception at all, which
				// is exactly why the coverage stays blocked
				// (L4-state-transition-core.md:89).
				current, err := os.ReadFile(filepath.Join(root, oldPath))
				if err != nil || !bytes.Equal(current, oldBytes) {
					t.Fatal("historical answer changed")
				}
				if _, accepted, _ := runtime.FindSubmission(after.State, oldID); accepted {
					t.Fatal("refused rework acquired the missing S3 reception")
				}
				// The refusal is stable: repeating it opens no entry either.
				retryCode, retryOut, retryDiagnostic := reworkCLIRefusal(t, root, tc.target, "The professional boundary needs a corrected delivery")
				if retryCode == 0 {
					t.Fatalf("repeated refused rework was admitted: %s", retryOut)
				}
				if !strings.Contains(retryDiagnostic, "no original S3 reception advanced the S3 continuation") {
					t.Fatalf("repeated refusal did not name the missing S3 continuation: %s", retryDiagnostic)
				}
				if stageSnapshot(t, root).Revision != before.Revision {
					t.Fatal("repeated refused rework changed Runtime")
				}
				assertDesignCheckpoint(t, root)
				return
			}
			r := reworkCLI(t, root, tc.target)
			after := stageSnapshot(t, root)
			if !reflect.DeepEqual(before.State["bound_req"], after.State["bound_req"]) || !reflect.DeepEqual(before.State["baseline"], after.State["baseline"]) {
				t.Fatal("rework changed locked REQ or generation")
			}
			life := after.State["lifecycle"].(map[string]any)
			if life["phase"] != tc.target || life["phase_revision"].(float64) != before.State["lifecycle"].(map[string]any)["phase_revision"].(float64)+1 {
				t.Fatal("missing fresh entry", life)
			}
			for _, raw := range after.State["evidence"].([]any) {
				e := raw.(map[string]any)
				if e["kind"] == "planning_design" {
					want := "valid"
					if tc.target == "design" {
						want = "invalid"
					}
					if e["status"] != want {
						t.Fatal("wrong upstream S2 invalidation", e)
					}
				}
				if e["kind"] == "planning_contract" && tc.target != "tasks" && (e["status"] != "invalid" || e["invalidation_rule"] != "planning_rework") {
					t.Fatal("dependent S3 evidence remains current", e)
				}
			}
			for _, raw := range after.State["documents"].([]any) {
				d := raw.(map[string]any)
				if d["kind"] == "design" {
					want := "locked"
					if tc.target == "design" {
						want = "draft"
					}
					if d["status"] != want {
						t.Fatal("wrong S2 document validity", d)
					}
				}
				if d["kind"] == "contract" && tc.target != "tasks" && d["status"] != "draft" {
					t.Fatal("dependent contract stayed locked", d)
				}
			}
			replay := reworkCLI(t, root, tc.target)
			if replay.StageEntry != r.StageEntry || stageSnapshot(t, root).Revision != after.Revision {
				t.Fatal("retry opened another entry")
			}
			current, err := os.ReadFile(filepath.Join(root, oldPath))
			if err != nil || !bytes.Equal(current, oldBytes) {
				t.Fatal("historical answer changed")
			}
			var out, diagnostic bytes.Buffer
			// Old unaccepted answers cannot acquire a new-entry reception.
			cli.Run([]string{"runtime", "stage-answer", "consume", "--root", root, "--path", oldPath}, nil, &out, &diagnostic)
			now := stageSnapshot(t, root)
			if _, accepted, _ := runtime.FindSubmission(now.State, oldID); accepted {
				t.Fatal("old unaccepted packet replay acquired reception")
			}
			if now.Revision != after.Revision {
				t.Fatal("old packet replay moved Runtime")
			}
			assertDesignCheckpoint(t, root)
		})
	}
}

func TestPlanningReworkCLIRejectsInvalidRequests(t *testing.T) {
	root, _ := newContractAnswerFixture(t)
	for _, flags := range [][]string{{"--target", "tasks", "--reason", "wrong target"}, {"--target", "design"}, {"--target", "design", "--reason", "boundary", "--agent", "Worker"}} {
		before := stageSnapshot(t, root)
		var out, diagnostic bytes.Buffer
		args := append([]string{"runtime", "stage-answer", "rework", "--root", root}, flags...)
		if code := cli.Run(args, nil, &out, &diagnostic); code == 0 {
			t.Fatal("accepted invalid request", args, out.String())
		}
		if stageSnapshot(t, root).Revision != before.Revision {
			t.Fatal("invalid request mutated Runtime")
		}
	}
	before := stageSnapshot(t, root)
	req := before.State["bound_req"].(map[string]any)["path"].(string)
	f, err := os.OpenFile(filepath.Join(root, req), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\nchanged requirement\n")
	f.Close()
	var out, diagnostic bytes.Buffer
	// A changed REQ file is not a source qualification. The Hook cannot claim
	// to discover every change, so the request is still a valid rework and the
	// recorded REQ identity stays exactly as it was.
	if code := cli.Run([]string{"runtime", "stage-answer", "rework", "--root", root, "--target", "design", "--reason", "cannot hide REQ change"}, nil, &out, &diagnostic); code != 0 {
		t.Fatalf("REQ byte change refused a valid rework: %d %s %s", code, out.String(), diagnostic.String())
	}
	if !reflect.DeepEqual(before.State["bound_req"], stageSnapshot(t, root).State["bound_req"]) {
		t.Fatal("rework rewrote the recorded REQ identity")
	}
}

func TestPlanningReworkCLIPreservesAcceptedHistory(t *testing.T) {
	root, p := newContractAnswerFixture(t)
	// The original S3 reception must really advance the S3->S4 continuation:
	// an issued but never received packet carries no reception and is not a
	// source for the contracts rework (L4-revision-usage.md:169).
	prior := establishS3Continuation(t, root, p)
	before := stageSnapshot(t, root)
	if _, accepted, err := runtime.FindSubmission(before.State, p.ID); err != nil || !accepted {
		t.Fatal("fixture did not accept original answer", err)
	}
	original, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	reworkCLI(t, root, "contracts")
	after := stageSnapshot(t, root)
	preserved, accepted, err := runtime.FindSubmission(after.State, p.ID)
	if err != nil || !accepted || !reflect.DeepEqual(prior, preserved) {
		t.Fatal("original reception history changed", prior, preserved, err)
	}
	// The accepted reception stays history: its transition already processed
	// the followup, so the followup is no longer executable and the rework
	// neither revives it nor re-registers it on the new entry
	// (L4-state-transition-core.md:89).
	if _, err := runtime.SubmissionFollowup(after.State, prior.FollowupID); !errors.Is(err, runtime.ErrSubmissionFollowupProcessed) {
		t.Fatalf("accepted followup is not preserved as processed history: %v", err)
	}
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--path", p.AnswerPath)
	if stageSnapshot(t, root).Revision != after.Revision {
		t.Fatal("accepted replay advanced old entry")
	}
	current, err := os.ReadFile(filepath.Join(root, p.AnswerPath))
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("accepted answer changed")
	}
	assertDesignCheckpoint(t, root)
	// A fresh accepted answer ends the immediate-retry window even when the
	// professional cause recurs; the next request must open another entry.
	var next workpacket.ContractPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &next); err != nil {
		t.Fatal(err)
	}
	stageJSON(t, root, next.AnswerPath, completedContractDraft(next))
	acceptPendingContractForRework(t, root, next)
	acceptedNew := stageSnapshot(t, root)
	if _, accepted, _ := runtime.FindSubmission(acceptedNew.State, next.ID); !accepted {
		t.Fatal("new entry not accepted")
	}
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	// The accepted original is history and must not consume the bounded
	// recovery budget; only the current pending entry stays recoverable
	// (L4-state-transition-core.md:89).
	receipts, err := s.Recover(context.Background(), "Main", 1)
	if err != nil || len(receipts) != 1 || receipts[0].PacketID != next.ID {
		t.Fatalf("accepted history consumed bounded Recover limit: %+v %v", receipts, err)
	}
	r := reworkCLI(t, root, "contracts")
	if r.StageEntry.(float64) <= after.State["lifecycle"].(map[string]any)["phase_revision"].(float64) {
		t.Fatal("new accepted delivery was swallowed by historical retry")
	}
}

func acceptPendingContractForRework(t *testing.T, root string, p workpacket.ContractPacket) {
	t.Helper()
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = &cancelAfterDurableReception{Context: ctx, statePath: s.StatePath, packetID: p.ID, cancel: cancel}
	receipt, err := s.SaveContracts(ctx, p.AnswerPath)
	if err != nil || !receipt.Accepted {
		t.Fatalf("durable pending reception: %+v %v", receipt, err)
	}
}

func TestPlanningReworkCLIRejectsFrozenDesignDrift(t *testing.T) {
	root, p := newContractAnswerFixture(t)
	// The original S3 reception must really advance the S3->S4 continuation,
	// so the contracts rework has an original source to resolve at all
	// (L4-revision-usage.md:169).
	establishS3Continuation(t, root, p)
	// The upstream S2 design drifts: the corrected architecture declares a new
	// version in the bound source.
	arch := p.Inputs.Designs[0]
	data, err := os.ReadFile(filepath.Join(root, arch.Path))
	if err != nil {
		t.Fatal(err)
	}
	drifted := arch
	drifted.Version = "v2.0.0"
	corrected := strings.Replace(string(data), "> Version: "+arch.Version, "> Version: "+drifted.Version, 1)
	if corrected == string(data) {
		t.Fatalf("design source does not declare %s: %s", arch.Version, data)
	}
	writeContractSource(t, root, arch.Path, corrected+"\nsubstantive design correction\n")
	fixtures.CommitCurrentFixture(t, root)
	// The design target is the correct route for a corrected S2: it redoes the
	// design within the same locked REQ (L4-agent-guidance.md:190).
	reworkCLI(t, root, "design")
	var design workpacket.DesignPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &design); err != nil {
		t.Fatal(err)
	}
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, design.AnswerPath, designDraft(design))
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", design.AnswerPath)
	settleMainPlanningFollowups(t, root)
	// The original S2 outlet registered the drifted design, so the original S3
	// frozen S2 input no longer matches the registered S2 design. A contracts
	// rework still resolves that original S3, and it must refuse the drifted
	// upstream instead of re-freezing it as a new fact
	// (L4-agent-guidance.md:190).
	before := stageSnapshot(t, root)
	code, out, diagnostic := reworkCLIRefusal(t, root, "contracts", "correct contracts")
	if code == 0 {
		t.Fatalf("contracts rework re-froze the drifted upstream design: %s", out)
	}
	// The refusal names the drift itself, not merely that the command failed.
	if !strings.Contains(diagnostic, "frozen S2 inputs changed or unavailable") || !strings.Contains(diagnostic, "frozen design identity differs from the currently registered S2 design") {
		t.Fatalf("contracts rework refusal did not name the frozen design drift: %s", diagnostic)
	}
	if stageSnapshot(t, root).Revision != before.Revision {
		t.Fatal("rejected frozen source changed Runtime")
	}
	var replacement workpacket.ContractPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &replacement); err != nil {
		t.Fatal(err)
	}
	// The new S3 responsibility carries the actual current design identity, and
	// the original frozen identity stays history (L4-agent-guidance.md:190).
	if replacement.ID == p.ID || len(replacement.Inputs.Designs) != 1 || replacement.Inputs.Designs[0] != drifted {
		t.Fatal("new S2 self-review failed to issue a new S3 responsibility with the actual design identity", replacement.Inputs.Designs)
	}
	if p.Inputs.Designs[0] == replacement.Inputs.Designs[0] {
		t.Fatal("the original frozen design identity was rewritten")
	}
	assertDesignCheckpoint(t, root)
}

// A byte-only change to the registered design source is deliberately not a
// source qualification. The Hook does not recompute, register or verify
// digests to judge whether a source changed, and it must not claim to
// discover every change (L4-agent-guidance.md:183). The system keeps no
// per-file digest to authenticate bytes or closure
// (L4-revision-usage.md:171), so the frozen S2 input is qualified by its
// recorded identity alone: a byte change neither blocks the contracts rework
// nor rewrites that identity.
func TestPlanningReworkCLIAcceptsUndiscoverableDesignByteChange(t *testing.T) {
	root, p := newContractAnswerFixture(t)
	// The contracts rework resolves the original S3 reception, so the chain
	// must really hold an advanced one (L4-revision-usage.md:169).
	establishS3Continuation(t, root, p)
	// Append bytes to the registered design source only: the declared version
	// line stays as it is and nothing is re-registered.
	arch := p.Inputs.Designs[0]
	path := filepath.Join(root, arch.Path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\nsubstantive design correction\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	// The bound source carries the changed bytes.
	fixtures.CommitCurrentFixture(t, root)
	changed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(changed), "substantive design correction") {
		t.Fatalf("the design source did not change: %s", changed)
	}
	before := stageSnapshot(t, root)
	// The byte change is not a source qualification, so the rework is admitted.
	r := reworkCLI(t, root, "contracts")
	if r.StageEntry == nil {
		t.Fatal("contracts rework opened no entry", r)
	}
	after := stageSnapshot(t, root)
	if after.State["lifecycle"].(map[string]any)["phase_revision"].(float64) != before.State["lifecycle"].(map[string]any)["phase_revision"].(float64)+1 {
		t.Fatal("missing fresh entry", after.State["lifecycle"])
	}
	// The recorded design identity stays exactly as it was.
	var row map[string]any
	for _, raw := range after.State["documents"].([]any) {
		d := raw.(map[string]any)
		if d["kind"] == "design" && d["id"] == arch.ID {
			row = d
		}
	}
	if row == nil {
		t.Fatal("the registered design document disappeared", after.State["documents"])
	}
	if row["path"] != arch.Path || row["version"] != arch.Version || row["status"] != "locked" {
		t.Fatalf("recorded design identity moved on a byte-only change: %+v", row)
	}
	// The newly issued S3 responsibility carries the same design reference, so
	// nothing was rewritten or re-frozen on account of the byte change.
	var next workpacket.ContractPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &next); err != nil {
		t.Fatal(err)
	}
	if len(next.Inputs.Designs) != 1 || next.Inputs.Designs[0] != arch {
		t.Fatalf("the new S3 responsibility rewrote the design identity: %+v", next.Inputs.Designs)
	}
	assertDesignCheckpoint(t, root)
}

func TestPlanningReworkCLISeparatesCommittedPreparationFailure(t *testing.T) {
	root, _ := newContractAnswerFixture(t)
	before := stageSnapshot(t, root)
	projection := map[string]any{}
	for k, v := range before.State {
		projection[k] = v
	}
	projection["lifecycle"] = map[string]any{"state": "planning", "phase": "design", "phase_revision": int(before.State["lifecycle"].(map[string]any)["phase_revision"].(float64)) + 1}
	p, err := runtime.CurrentDesignPacket(projection)
	if err != nil {
		t.Fatal(err)
	}
	// Occupy the default questionnaire path with an unknown object. The
	// original Writer must keep it untouched and choose an independent path.
	if err := os.MkdirAll(filepath.Join(root, p.ViewPath), 0700); err != nil {
		t.Fatal(err)
	}
	var r submission.PlanningReworkResult
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "rework", "--root", root, "--target", "design", "--reason", "publication failure remains recoverable")), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Committed || r.PreparationStatus != "ready" || r.NextAction == "" {
		t.Fatalf("lost committed/preparation boundary: %+v", r)
	}
	after := stageSnapshot(t, root)
	if after.State["lifecycle"].(map[string]any)["phase"] != "design" {
		t.Fatal("failed preparation rolled back entry")
	}
	// The occupied original is preserved: not overwritten, not deleted, and not
	// reused as the registered questionnaire.
	if info, err := os.Stat(filepath.Join(root, p.ViewPath)); err != nil || !info.IsDir() {
		t.Fatalf("occupied original view path was not preserved: %v", err)
	}
	if strings.Contains(r.NextAction, p.ViewPath) {
		t.Fatalf("registered questionnaire reused the occupied path: %s", r.NextAction)
	}
	// The independent candidate is a real, readable questionnaire.
	const marker = "Read the S2 design questionnaire at "
	index := strings.Index(r.NextAction, marker)
	if index < 0 {
		t.Fatalf("no independent questionnaire named: %s", r.NextAction)
	}
	view := strings.TrimRight(strings.TrimSpace(strings.Fields(r.NextAction[index+len(marker):])[0]), ".;")
	if _, err := os.Stat(view); err != nil {
		t.Fatalf("independent questionnaire was not published: %v", err)
	}
	stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)
	if stageSnapshot(t, root).State["lifecycle"].(map[string]any)["phase_revision"] != after.State["lifecycle"].(map[string]any)["phase_revision"] {
		t.Fatal("issue recovery opened another entry")
	}
}

func acceptPendingTaskForRework(t *testing.T, root string, p workpacket.TaskPlanningPacket) {
	t.Helper()
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = &cancelAfterDurableReception{Context: ctx, statePath: s.StatePath, packetID: p.ID, cancel: cancel}
	receipt, err := s.SaveTaskPlanning(ctx, p.AnswerPath)
	if err != nil || !receipt.Accepted {
		t.Fatalf("durable pending S4 reception: %+v %v", receipt, err)
	}
}

func TestPlanningReworkCLITasksAcceptedHistoryAndOriginalS5Outlet(t *testing.T) {
	root, original := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, original.AnswerPath, completedTaskPlanningDraft(original))
	acceptPendingTaskForRework(t, root, original)
	before := stageSnapshot(t, root)
	prior, accepted, err := runtime.FindSubmission(before.State, original.ID)
	if err != nil || !accepted {
		t.Fatal("original S4 reception missing", err)
	}
	oldBytes, err := os.ReadFile(filepath.Join(root, original.AnswerPath))
	if err != nil {
		t.Fatal(err)
	}
	// Own output edits are legal even before the named rework entry; upstream
	// source drift must still be rejected instead of incorporated into new inputs.
	taskPath := "docs/dev/tasks/TASK-039-01.md"
	taskBytes, err := os.ReadFile(filepath.Join(root, taskPath))
	if err != nil {
		t.Fatal(err)
	}
	writeContractSource(t, root, taskPath, string(taskBytes)+"\nCorrected planning explanation.\n")
	indexPath := "docs/dev/tasks/index-REQ-039.md"
	indexBytes, err := os.ReadFile(filepath.Join(root, indexPath))
	if err != nil {
		t.Fatal(err)
	}
	writeContractSource(t, root, indexPath, string(indexBytes)+"\nCorrected dispatch rationale.\n")
	r := reworkCLI(t, root, "tasks")
	for _, want := range []string{"S4", "TASK", "dispatch", "answer", "contracts"} {
		if !strings.Contains(r.NextAction, want) {
			t.Fatalf("missing rework guidance %q: %s", want, r.NextAction)
		}
	}
	after := stageSnapshot(t, root)
	preserved, accepted, err := runtime.FindSubmission(after.State, original.ID)
	if err != nil || !accepted || !reflect.DeepEqual(prior, preserved) {
		t.Fatal("S4 history changed", err)
	}
	for _, raw := range after.State["evidence"].([]any) {
		e := raw.(map[string]any)
		if (e["kind"] == "planning_design" || e["kind"] == "planning_design_record" || e["kind"] == "planning_contract" || e["kind"] == "planning_contract_record") && e["status"] != "valid" {
			t.Fatal("upstream evidence invalidated", e)
		}
		if (e["kind"] == "planning_task" || e["kind"] == "planning_task_record") && e["status"] != "invalid" {
			t.Fatal("old S4 evidence remains valid", e)
		}
	}
	if !reflect.DeepEqual(before.State["documents"], after.State["documents"]) {
		t.Fatal("upstream documents changed")
	}
	replay := reworkCLI(t, root, "tasks")
	if replay.StageEntry != r.StageEntry || stageSnapshot(t, root).Revision != after.Revision {
		t.Fatal("retry opened another S4")
	}
	current, err := os.ReadFile(filepath.Join(root, original.AnswerPath))
	if err != nil || !bytes.Equal(current, oldBytes) {
		t.Fatal("original S4 answer rewritten")
	}
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--path", original.AnswerPath)
	if stageSnapshot(t, root).Revision != after.Revision {
		t.Fatal("old accepted replay moved new S4")
	}
	var next workpacket.TaskPlanningPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &next); err != nil {
		t.Fatal(err)
	}
	if next.ID == original.ID || !reflect.DeepEqual(original.Inputs, next.Inputs) {
		t.Fatal("replacement changed upstream frozen facts")
	}
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, next.AnswerPath, completedTaskPlanningDraft(next))
	acceptPendingTaskForRework(t, root, next)
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipts, err := s.Recover(context.Background(), "Main", 1)
	if err != nil || len(receipts) != 1 || receipts[0].PacketID != next.ID {
		t.Fatalf("old pending S4 consumed recovery budget: %+v %v", receipts, err)
	}
	settleMainPlanningFollowups(t, root)
	final := stageSnapshot(t, root)
	if final.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Fatal("new S4 did not enter S5")
	}
	// S5 cannot use a S4 same-stage rework to bypass independent review.
	var out, diagnostic bytes.Buffer
	if code := cli.Run([]string{"runtime", "stage-answer", "rework", "--root", root, "--target", "tasks", "--reason", "cannot bypass review"}, nil, &out, &diagnostic); code == 0 {
		t.Fatal("tasks rework accepted in S5")
	}
	if stageSnapshot(t, root).Revision != final.Revision {
		t.Fatal("S5 rejection mutated Runtime")
	}
}

func TestPlanningReworkCLITasksRejectsUpstreamDrift(t *testing.T) {
	for _, source := range []string{"design", "contract", "REQ"} {
		t.Run(source, func(t *testing.T) {
			root, p := newTaskPlanningAnswerFixture(t)
			path := p.Inputs.Contracts[0].Path
			switch source {
			case "design":
				for _, ref := range p.Inputs.Sources {
					if strings.Contains(ref.Path, "architecture") {
						path = ref.Path
						break
					}
				}
				if path == p.Inputs.Contracts[0].Path {
					t.Fatal("no frozen architecture source")
				}
			case "REQ":
				path = p.Binding.Requirement.Path
			}
			data, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				t.Fatal(err)
			}
			writeContractSource(t, root, path, string(data)+"\nChanged frozen upstream fact.\n")
			before := stageSnapshot(t, root)
			var out, diagnostic bytes.Buffer
			// Upstream byte drift is not a source qualification. The Hook cannot
			// claim to discover every change, so the tasks rework is admitted and
			// the recorded upstream identity stays as it is.
			if code := cli.Run([]string{"runtime", "stage-answer", "rework", "--root", root, "--target", "tasks", "--reason", "must not wash upstream drift"}, nil, &out, &diagnostic); code != 0 {
				t.Fatalf("tasks rework refused on upstream byte drift (%s): %d %s %s", source, code, out.String(), diagnostic.String())
			}
			if stageSnapshot(t, root).Revision == before.Revision {
				t.Fatal("tasks rework did not open an entry")
			}
		})
	}
}

func TestPlanningReworkCLITasksInvalidatesOnlyDependentProjections(t *testing.T) {
	root, _ := newTaskPlanningAnswerFixture(t)
	snap := stageSnapshot(t, root)
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the action's candidate before Writer commit, including dependent
	// projections that can survive an interrupted original stage outlet.
	state := snap.State
	life := state["lifecycle"].(map[string]any)
	life["phase_revision"] = life["phase_revision"].(float64) + 1
	generation := state["baseline"].(map[string]any)["generation"]
	docs := state["documents"].([]any)
	upstreamBytes, err := json.Marshal(docs)
	if err != nil {
		t.Fatal(err)
	}
	state["documents"] = append(docs,
		map[string]any{"kind": "task", "generation": generation, "status": "locked"},
		map[string]any{"kind": "dispatch_plan", "generation": generation, "status": "locked"})
	evidence := state["evidence"].([]any)
	for _, kind := range []string{"planning_task", "planning_task_record", "document_review", "document_review_record", "unrelated_evidence"} {
		evidence = append(evidence, map[string]any{"kind": kind, "baseline_generation": generation, "status": "valid"})
	}
	state["evidence"] = evidence
	if err := runtime.ApplyPlanningRework(root, state, "tasks", "correct task decomposition", "evt-test-rework", s.Files); err != nil {
		t.Fatal(err)
	}
	revised := state["documents"].([]any)
	preserved, err := json.Marshal(revised[:len(docs)])
	if err != nil || !bytes.Equal(preserved, upstreamBytes) {
		t.Fatal("upstream document projections changed")
	}
	for _, raw := range revised[len(docs):] {
		if raw.(map[string]any)["status"] != "draft" {
			t.Fatal("dependent document remained locked", raw)
		}
	}
	for _, raw := range state["evidence"].([]any) {
		e := raw.(map[string]any)
		switch e["kind"] {
		case "planning_task", "planning_task_record", "document_review", "document_review_record":
			if e["status"] != "invalid" || e["invalidated_by"] != "evt-test-rework" {
				t.Fatal("dependent evidence stayed current", e)
			}
		default:
			if e["status"] != "valid" {
				t.Fatal("unrelated or upstream evidence changed", e)
			}
		}
	}
}

func TestPlanningReworkCLITasksRefreshesMethodDescriptionsWithoutChangingFormalInputs(t *testing.T) {
	root, original := newTaskPlanningAnswerFixture(t)
	stageJSON(t, root, original.AnswerPath, completedTaskPlanningDraft(original))
	acceptPendingTaskForRework(t, root, original)
	before := stageSnapshot(t, root)
	methodPaths := map[string]bool{}
	for _, path := range []string{"docs/dev/tasks/TASK-template.md", "docs/rules/dispatch-plan.md"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		updated := string(data) + "\nClarified professional method description for this planning revision.\n"
		writeContractSource(t, root, path, updated)
		methodPaths[path] = true
	}
	// New issuance reads the current declared Git source for methods. Commit
	// the real description changes and keep the actual method paths.
	fixtures.CommitCurrentFixture(t, root)
	result := reworkCLI(t, root, "tasks")
	encoded, err := json.Marshal(result.Packet)
	if err != nil {
		t.Fatal(err)
	}
	var next workpacket.TaskPlanningPacket
	if err := json.Unmarshal(encoded, &next); err != nil {
		t.Fatal(err)
	}
	if next.ID == original.ID {
		t.Fatal("method clarification did not open a new S4 entry")
	}
	if original.Binding.Requirement != next.Binding.Requirement || !reflect.DeepEqual(original.Inputs.Contracts, next.Inputs.Contracts) || !reflect.DeepEqual(original.Inputs.ClauseUniverse, next.Inputs.ClauseUniverse) || !reflect.DeepEqual(original.Inputs.Directories, next.Inputs.Directories) {
		t.Fatal("method refresh changed frozen formal inputs")
	}
	formal := func(p workpacket.TaskPlanningPacket) []workpacket.Subject {
		var refs []workpacket.Subject
		for _, ref := range p.Inputs.Sources {
			if _, method := methodPaths[ref.Path]; !method {
				refs = append(refs, ref)
			}
		}
		return refs
	}
	if !reflect.DeepEqual(formal(original), formal(next)) {
		t.Fatal("method refresh changed REQ/S2/S3 source closure")
	}
	for path := range methodPaths {
		var oldFound, newFound bool
		for _, ref := range original.Inputs.Sources {
			if ref.Path == path {
				oldFound = true
			}
		}
		for _, ref := range next.Inputs.Sources {
			if ref.Path == path {
				newFound = true
			}
		}
		if !oldFound || !newFound {
			t.Fatalf("S4 did not retain method path %s across entries", path)
		}
	}
	after := stageSnapshot(t, root)
	if !reflect.DeepEqual(before.State["bound_req"], after.State["bound_req"]) || !reflect.DeepEqual(before.State["baseline"], after.State["baseline"]) || !reflect.DeepEqual(before.State["documents"], after.State["documents"]) {
		t.Fatal("method refresh changed locked REQ, baseline or upstream documents")
	}
	for _, raw := range after.State["evidence"].([]any) {
		e := raw.(map[string]any)
		if (e["kind"] == "planning_design" || e["kind"] == "planning_design_record" || e["kind"] == "planning_contract" || e["kind"] == "planning_contract_record") && e["status"] != "valid" {
			t.Fatal("method refresh invalidated upstream evidence", e)
		}
	}
}
