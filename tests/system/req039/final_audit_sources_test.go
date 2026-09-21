package req039_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestFinalAuditReviewTemplatePathRequiresCommittedGitSource follows the REV
// template's documented docs/reports/review path through the real evidence
// CLI and Hook. A planning_design envelope keeps the fixture at the design
// gate; the source rule is shared by the template's document_review kind. The
// CLI accepts the envelope while it is an untracked docs/reports/review file;
// productionFiles then applies the catalog root git_tree rule, so the same
// Runtime row is invisible to the gate until the envelope is committed. This
// is the concrete evidence/template source mismatch; it does not refresh or
// alter a frozen product subject.
func TestFinalAuditReviewTemplatePathRequiresCommittedGitSource(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "Final audit")
	t.Setenv("GIT_AUTHOR_EMAIL", "final-audit@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Final audit")
	t.Setenv("GIT_COMMITTER_EMAIL", "final-audit@example.invalid")

	root := auditSourceRoot(t)
	state := systemPlanningState(t, root, "design", 1)
	writeSystemState(t, root, state)

	architecturePath := "docs/architecture/ARCHITECTURE-039-review-source.md"
	architecture := []byte("# ARCHITECTURE-039-review-source\n\n> Status: locked\n> Version: v1.0.0\n")
	writeAuditFile(t, root, architecturePath, string(architecture))
	runGitIn(t, root, "add", architecturePath)
	runGitIn(t, root, "commit", "-qm", "authority architecture for template source audit")

	reviewPath := "docs/reports/review/REV-final-audit.json"
	review := map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             "ev-final-audit-template-source",
		"kind":                    "planning_design",
		"runtime_id":              "loop-system-test",
		"baseline_generation":     1,
		"review_round":            1,
		"producer_agent_id":       "architect-final-audit",
		"producer_responsibility": "Architect",
		"subject_refs": []any{map[string]any{
			"path": architecturePath, "version": "v1.0.0", "sha256": auditFinalSHA(architecture),
		}},
		"conclusion": "pass",
		"created_at": "2026-09-20T00:00:00Z",
	}
	reviewBytes, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	writeAuditFile(t, root, reviewPath, string(reviewBytes))

	var stdout, stderr bytes.Buffer
	code := runCLI(t, []string{
		"runtime", "evidence", "add", "--root", root,
		"--id", "ev-final-audit-template-source", "--kind", "planning_design",
		"--path", reviewPath, "--produced-by", "architect-final-audit",
		"--responsibility", "Architect",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("template-path evidence registration failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	// The registered row exists, but the pinned Git view cannot see its
	// untracked artifact. The gate must report an unreadable evidence conflict
	// rather than accidentally accepting the root working tree.
	_, qg := auditHook(t, root, root, "final-audit-template-untracked")
	t.Logf("uncommitted template envelope gate: status=%v conflicts=%v missing=%v", qg["status"], qg["conflicts"], qg["missing"])
	if status := stringValueAudit(qg["status"]); status != "unknown" {
		t.Fatalf("uncommitted template review envelope must make the gate unknown, status=%q qg=%v", status, qg)
	}
	if conflicts := fmt.Sprint(qg["conflicts"]); !strings.Contains(conflicts, "evidence:ev-final-audit-template-source:unreadable") {
		t.Fatalf("uncommitted template envelope must be reported unreadable, conflicts=%v qg=%v", qg["conflicts"], qg)
	}

	runGitIn(t, root, "add", reviewPath)
	runGitIn(t, root, "commit", "-qm", "commit template review envelope")
	_, qg = auditHook(t, root, root, "final-audit-template-committed")
	t.Logf("committed template envelope gate: status=%v transition_committed=%v evidence_refs=%v", qg["status"], qg["transition_committed"], qg["evidence_refs"])
	if status := stringValueAudit(qg["status"]); status != "satisfied" && status != "advanced" {
		t.Fatalf("committed template review envelope must satisfy/advance the gate, status=%q qg=%v", status, qg)
	}
	if committed, _ := qg["transition_committed"].(bool); !committed {
		t.Fatalf("committed template review envelope must commit the automatic transition, qg=%v", qg)
	}
}

func auditFinalSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
