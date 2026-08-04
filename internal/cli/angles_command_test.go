package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// writeRuntimeStateWithChange builds an inactive-style state with an
// active Change Record carrying scope.include = scopeFor. The matchReq
// argument is written into the change.req_ref so loadScopeInclude() picks
// up the scope; pass an empty string to omit the field (used by the
// "no active change record" branch).
func writeRuntimeStateWithChange(t *testing.T, root, matchReq string, scopeFor []string) {
	t.Helper()
	state := map[string]any{
		"runtime_id": "loop-test",
		"revision":   7,
		"bound_req": map[string]any{
			"id":     matchReq,
			"path":   "docs/requirements/" + matchReq + ".md",
			"sha256": "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}
	if matchReq != "" {
		state["change"] = map[string]any{
			"id":      "CR-001",
			"req_ref": matchReq,
			"scope": map[string]any{
				"include": stringSliceAny(scopeFor),
				"exclude": []any{},
			},
		}
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func stringSliceAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}
	return out
}

// TestAnglesListAggregatesAcrossScopeIncludeModules is the FR-006 closing
// contract: `loop-harness angles list --baseline-for <req>` aggregates all
// active angles across the modules listed in scope.include.
func TestAnglesListAggregatesAcrossScopeIncludeModules(t *testing.T) {
	root := t.TempDir()
	writeRuntimeStateWithChange(t, root, "REQ-003", []string{"internal/change", "internal/runtime"})

	// Seed two active angles, one per module, via the registry API helpers
	// exposed for tests. We invoke the runtime API directly because the
	// CLI's own `commit` subcommand requires a parent directory layout that
	// is not available in t.TempDir (the registry file path is
	// docs/design/angles/{module}.yaml, which the CLI writes through
	// runtime.SaveRegistry once the parent tree exists).
	for _, mp := range []string{"internal/change", "internal/runtime"} {
		// invoke CLI commit subcommand; this writes the registry file.
		var stdout, stderr bytes.Buffer
		code := cli.Run([]string{
			"angles", "commit",
			"--root", root,
			"--module", mp,
			"--statement", "active in " + mp,
			"--target", mp + "/file.go specific invariant",
			"--declared-in", "REQ-002",
		}, strings.NewReader(""), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("commit %s failed: code=%d stderr=%s", mp, code, stderr.String())
		}
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"angles", "list",
		"--root", root,
		"--baseline-for", "REQ-003",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("angles list failed: code=%d stderr=%s", code, stderr.String())
	}
	var out struct {
		BaselineFor string   `json:"baseline_for"`
		Scope       []string `json:"scope_include"`
		Count       int      `json:"count"`
		Inherited   []struct {
			ID     string `json:"id"`
			Module string `json:"module"`
		} `json:"inherited_angles"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("angles list output is not JSON: %v: %s", err, stdout.String())
	}
	if out.BaselineFor != "REQ-003" {
		t.Errorf("baseline_for = %q; want REQ-003", out.BaselineFor)
	}
	if out.Count != 2 {
		t.Errorf("count = %d; want 2 (one per scope.include module)", out.Count)
	}
	seen := map[string]bool{}
	for _, a := range out.Inherited {
		seen[a.Module] = true
	}
	if !seen["internal/change"] || !seen["internal/runtime"] {
		t.Errorf("expected both modules in inherited_angles; got %v", seen)
	}
}

// TestAnglesListIgnoresRetractedAngles asserts that the FR-006 baseline
// only counts status=active; retracted angles must not appear.
func TestAnglesListIgnoresRetractedAngles(t *testing.T) {
	root := t.TempDir()
	writeRuntimeStateWithChange(t, root, "REQ-003", []string{"internal/change"})

	var stdout, stderr bytes.Buffer
	// Create + retract one angle, leave one active.
	code := cli.Run([]string{
		"angles", "commit", "--root", root,
		"--module", "internal/change",
		"--statement", "first",
		"--target", "internal/change/file.go:first specific invariant",
		"--declared-in", "REQ-002",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("commit first: %d %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{
		"angles", "commit", "--root", root,
		"--module", "internal/change",
		"--statement", "second",
		"--target", "internal/change/file.go:second specific invariant",
		"--declared-in", "REQ-002",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("commit second: %d %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{
		"angles", "retract", "--root", root,
		"--module", "internal/change",
		"--id", "ANG-CHANGE-001",
		"--req", "REQ-003",
		"--reason", "test retract",
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("retract: %d %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	code = cli.Run([]string{
		"angles", "list",
		"--root", root,
		"--baseline-for", "REQ-003",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list: %d %s", code, stderr.String())
	}
	var out struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("list not JSON: %v", err)
	}
	if out.Count != 1 {
		t.Errorf("count = %d; want 1 (retracted filtered)", out.Count)
	}
}

// TestAnglesListEmptyWhenNoChangeRecord ensures the bootstrap path
// (REQ-003 Q-009: no active Change Record yet) yields an empty baseline
// instead of failing.
func TestAnglesListEmptyWhenNoChangeRecord(t *testing.T) {
	root := t.TempDir()
	writeRuntimeStateWithChange(t, root, "", nil) // no change record

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"angles", "list",
		"--root", root,
		"--baseline-for", "REQ-003",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list: %d %s", code, stderr.String())
	}
	var out struct {
		Count        int      `json:"count"`
		ScopeInclude []string `json:"scope_include"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("list not JSON: %v", err)
	}
	if out.Count != 0 {
		t.Errorf("expected empty baseline (no change record), got count=%d", out.Count)
	}
	if len(out.ScopeInclude) != 0 {
		t.Errorf("expected empty scope_include, got %v", out.ScopeInclude)
	}
}

// TestAnglesListEmptyWhenChangeBoundToDifferentReq asserts that scope is
// hidden when the active Change Record is bound to a different REQ than
// the requested baseline-for. Avoids silently aggregating the wrong scope.
func TestAnglesListEmptyWhenChangeBoundToDifferentReq(t *testing.T) {
	root := t.TempDir()
	writeRuntimeStateWithChange(t, root, "REQ-002", []string{"internal/change"})

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"angles", "list",
		"--root", root,
		"--baseline-for", "REQ-003",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list: %d %s", code, stderr.String())
	}
	var out struct {
		Count        int      `json:"count"`
		ScopeInclude []string `json:"scope_include"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("list not JSON: %v", err)
	}
	if out.Count != 0 || len(out.ScopeInclude) != 0 {
		t.Errorf("expected empty baseline, got %+v", out)
	}
}

// TestAnglesPathMappingBetweenCLIAndSchema verifies the closing contract
// that `internal/change/` maps to `internal-change` consistently between
// the CLI subcommands and the registry schema. The mapping rule is
// documented in the angle-registry.schema.json description; the CLI
// delegates to runtime.AngleRegistryFileName which is the same code path
// the schema uses for cross-validation.
func TestAnglesPathMappingBetweenCLIAndSchema(t *testing.T) {
	root := t.TempDir()
	writeRuntimeStateWithChange(t, root, "REQ-003", []string{"internal/change"})

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"angles", "commit", "--root", root,
		"--module", "internal/change/",
		"--statement", "with trailing slash",
		"--target", "internal/change/file.go:trailing-slash invariant specific",
		"--declared-in", "REQ-002",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("commit: %d %s", code, stderr.String())
	}

	expected := filepath.Join(root, "docs", "design", "angles", "internal-change.yaml")
	data, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("registry file at expected mapped path missing: %v", err)
	}
	var reg struct {
		Module string `json:"module"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("registry JSON decode: %v", err)
	}
	if reg.Module != "internal/change" {
		t.Errorf("registry module = %q; want internal/change", reg.Module)
	}
	// Negative direction: no other filename produced.
	if _, err := os.Stat(filepath.Join(root, "docs", "design", "angles", "internal.yaml")); err == nil {
		t.Errorf("unexpected non-mapped filename present: internal.yaml")
	}
}

// TestAnglesCommitRejectsBadTarget asserts the FR-001 generic-category
// blacklist rejects targets like "security". The blacklist is owned by
// runtime.ValidateAngleTarget and the CLI must surface the error
// verbatim.
func TestAnglesCommitRejectsBadTarget(t *testing.T) {
	root := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"angles", "commit", "--root", root,
		"--module", "internal/change",
		"--statement", "x",
		"--target", "security",
		"--declared-in", "REQ-003",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit on blacklisted target; got 0; stderr=%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "generic category") {
		t.Errorf("expected 'generic category' in stderr; got %q", stderr.String())
	}
}

