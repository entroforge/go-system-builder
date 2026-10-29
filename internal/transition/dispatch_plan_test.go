package transition

import (
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func dispatchTransitionFixture(t *testing.T) (string, map[string]any, *ActionContext) {
	t.Helper()
	root := t.TempDir()
	source := "../../docs/examples/dispatch-plan/project"
	err := filepath.WalkDir(source, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, p)
		out := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0755)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(out, b, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"baseline": map[string]any{"generation": 1}, "bound_req": map[string]any{"id": "REQ-042"}, "documents": []any{}}
	return root, state, &ActionContext{Root: root, OccurredAt: time.Now(), Request: &Request{Actor: "planner", Files: fileview.Disk{Root: root}}}
}
func TestDispatchRegistrationAndFreezeDrift(t *testing.T) {
	root, state, ctx := dispatchTransitionFixture(t)
	if _, err := actionRegisterPlanningTasks(state, ctx); err != nil {
		t.Fatal(err)
	}
	docs := state["documents"].([]any)
	if len(docs) != 7 {
		t.Fatal(docs)
	}
	if err := dispatchPlanDocuments(state, ctx, false); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "docs/dev/tasks/index-REQ-042.md")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, append(b, []byte("\nchanged after review\n")...), 0644)
	if err := dispatchPlanDocuments(state, ctx, false); err == nil {
		t.Fatal("freeze laundered changed plan")
	}
	if _, err := actionRegisterPlanningTasks(state, ctx); err != nil {
		t.Fatal(err)
	}
	if len(state["documents"].([]any)) != 7 {
		t.Fatal("re-registration duplicated subjects")
	}
	if err := dispatchPlanDocuments(state, ctx, false); err != nil {
		t.Fatal(err)
	}
	os.Remove(p)
	if err := dispatchPlanDocuments(state, ctx, false); err == nil {
		t.Fatal("removed plan accepted")
	}
}
func TestDispatchFreezeDoesNotRefreshTaskHash(t *testing.T) {
	root, state, ctx := dispatchTransitionFixture(t)
	if _, e := actionRegisterPlanningTasks(state, ctx); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "docs/dev/tasks/TASK-042-01.md")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.ReplaceAll(string(b), "共享库", "new goal")), 0644)
	if e := dispatchPlanDocuments(state, ctx, false); e == nil {
		t.Fatal("TASK change accepted")
	}
}

func TestDispatchFreezeProjectsExistingArtifactLocks(t *testing.T) {
	_, state, ctx := dispatchTransitionFixture(t)
	if _, e := actionRegisterPlanningTasks(state, ctx); e != nil {
		t.Fatal(e)
	}
	ctx.Evidence = map[string]string{"execution_batch": "reviewed"}
	if _, e := actionRegisterExecutionBatch(state, ctx); e != nil {
		t.Fatal(e)
	}
	for _, raw := range state["documents"].([]any) {
		d := raw.(map[string]any)
		if d["status"] != "locked" {
			t.Fatalf("not frozen: %v", d)
		}
	}
}

func TestPacketTR002RegistrationRequiresCurrentS4Followup(t *testing.T) {
	root := t.TempDir()
	state := map[string]any{
		"schema_version": "1.2.0",
		"configuration":  map[string]any{"optimization": map[string]any{"packet": true}},
		"baseline":       map[string]any{"generation": 1},
		"documents":      []any{},
	}
	ctx := &ActionContext{
		Root: root, Spec: TransitionSpec{ID: "TR-002"},
		Request: &Request{Actor: "hook_controller", Files: fileview.Disk{Root: root}},
	}
	for _, action := range []ActionFn{actionRegisterLockedContracts, actionRegisterPlanningTasks} {
		if _, err := action(state, ctx); err == nil || !strings.Contains(err.Error(), "current accepted S4 planning_task followup") {
			t.Fatalf("packet-mode TR-002 action accepted a missing S4 followup: %v", err)
		}
	}
	if len(state["documents"].([]any)) != 0 {
		t.Fatalf("rejected TR-002 registration mutated documents: %#v", state["documents"])
	}
}

