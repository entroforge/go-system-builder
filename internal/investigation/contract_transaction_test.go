package investigation_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func approvalTransactionFixture(t *testing.T) (*intakeFixture, investigation.ContractRequest) {
	t.Helper()
	f := newIntakeFixture(t, []string{"finding-1"})
	setContractLifecycle(t, f)
	if _, err := investigation.Ingest(f.root, f.statePath, f.journalPath, investigation.IngestRequest{ExpectedRevision: 0, GroupingRationale: "same fault"}); err != nil {
		t.Fatal(err)
	}
	prepareCaseForContractApproval(t, f)
	draft := writeContractDraft(t, f.root, []string{"finding-1"})
	sha, id, rev := registerContractApprovalEvidence(t, f, draft)
	return f, investigation.ContractRequest{OperationID: "approve-once", ExpectedRevision: rev, CaseID: "investigation-case-observation-batch-r1", ContractPath: draft, ApprovedBy: "main-session", ApprovalHash: sha, ApprovalEvidenceID: id}
}

func approvalCLI(t *testing.T, f *intakeFixture, r investigation.ContractRequest) (int, []byte, string) {
	t.Helper()
	args := []string{"runtime", "investigation", "contract", "approve", "--root", f.root, "--state", f.statePath, "--journal", f.journalPath, "--operation-id", r.OperationID, "--case-id", r.CaseID, "--file", r.ContractPath, "--approved-by", r.ApprovedBy, "--approval-hash", r.ApprovalHash, "--approval-evidence-id", r.ApprovalEvidenceID, "--expected-revision", strconv.Itoa(r.ExpectedRevision)}
	if r.DelegationEvidenceID != "" {
		args = append(args, "--delegation-evidence-id", r.DelegationEvidenceID)
	}
	var out, diagnostic bytes.Buffer
	if binary := os.Getenv("FRAMEWORK_REPAIR_BINARY"); binary != "" {
		cmd := exec.Command(binary, args...)
		cmd.Stdout, cmd.Stderr = &out, &diagnostic
		if err := cmd.Run(); err != nil {
			var exited *exec.ExitError
			if !errors.As(err, &exited) {
				t.Fatal(err)
			}
			return exited.ExitCode(), out.Bytes(), diagnostic.String()
		}
		return 0, out.Bytes(), diagnostic.String()
	}
	code := cli.Run(args, strings.NewReader(""), &out, &diagnostic)
	return code, out.Bytes(), diagnostic.String()
}

func TestContractApprovalOperationCLIReplayAndConflict(t *testing.T) {
	f, r := approvalTransactionFixture(t)
	code, out, diagnostic := approvalCLI(t, f, r)
	if code != 0 {
		t.Fatalf("approve: %d %s %s", code, out, diagnostic)
	}
	var first struct {
		Receipt *runtime.OperationReceipt `json:"operation_receipt"`
	}
	if err := json.Unmarshal(out, &first); err != nil || first.Receipt == nil || len(first.Receipt.Artifacts) != 2 {
		t.Fatalf("receipt: %v %s", err, out)
	}
	before, journal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
	// Replay is anchored to the original approved hash and bytes; the draft
	// authoring file is no longer needed once the durable receipt exists.
	if err := os.Remove(r.ContractPath); err != nil {
		t.Fatal(err)
	}
	code, out, diagnostic = approvalCLI(t, f, r)
	if code != 0 {
		t.Fatalf("replay: %d %s %s", code, out, diagnostic)
	}
	var next struct {
		Receipt  *runtime.OperationReceipt `json:"operation_receipt"`
		Replayed bool                      `json:"operation_replayed"`
	}
	if err := json.Unmarshal(out, &next); err != nil || !next.Replayed {
		t.Fatalf("replay receipt %v %s", err, out)
	}
	a, _ := json.Marshal(first.Receipt)
	b, _ := json.Marshal(next.Receipt)
	if !bytes.Equal(a, b) {
		t.Fatal("retry changed approval receipt")
	}
	for _, mode := range []string{"actor", "contract-hash", "case", "decision", "delegation"} {
		changed := r
		switch mode {
		case "actor":
			changed.ApprovedBy = "someone-else"
		case "contract-hash":
			changed.ApprovalHash = strings.Repeat("a", 64)
		case "case":
			changed.CaseID = "another-case"
		case "decision":
			changed.ApprovalEvidenceID = "another-decision"
		case "delegation":
			changed.DelegationEvidenceID = "another-grant"
		}
		if _, err := investigation.ApproveContract(f.root, f.statePath, f.journalPath, changed); !errors.Is(err, runtime.ErrOperationConflict) {
			t.Fatalf("%s conflict: %v", mode, err)
		}
	}
	if !bytes.Equal(before, mustRead(t, f.statePath)) || !bytes.Equal(journal, mustRead(t, f.journalPath)) {
		t.Fatal("replay/conflict consumed approval twice")
	}
}