// TestAnglesRetractRefusesEmptyReason asserts the FR-007 closing contract
// that `--reason` is mandatory; a missing or whitespace reason must be
// rejected at the CLI boundary without writing the registry.
func TestAnglesRetractRefusesEmptyReason(t *testing.T) {
	root := t.TempDir()
	// First commit an angle we can target.
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{
		"angles", "commit", "--root", root,
		"--module", "internal/change",
		"--statement", "retract-target",
		"--target", "internal/change/file.go:retract-target specific invariant",
		"--declared-in", "REQ-002",
	}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("setup commit: %d %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	for _, reason := range []string{"", "   "} {
		code := cli.Run([]string{
			"angles", "retract", "--root", root,
			"--module", "internal/change",
			"--id", "ANG-CHANGE-001",
			"--req", "REQ-003",
			"--reason", reason,
		}, strings.NewReader(""), &stdout, &stderr)
		if code == 0 {
			t.Errorf("retract with reason=%q accepted", reason)
		}
		if reason == "" && !strings.Contains(stderr.String(), "required") {
			t.Errorf("expected 'required' in stderr; got %q", stderr.String())
		}
		stdout.Reset()
		stderr.Reset()
	}

	// The registry file must still show the angle as active — a rejected
	// retract must NEVER have side effects.
	regPath := filepath.Join(root, "docs", "design", "angles", "internal-change.yaml")
	data, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status": "active"`) {
		t.Errorf("registry should still show status=active; got %s", data)
	}
}

