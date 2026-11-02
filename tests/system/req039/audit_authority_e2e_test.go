package req039_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every Runtime writer must enforce the persisted authority binding.
func TestAuditCopiedRuntimeCannotRecordEvidence(t *testing.T) {
	root := freshRoot(t)
	writeSystemState(t, root, systemPlanningState(t, root, "design", 0))
	worker := filepath.Join(t.TempDir(), "worker")
	runGitIn(t, root, "worktree", "add", "-b", "audit-worker", worker, "HEAD")
	statePath := filepath.Join(root, ".claude/loop-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(worker, ".claude/evidence"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worker, ".claude/loop-state.json"), before, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worker, ".claude/evidence/audit.md"), []byte("audit evidence\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := runCLI(t, []string{"runtime", "evidence", "add", "--root", worker, "--id", "audit-copy", "--kind", "planning_design", "--path", ".claude/evidence/audit.md", "--produced-by", "main"}, strings.NewReader(""), &out, &stderr)
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("authority runtime unexpectedly changed")
	}
	workerAfter, err := os.ReadFile(filepath.Join(worker, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 || !bytes.Equal(before, workerAfter) {
		t.Fatalf("copied Runtime became an independent writer: code=%d worker_changed=%t authority_unchanged=true stdout=%s stderr=%s", code, !bytes.Equal(before, workerAfter), out.String(), stderr.String())
	}
}

// TestRuntimeAuthorityProtectsRecoveryAndStorageCoordinates exercises the
// persisted authority binding of the current Runtime writer.
//
// The `runtime fingerprint` verb and the `.fingerprint-pending.json` marker
// were removed with the hash-proof machinery: the blueprint forbids renaming
// the same hash purpose as "fingerprint" and requires the Hook-side writer,
// transaction recovery and successors to compute, register or validate no
// hash at all (L4-workflow-convergence.md:138, L4-runtime-control-plane.md:84).
// The guard itself is unchanged and is still enforced by the current writer
// verb, so the scenarios below drive `runtime reconcile` and the current
// `.commit-pending.json` marker.
func TestRuntimeAuthorityProtectsRecoveryAndStorageCoordinates(t *testing.T) {
	for _, scenario := range []string{"writer-root", "reconcile", "pending-recovery", "alternate-state", "alternate-journal", "state-symlink"} {
		t.Run(scenario, func(t *testing.T) {
			root := freshRoot(t)
			writeSystemState(t, root, systemPlanningState(t, root, "design", 0))
			worker := filepath.Join(t.TempDir(), "worker")
			runGitIn(t, root, "worktree", "add", "-b", "audit-worker", worker, "HEAD")
			statePath := filepath.Join(root, ".claude/loop-state.json")
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			workerState := filepath.Join(worker, ".claude/loop-state.json")
			if err := os.MkdirAll(filepath.Dir(workerState), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(workerState, before, 0644); err != nil {
				t.Fatal(err)
			}
			args := []string{"runtime", "reconcile", "--root", worker}
			switch scenario {
			case "reconcile":
				args[1] = "reconcile"
			case "pending-recovery":
				// Identity must be checked before replay/cleanup, even before
				// decoding this deliberately invalid marker.
				if err := os.WriteFile(workerState+".commit-pending.json", []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
			case "alternate-state":
				args = []string{"runtime", "reconcile", "--root", root, "--state", workerState}
			case "alternate-journal":
				args = []string{"runtime", "reconcile", "--root", root, "--journal", filepath.Join(worker, ".claude/loop-events.jsonl")}
			case "state-symlink":
				alias := filepath.Join(root, ".claude/state-alias.json")
				if err := os.Symlink(statePath, alias); err != nil {
					t.Fatal(err)
				}
				args = []string{"runtime", "reconcile", "--root", root, "--state", alias}
			}
			var out, stderr bytes.Buffer
			code := runCLI(t, args, strings.NewReader(""), &out, &stderr)
			if code == 0 || !strings.Contains(stderr.String(), "runtime writer is outside the bound authority") {
				t.Fatalf("expected authority rejection, code=%d stderr=%s stdout=%s", code, stderr.String(), out.String())
			}
			for _, path := range []string{statePath, workerState} {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("state changed at %s: %v", path, err)
				}
			}
			if scenario == "pending-recovery" {
				marker, err := os.ReadFile(workerState + ".commit-pending.json")
				if err != nil || string(marker) != "{}" {
					t.Fatalf("copied marker was changed: %v", err)
				}
			}
		})
	}
}

func TestRuntimeAuthorityAllowsProjectDirectoryAlias(t *testing.T) {
	root := freshRoot(t)
	writeSystemState(t, root, systemPlanningState(t, root, "design", 0))
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude/evidence"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude/evidence/audit.md"), []byte("evidence\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := runCLI(t, []string{"runtime", "evidence", "add", "--root", alias, "--id", "audit-alias", "--kind", "planning_design", "--path", ".claude/evidence/audit.md", "--produced-by", "main"}, strings.NewReader(""), &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), `"recorded":true`) {
		t.Fatalf("directory alias failed: %s %s", out.String(), stderr.String())
	}
}