func TestContractApprovalConcurrentOneReceiptAndDelegationUse(t *testing.T) {
	x := newDelegationFixture(t, nil, nil)
	x.request.OperationID = "delegated-once"
	before := bytes.Count(mustRead(t, x.f.journalPath), []byte("\n"))
	snapshots := make([]runtime.Snapshot, 4)
	errs := make([]error, 4)
	var group sync.WaitGroup
	for i := range snapshots {
		group.Go(func() {
			snapshots[i], errs[i] = investigation.ApproveContract(x.f.root, x.f.statePath, x.f.journalPath, x.request)
		})
	}
	group.Wait()
	first, _ := json.Marshal(snapshots[0].Operation)
	for i, snap := range snapshots {
		data, _ := json.Marshal(snap.Operation)
		if errs[i] != nil || snap.Operation == nil || !bytes.Equal(first, data) {
			t.Fatalf("caller %d: %v %s", i, errs[i], data)
		}
	}
	if bytes.Count(mustRead(t, x.f.journalPath), []byte("\n")) != before+1 {
		t.Fatal("duplicate approval event")
	}
	state, err := runtime.NewStore(x.f.statePath, x.f.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	uses := state.State["configuration"].(map[string]any)["repair"].(map[string]any)["delegation_uses"].(map[string]any)
	if uses["ev-grant"] != float64(1) {
		t.Fatalf("delegation budget=%v", uses)
	}
}

func TestContractApprovalCASRechecksInputsWithoutPublication(t *testing.T) {
	for _, mode := range []string{"draft", "case", "approval", "batch", "collision"} {
		t.Run(mode, func(t *testing.T) {
			f, r := approvalTransactionFixture(t)
			before, journal := mustRead(t, f.statePath), mustRead(t, f.journalPath)
			var state map[string]any
			_ = json.Unmarshal(before, &state)
			pointer := state["review"].(map[string]any)["investigation"].(map[string]any)
			_, err := investigation.ApproveContractWithTestWriter(f.root, f.statePath, f.journalPath, r, func(store *runtime.Store) *runtime.Store {
				var target string
				switch mode {
				case "draft":
					target = r.ContractPath
				case "case":
					target = filepath.Join(f.root, pointer["path"].(string))
				case "approval":
					target = filepath.Join(f.root, ".claude/decisions/contract-approval.json")
				case "batch":
					target = filepath.Join(f.root, state["review"].(map[string]any)["observation_batch"].(map[string]any)["path"].(string))
				case "collision":
					target = filepath.Join(f.root, ".claude/review/investigation/contracts/repair-contract-intake-r2.json")
				}
				if mode == "collision" { // Derive the exact output name from the actual draft.
					var draft map[string]any
					_ = json.Unmarshal(mustRead(t, r.ContractPath), &draft)
					target = filepath.Join(f.root, ".claude/review/investigation/contracts", draft["repair_contract_id"].(string)+"-r2.json")
				}
				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte("independently changed bytes"), 0644); err != nil {
					t.Fatal(err)
				}
				return store
			})
			if err == nil {
				t.Fatalf("%s accepted", mode)
			}
			if !bytes.Equal(before, mustRead(t, f.statePath)) || !bytes.Equal(journal, mustRead(t, f.journalPath)) {
				t.Fatal("rejected approval changed Runtime")
			}
			cases, _ := filepath.Glob(filepath.Join(f.root, ".claude/review/investigation/cases/*-r2.json"))
			if len(cases) != 0 {
				t.Fatal("published Case on rejection")
			}
			contracts, _ := filepath.Glob(filepath.Join(f.root, ".claude/review/investigation/contracts/*-r2.json"))
			if mode == "collision" {
				if len(contracts) != 1 || string(mustRead(t, contracts[0])) != "independently changed bytes" {
					t.Fatal("collision removed historical file")
				}
			} else if len(contracts) != 0 {
				t.Fatal("published Contract on rejection")
			}
			staged, _ := filepath.Glob(filepath.Join(f.root, ".claude/operations/staging/*/*.data"))
			if len(staged) != 0 {
				t.Fatal("definitive rejection leaked private staging")
			}
		})
	}
}