// TestAnglesReviveRefusesSecondRevive asserts the FR-007 single-revive
// invariant at the CLI level. After retract + revive, a second revive
// call is rejected. (The runtime API also rejects this; the CLI test
// confirms the rejection reaches the user.)
func TestAnglesReviveRefusesSecondRevive(t *testing.T) {
	root := t.TempDir()

	var stdout, stderr bytes.Buffer
	// Setup: commit -> retract -> revive (one allowed round-trip). Any
	// 4th-step call would be the second revive and intentionally belongs
	// OUTSIDE this setup list.
	for _, cmd := range [][]string{
		{
			"angles", "commit", "--root", root,
			"--module", "internal/change",
			"--statement", "revive-target",
			"--target", "internal/change/file.go:revive-target specific invariant",
			"--declared-in", "REQ-002",
		},
		{
			"angles", "retract", "--root", root,
			"--module", "internal/change",
			"--id", "ANG-CHANGE-001",
			"--req", "REQ-003",
			"--reason", "first retract",
		},
		{
			"angles", "revive", "--root", root,
			"--module", "internal/change",
			"--id", "ANG-CHANGE-001",
			"--req", "REQ-003",
		},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := cli.Run(cmd, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("setup cmd %v: code=%d stderr=%s", cmd, code, stderr.String())
		}
	}

	// Act: attempt a second revive. Must fail.
	stdout.Reset()
	stderr.Reset()
	code := cli.Run([]string{
		"angles", "revive", "--root", root,
		"--module", "internal/change",
		"--id", "ANG-CHANGE-001",
		"--req", "REQ-006",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("second revive accepted; stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "already been revived") &&
		!strings.Contains(stderr.String(), "single-revive") &&
		!strings.Contains(stderr.String(), "revived") {
		t.Errorf("expected 'revived' in error; got %q", stderr.String())
	}
}

// TestAnglesAuditMarksOutOfScopeActiveAnglesStale is the FR-008 positive
// case: active angles in modules NOT covered by scope.include that have
// not been applied for the stale window (3 rounds by default) get marked
// status=stale.
func TestAnglesAuditMarksOutOfScopeActiveAnglesStale(t *testing.T) {
	root := t.TempDir()

	// Seed: angle in out-of-scope module with LastAppliedIn = REQ-001,
	// which is 2 rounds before REQ-003 (>= 3 default threshold triggers;
	// gap = 3 - 1 = 2 < 3, so we want gap >= 3 actually to be safe).
	// Using LastAppliedIn = REQ-001 keeps the angle out-of-scope and
	// sufficiently stale relative to REQ-003:
	//   gap = 3 - 1 = 2; threshold default 3 means gap must >= 3.
	// Raise default threshold test by using stale-after=1 so ANY gap
	// counts.
	if code := seedAngleForAudit(t, root, "internal/other", "REQ-001"); code != 0 {
		t.Fatal(code)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"angles", "audit",
		"--root", root,
		"--current-req", "REQ-003",
		"--scope-include", "internal/change",
		"--stale-after", "1",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("audit: code=%d stderr=%s", code, stderr.String())
	}
	var out struct {
		StaleAngles []struct {
			Module string `json:"module"`
			ID     string `json:"id"`
		} `json:"stale_angles"`
		Marked []string `json:"marked"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("audit not JSON: %v: %s", err, stdout.String())
	}
	if len(out.StaleAngles) != 1 {
		t.Fatalf("expected 1 stale angle; got %d: %s", len(out.StaleAngles), stdout.String())
	}
	if out.StaleAngles[0].Module != "internal/other" || out.StaleAngles[0].ID != "ANG-OTHER-001" {
		t.Errorf("unexpected stale entry: %+v", out.StaleAngles[0])
	}
	// Registry file should now show status=stale.
	regPath := filepath.Join(root, "docs", "design", "angles", "internal-other.yaml")
	data, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status": "stale"`) {
		t.Errorf("registry status not flipped to stale; got %s", data)
	}
}

// TestAnglesAuditNeverMarksInScopeAnglesStale is the FR-008 closing-contract
// negative: scope.include angles must NEVER be marked stale by audit,
// even when last_applied_in is far behind the current REQ. The audit
// tool reports them under `protected_angles`.
func TestAnglesAuditNeverMarksInScopeAnglesStale(t *testing.T) {
	root := t.TempDir()

	if code := seedAngleForAudit(t, root, "internal/change", "REQ-001"); code != 0 {
		t.Fatal(code)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"angles", "audit",
		"--root", root,
		"--current-req", "REQ-003",
		"--scope-include", "internal/change",
		"--stale-after", "1",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("audit: code=%d stderr=%s", code, stderr.String())
	}
	var out struct {
		StaleAngles []struct {
			ID string `json:"id"`
		} `json:"stale_angles"`
		ProtectedAngles []struct {
			Module string `json:"module"`
			ID     string `json:"id"`
		} `json:"protected_angles"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("audit not JSON: %v", err)
	}
	if len(out.StaleAngles) != 0 {
		t.Errorf("expected 0 stale (scope angle must never be marked stale); got %d", len(out.StaleAngles))
	}
	if len(out.ProtectedAngles) != 1 || out.ProtectedAngles[0].ID != "ANG-CHANGE-001" {
		t.Errorf("expected 1 protected angle entry; got %+v", out.ProtectedAngles)
	}
	// Registry MUST NOT have flipped.
	regPath := filepath.Join(root, "docs", "design", "angles", "internal-change.yaml")
	data, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status": "active"`) {
		t.Errorf("scope-included angle flipped to stale; registry=%s", data)
	}
}

// TestAnglesAuditDoesNotMarkRecentAngleStale verifies a fresh angle
// (LastAppliedIn close to current REQ) is not marked stale — guards the
// gap threshold direction.
func TestAnglesAuditDoesNotMarkRecentAngleStale(t *testing.T) {
	root := t.TempDir()

	// Out of scope, but last applied only 1 round ago with threshold 2:
	// gap = 3 - 2 = 1 < 2 -> keep active.
	if code := seedAngleForAudit(t, root, "internal/other", "REQ-002"); code != 0 {
		t.Fatal(code)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"angles", "audit",
		"--root", root,
		"--current-req", "REQ-003",
		"--scope-include", "internal/change",
		"--stale-after", "2",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("audit: %d %s", code, stderr.String())
	}
	var out struct {
		StaleAngles []any `json:"stale_angles"`
	}
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if len(out.StaleAngles) != 0 {
		t.Errorf("expected 0 stale (recent angle); got %d", len(out.StaleAngles))
	}
}

// TestAnglesCommitRefusesEmptyAndWhitespaceFields asserts the CLI's
// argument validation rejects empty / whitespace-only required fields
// before reaching the runtime API. Empty --statement / --target flow
// through runtime.ValidateAngleTarget; we mirror its semantics with the
// CLI-side guard.
func TestAnglesCommitRefusesEmptyAndWhitespaceFields(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		name    string
		target  string
		wantMsg string
	}{
		{"empty target", "", "is required"},
		{"whitespace target", "   ", "is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run([]string{
				"angles", "commit", "--root", root,
				"--module", "internal/change",
				"--statement", "x",
				"--target", tc.target,
				"--declared-in", "REQ-002",
			}, strings.NewReader(""), &stdout, &stderr)
			if code == 0 {
				t.Fatalf("expected non-zero exit; got 0")
			}
			if !strings.Contains(stderr.String(), tc.wantMsg) {
				t.Errorf("stderr = %q; want contains %q", stderr.String(), tc.wantMsg)
			}
		})
	}
}

