package req039_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// The actual S2 evidence consumer reads its registered envelope from disk,
// including the reports directory. This planning_design fixture makes no
// claim about the separate S5 document_review consumer or template.
func TestPlanningDesignEvidenceUsesRegisteredDiskSource(t *testing.T) {
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

	// Registration and consumption use the same original disk artifact;
	// authors do not commit an extra envelope before normal S2 continuation.
	if tracked := strings.TrimSpace(runGitIn(t, root, "ls-files", "--", reviewPath)); tracked != "" {
		t.Fatalf("fixture envelope was unexpectedly committed: %s", tracked)
	}
	_, qg := auditHook(t, root, root, "final-audit-template-untracked")
	if status := stringValueAudit(qg["status"]); status != "satisfied" && status != "advanced" {
		t.Fatalf("registered disk design envelope must satisfy S2, status=%q qg=%v", status, qg)
	}
	if committed, _ := qg["transition_committed"].(bool); !committed {
		t.Fatalf("registered disk envelope must continue its original transition, qg=%v", qg)
	}
	if !strings.Contains(gotEvidenceRefsAudit(qg), "ev-final-audit-template-source") {
		t.Fatalf("S2 did not consume the actually registered envelope: %v", qg)
	}
}

func auditFinalSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
