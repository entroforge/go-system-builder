package req039_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// canonicalBuilder describes one current canonical Assignment whose Result was
// received through the native consumers and is ready for Main's integration.
type canonicalBuilder struct {
	root   string
	record assignmentrecord.Record
	task   string
	worker string
	branch string
	output string
}

// deliverCanonicalBuilder registers a canonical Assignment through the
// production S6 preparation and delivers its Result through the native
// SubagentStart/SubagentStop consumers. The worker checkout stays in place for
// Main's integration.
//
// The legacy Assignment shape — a hand-written team manifest without a
// canonical Record projection — is no longer something the Runtime produces.
// The blueprint deletes the frozen/legacy wiring instead of adding a
// compatibility layer (blueprint/l4-mechanisms/L4-workflow-convergence.md:293),
// and the current integration ACK requires the canonical Record projection
// (internal/cli/integration_ack.go:126-128). A fixture that re-creates the
// legacy shape would therefore describe a wiring the product no longer has.
func deliverCanonicalBuilder(t *testing.T, root, task, dir string) canonicalBuilder {
	t.Helper()
	runRecheckCLI(t, root, "s6", "prepare", "--task", task, "--role", "backend-builder")
	record, found, err := assignmentrecord.ForOwner(req039fixtures.ReadState(t, root), "native-"+task)
	if err != nil || !found {
		t.Fatalf("prepare %s: found=%t err=%v", task, found, err)
	}
	worker := filepath.Join(t.TempDir(), "worker")
	branch := "native/" + task
	runGitIn(t, root, "worktree", "add", "-b", branch, worker, "HEAD")
	brief, plan, result := assignmentrecord.NativePaths(record)
	code, out, errout := systemNativeEvent(t, root, worker, "SubagentStart", filepath.Join(root, brief))
	if code != 0 {
		t.Fatalf("Start %s: %d %s %s", task, code, out, errout)
	}
	assertion, oracle := "Deliver the reviewed output.", "Run the TASK check."
	output := dir + "/result.txt"
	writeCanonicalJSON(t, worker, plan, map[string]any{
		"objective":        "Deliver the reviewed task.",
		"planned_paths":    []string{output},
		"assertion_checks": []any{map[string]any{"assertion": assertion, "oracle": oracle, "done_when_indexes": []int{0}}},
		"risks_blockers":   []string{},
	})
	writeAuditFile(t, worker, output, "delivered\n")
	runGitIn(t, worker, "add", output)
	runGitIn(t, worker, "commit", "-m", "deliver "+task)
	cmd := exec.Command("go", "test", "./"+dir)
	cmd.Dir = worker
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Worker check %s: %v %s", task, err, out)
	}
	writeCanonicalJSON(t, worker, result, map[string]any{
		"summary": "Implemented and checked the output.",
		"assertion_evidence": []any{map[string]any{
			"assertion": assertion, "oracle": oracle, "evidence_refs": []string{output},
		}},
		"risks_blockers":   []string{},
		"scope_deviations": []string{},
		"closing_assessment": map[string]any{
			"disposition": "complete",
			"conditions": []any{map[string]any{
				"done_when_index": 0, "judgment": "satisfied",
				"plan_assertion_indexes": []int{0}, "rationale": "TASK check passes.",
			}},
		},
	})
	code, out, errout = systemNativeEvent(t, root, worker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(worker, result))
	if code != 0 {
		t.Fatalf("Stop %s: %d %s %s", task, code, out, errout)
	}
	if ref := completionTaskRef(t, req039fixtures.ReadState(t, root), task); ref == "" {
		t.Fatalf("%s result not received", task)
	}
	// The native worker authored its plan and Result under its own
	// .claude/assignments/native/... paths inside the checkout. The Runtime
	// now holds the authoritative received copies, and Main's cleanup
	// contract removes a checkout only when no untracked, ignored or modified
	// material is left in it
	// (internal/integration/integrate_responsibility.go:376). Hand the clean
	// checkout back so the current contract is exercised rather than the
	// retired legacy fixture shape.
	if err := os.RemoveAll(filepath.Join(worker, ".claude")); err != nil {
		t.Fatal(err)
	}
	return canonicalBuilder{root: root, record: record, task: task, worker: worker, branch: branch, output: output}
}

// writeCanonicalJSON writes one machine-owned document into the worker
// checkout at the exact path the prepared brief owns.
func writeCanonicalJSON(t *testing.T, worker, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeAuditFile(t, worker, path, string(raw))
}

// canonicalBuilderTask / canonicalBuilderDir select the reviewed TASK and the
// directory its owned check covers.
const (
	canonicalBuilderTask = "TASK-042-01"
	canonicalBuilderDir  = "packages/validation"
)

// seedCanonicalIntegrableAssignment prepares a current canonical Assignment
// whose Result was received through the native consumers, and returns the
// repository root together with the delivered Assignment.
//
// It replaces the former legacy-Assignment seed (a hand-written team manifest
// without a canonical Record projection). That shape is no longer produced by
// the Runtime: the blueprint deletes the frozen/legacy wiring instead of
// adding a compatibility layer
// (blueprint/l4-mechanisms/L4-workflow-convergence.md:293), and the current
// integration ACK requires the canonical Record projection
// (internal/cli/integration_ack.go:126-128).
func seedCanonicalIntegrableAssignment(t *testing.T) (string, canonicalBuilder) {
	t.Helper()
	root := canonicalBuilderRoot(t)
	builder := deliverCanonicalBuilder(t, root, canonicalBuilderTask, canonicalBuilderDir)
	return root, builder
}

// canonicalBuilderCheckpoint returns the durable integration checkpoint of a
// delivered canonical Assignment.
func canonicalBuilderCheckpoint(t *testing.T, builder canonicalBuilder) string {
	t.Helper()
	path := assignmentrecord.IntegrationCheckpointPath(builder.root, builder.record.Reference())
	if path == "" {
		t.Fatal("canonical Assignment has no integration checkpoint slot")
	}
	return path
}

// canonicalBuilderInScopeFile returns a file inside the delivered Assignment's
// registered write scope, so a divergence is an in-scope conflict.
func canonicalBuilderInScopeFile(t *testing.T, builder canonicalBuilder) string {
	t.Helper()
	return filepath.Join(builder.root, builder.output)
}

// canonicalTargetBranch reads the authority target branch the current Runtime
// binds for integration.
func canonicalTargetBranch(t *testing.T, root string) string {
	t.Helper()
	state := req039fixtures.ReadState(t, root)
	bound, _ := state["bound_req"].(map[string]any)
	workspace, _ := bound["workspace"].(map[string]any)
	branch, _ := workspace["dev_branch"].(string)
	if strings.TrimSpace(branch) == "" {
		t.Fatal("Runtime does not bind a development branch")
	}
	return branch
}

// canonicalBuilderRoot builds the repository the current S6 preparation needs:
// the real dispatch plan, the builder role resources and the contract the
// reviewed TASK closes against.
func canonicalBuilderRoot(t *testing.T) string {
	t.Helper()
	return nativeMultiBuilderRoot(t)
}