// seedAngleForAudit writes a single registry YAML containing one active
// angle in module `modulePath` with last_applied_in = declaredIn. It uses
// the JSON format the runtime SaveRegistry writes (the .yaml extension is
// purely cosmetic; internal format is JSON per REQ-003 §10 / runtime
// source comments).
func seedAngleForAudit(t *testing.T, root, modulePath, declaredIn string) int {
	t.Helper()
	// Build the bare registry document directly. Using the runtime API
	// requires CreateAngle, but we want fine-grained control of
	// LastAppliedIn (CreateAngle sets it equal to DeclaredIn and we want
	// to override it).
	suffix := "OTHER"
	if parts := strings.Split(strings.Trim(modulePath, "/"), "/"); len(parts) > 0 {
		suffix = strings.ToUpper(strings.ReplaceAll(parts[len(parts)-1], "-", ""))
	}
	id := "ANG-" + suffix + "-001"
	body := map[string]any{
		"module":  modulePath,
		"version": "v0.0.1",
		"angles": []map[string]any{
			{
				"id":              id,
				"statement":       "seeded for audit test",
				"target":          modulePath + "/file.go seeded specific invariant",
				"declared_in":     declaredIn,
				"last_applied_in": declaredIn,
				"status":          "active",
				"declared_at":     "2026-01-01T00:00:00Z",
			},
		},
		"refactor_history": []any{},
		"revive_history":   []any{},
	}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "docs", "design", "angles", strings.ReplaceAll(strings.Trim(modulePath, "/"), "/", "-")+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return 0
}
