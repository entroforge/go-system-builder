// req_baseline_p0_test.go pins the L3-S6 P0-4 real body of the
// req_baseline_unchanged guard: TR-004/TR-007 must compare the bound REQ's
// registered sha256 against the on-disk file, not accept any non-empty
// evidence map.
package transition_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/transition"
)

func writeBoundREQFixture(t *testing.T, root string, content []byte) string {
	t.Helper()
	path := filepath.Join(root, "docs", "requirements", "REQ-001.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReqBaselineUnchangedAcceptsMatchingFile(t *testing.T) {
	root := t.TempDir()
	content := []byte("# REQ\n> Status: locked\n")
	writeBoundREQFixture(t, root, content)
	state := map[string]any{
		"root": root,
		"bound_req": map[string]any{
			"path":   "docs/requirements/REQ-001.md",
			"sha256": transition.SHA256(content),
		},
	}
	guard, _ := transition.LookupGuard("req_baseline_unchanged")
	if err := guard(state, nil); err != nil {
		t.Fatalf("matching REQ must pass: %v", err)
	}
}

func TestReqBaselineUnchangedRejectsTamperedFile(t *testing.T) {
	root := t.TempDir()
	writeBoundREQFixture(t, root, []byte("# REQ\n> Status: locked\n"))
	state := map[string]any{
		"root": root,
		"bound_req": map[string]any{
			"path":   "docs/requirements/REQ-001.md",
			"sha256": transition.SHA256([]byte("# REQ\n> Status: locked\n")),
		},
	}
	// The REQ is reworked after bind — the fingerprint no longer matches.
	if err := os.WriteFile(filepath.Join(root, "docs", "requirements", "REQ-001.md"),
		[]byte("# REQ\n> Status: locked\n\nNEW GOAL sneaked in\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	guard, _ := transition.LookupGuard("req_baseline_unchanged")
	err := guard(state, map[string]string{"any": "evidence"})
	if err == nil {
		t.Fatal("tampered REQ must be rejected even with evidence present")
	}
}

func TestReqBaselineUnchangedRejectsMissingBinding(t *testing.T) {
	state := map[string]any{"root": t.TempDir()}
	guard, _ := transition.LookupGuard("req_baseline_unchanged")
	if err := guard(state, nil); err == nil {
		t.Fatal("missing bound REQ must be rejected")
	}
}

func TestReqBaselineUnchangedEnforcementIsSemantic(t *testing.T) {
	registration, ok := transition.LookupGuardRegistration("req_baseline_unchanged")
	if !ok {
		t.Fatal("guard must be registered")
	}
	if registration.Enforcement != transition.GuardSemanticCheck {
		t.Fatalf("enforcement = %q, want semantic_check", registration.Enforcement)
	}
}
