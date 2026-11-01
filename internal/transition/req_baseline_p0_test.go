// REQ continuation keeps the actual binding without digest authentication.
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
			"path": "docs/requirements/REQ-001.md",
		},
	}
	guard, _ := transition.LookupGuard("req_baseline_unchanged")
	if err := guard(state, nil); err != nil {
		t.Fatalf("matching REQ must pass: %v", err)
	}
}

func TestReqBaselineUnchangedAcceptsCRLF(t *testing.T) {
	root := t.TempDir()
	content := []byte("# REQ\r\n> Status: locked\r\n")
	writeBoundREQFixture(t, root, content)
	state := map[string]any{
		"root": root,
		"bound_req": map[string]any{
			"path": "docs/requirements/REQ-001.md",
		},
	}
	guard, _ := transition.LookupGuard("req_baseline_unchanged")
	if err := guard(state, nil); err != nil {
		t.Fatalf("raw-byte CRLF REQ must pass: %v", err)
	}
}

func TestReqBaselineUnchangedDoesNotAuthenticateEditedFile(t *testing.T) {
	root := t.TempDir()
	writeBoundREQFixture(t, root, []byte("# REQ\n> Status: locked\n"))
	state := map[string]any{
		"root": root,
		"bound_req": map[string]any{
			"path": "docs/requirements/REQ-001.md",
		},
	}
	// A later edit is not authenticated by an obsolete source hash.
	if err := os.WriteFile(filepath.Join(root, "docs", "requirements", "REQ-001.md"),
		[]byte("# REQ\n> Status: locked\n\nNEW GOAL sneaked in\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	guard, _ := transition.LookupGuard("req_baseline_unchanged")
	err := guard(state, map[string]string{"any": "evidence"})
	if err != nil {
		t.Fatalf("the binding should not authenticate old source bytes: %v", err)
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
