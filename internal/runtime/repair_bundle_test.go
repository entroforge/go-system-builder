package runtime_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairBundleCannotBeRelabeledAsS7Pending(t *testing.T) {
	root, sp, jp, pending, artifacts := artifactRecoveryFixture(t, "marker", true)
	pending["schema_version"] = "2.0.0"
	marker := sp + ".commit-pending.json"
	if err := os.WriteFile(marker, mustJSON(t, pending), 0644); err != nil {
		t.Fatal(err)
	}
	beforeState, beforeJournal, beforeMarker := mustRead(t, sp), mustRead(t, jp), mustRead(t, marker)
	if _, err := testWriter(sp, jp).RecoverPendingOperations(); err == nil {
		t.Fatal("Repair bundle accepted under S7-only pending protocol")
	}
	if !bytes.Equal(beforeState, mustRead(t, sp)) || !bytes.Equal(beforeJournal, mustRead(t, jp)) || !bytes.Equal(beforeMarker, mustRead(t, marker)) {
		t.Fatal("rejected version change modified durable inputs")
	}
	for _, a := range artifacts {
		if _, err := os.Stat(filepath.Join(root, a.Path)); !os.IsNotExist(err) {
			t.Fatalf("published on rejected downgrade: %s", a.Path)
		}
	}
}

// Supply a frozen pre-2.1 executable, not another build of the current source.
// This checks a specific deployed predecessor; it is not a universal writer
// fence or permission to mix arbitrary old and new writers after migration.
func TestRepairBundleRejectedByPredecessorBinary(t *testing.T) {
	binary := os.Getenv("FRAMEWORK_REPAIR_PREDECESSOR_BINARY")
	if binary == "" {
		t.Skip("requires frozen predecessor binary")
	}
	root, sp, jp, pending, artifacts := artifactRecoveryFixture(t, "partial_publish", true)
	plan := []byte(`{"schema_version":"1.0.0","review_plan_id":"review-plan-probe","review_round":1,"baseline_generation":1,"frozen_subjects":[{"path":"product.go","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"claims":[{"claim_id":"claim-probe","lens":"qa","target":"product.go","assertion":"error propagation","oracle":"no dropped errors","method":"code review","applicability":"required","source_refs":["REQ-probe"]}],"assignments":[{"assignment_id":"assignment-probe","lens":"qa","claim_ids":["claim-probe"],"non_overlap_boundary":"owns error propagation","execution_wave":"static"}],"e2e_coverage_state":"not_applicable","dispatch_capacity_policy":"coverage_complete","created_by":"test","created_at":"2026-10-04T00:00:00Z"}`)
	planPath := filepath.Join(root, "probe-plan.json")
	if err := os.WriteFile(planPath, plan, 0644); err != nil {
		t.Fatal(err)
	}
	protected := map[string][]byte{}
	for _, p := range []string{sp, jp, sp + ".commit-pending.json", filepath.Join(root, artifacts[0].Path)} {
		protected[p] = mustRead(t, p)
	}
	for _, a := range pending["artifacts"].([]map[string]any) {
		p := filepath.Join(root, a["staging_path"].(string))
		protected[p] = mustRead(t, p)
	}
	command := exec.Command(binary, "runtime", "review-plan", "--root", root, "--state", sp, "--journal", jp, "--file", planPath, "--operation-id", "probe-old-writer")
	out, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "2.1.0") {
		t.Fatalf("predecessor did not explicitly reject new pending version: %v %s", err, out)
	}
	for p, before := range protected {
		if !bytes.Equal(before, mustRead(t, p)) {
			t.Fatalf("old writer modified %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(root, artifacts[1].Path)); !os.IsNotExist(err) {
		t.Fatal("old writer published pending Repair artifact")
	}
	t.Logf("predecessor rejected pending 2.1 with inputs preserved: %s", out)
}