func TestPacketS3TransitionConsumesAcceptedFacts(t *testing.T) {
	fixture := func(t *testing.T) (map[string]any, *ActionContext) {
		root := t.TempDir()
		index, conclusion := "docs/dev/contracts/CONTRACTS-001.md", "pass"
		answer := workpacket.ContractAnswer{Conclusion: &conclusion, IndexPath: &index, ContractPaths: []string{"docs/dev/contracts/BE-001.md"}}
		digest, _ := workpacket.ContractAnswerDigest(answer)
		key := workpacket.Digest([]byte("submission:packet-s3"))
		followup := workpacket.Digest([]byte(key + ":after-domain"))
		envelope := map[string]any{"contract_answer": answer, "subject_refs": []workpacket.Subject{
			{Path: index, Version: "v1", SHA256: strings.Repeat("a", 64)},
			{Path: "docs/dev/contracts/BE-001.md", Version: "v2", SHA256: strings.Repeat("b", 64)},
			{Path: "docs/architecture/upstream.json", SHA256: strings.Repeat("c", 64)},
		}}
		data, _ := json.Marshal(envelope)
		path := filepath.Join(root, "accepted.json")
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		state := map[string]any{
			"schema_version": "1.2.0", "configuration": map[string]any{"optimization": map[string]any{"packet": true}},
			"runtime_id": "loop-REQ-001", "baseline": map[string]any{"generation": 1},
			"bound_req": map[string]any{"id": "REQ-001", "version": "v1", "sha256": "req-sha"},
			"lifecycle": map[string]any{"state": "planning", "phase": "contracts", "phase_revision": 2},
			"entities": map[string]any{
				"work_packets": []any{map[string]any{"packet_id": "packet-s3", "template_id": "s3-contract-v1", "binding": map[string]any{"runtime_id": "loop-REQ-001", "generation": 1, "stage": "S3", "stage_entry": 2, "owner": "Main", "requirement": map[string]any{"id": "REQ-001", "version": "v1", "sha256": "req-sha"}}}},
				"submissions":  []any{map[string]any{"submission_key": key, "packet_id": "packet-s3", "owner": "Main", "content_sha256": digest, "evidence_id": "evidence-s3", "followup_event_id": followup, "control_status": "pending", "origin_state": "planning", "origin_phase": "contracts", "origin_entry": 2}},
			},
			"evidence": []any{map[string]any{"id": "evidence-s3", "path": path, "sha256": SHA256(data)}},
		}
		ctx := &ActionContext{Root: root, Spec: TransitionSpec{ID: "PTR-PLAN-02"}, Request: &Request{Actor: "hook_controller", SubmissionFollowupID: followup, Files: fileview.Disk{Root: root}}}
		return state, ctx
	}
	t.Run("accepted facts need no professional rescan", func(t *testing.T) {
		state, ctx := fixture(t)
		// There are no contract/scenario professional files: those were checked by
		// the original reception; these post guards must only read its facts.
		for _, guard := range []GuardFn{guardContractsCheckedFn, guardScenarioBridgeCheckedFn} {
			if err := guard(state, nil); err != nil {
				t.Fatal(err)
			}
		}
		batch, err := acceptedContractArtifacts(state, ctx)
		if err != nil {
			t.Fatal(err)
		}
		refs := batch.contracts
		if len(refs) != 2 || refs[0].ID != "CONTRACTS-001" || refs[1].ID != "BE-001" || refs[1].Version != "v2" {
			t.Fatalf("wrong received set: %+v", refs)
		}
	})
	for _, change := range []string{"missing receipt", "stale entry", "foreign requirement", "processed", "superseded", "raw transition"} {
		t.Run(change, func(t *testing.T) {
			state, ctx := fixture(t)
			entities := state["entities"].(map[string]any)
			switch change {
			case "missing receipt":
				entities["submissions"] = []any{}
			case "stale entry":
				state["lifecycle"].(map[string]any)["phase_revision"] = 3
			case "foreign requirement":
				state["bound_req"].(map[string]any)["id"] = "REQ-OTHER"
			case "processed":
				entities["submissions"].([]any)[0].(map[string]any)["control_status"] = "transition_committed"
			case "superseded":
				entities["submissions"].([]any)[0].(map[string]any)["superseded_by"] = "later-packet"
			case "raw transition":
				ctx.Request.SubmissionFollowupID = ""
				if _, err := actionRegisterLockedContracts(state, ctx); err == nil {
					t.Fatal("raw transition accepted")
				}
				return
			}
			if err := guardContractsCheckedFn(state, nil); err == nil {
				t.Fatal("unreceived/current responsibility accepted")
			}
		})
	}
	t.Run("changed answer receipt", func(t *testing.T) {
		state, ctx := fixture(t)
		state["entities"].(map[string]any)["submissions"].([]any)[0].(map[string]any)["content_sha256"] = "different"
		if _, err := acceptedContractArtifacts(state, ctx); err == nil {
			t.Fatal("changed answer accepted")
		}
	})
}
