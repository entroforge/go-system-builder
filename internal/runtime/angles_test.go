package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAngleRegistryFileNameRoundTrip(t *testing.T) {
	cases := []struct {
		module string
		want   string
	}{
		{"internal/change/", "internal-change.yaml"},
		{"internal/change", "internal-change.yaml"},
		{"internal/runtime/sub/", "internal-runtime-sub.yaml"},
		{"", "root.yaml"},
		{"/", "root.yaml"},
	}
	for _, c := range cases {
		got := AngleRegistryFileName(c.module)
		if got != c.want {
			t.Errorf("AngleRegistryFileName(%q) = %q; want %q", c.module, got, c.want)
		}
	}
	// Round-trip: file name -> module path
	if got := ModuleFromRegistryFileName("internal-change.yaml"); got != "internal/change" {
		t.Errorf("ModuleFromRegistryFileName(internal-change.yaml) = %q; want %q", got, "internal/change")
	}
}

func TestValidateAngleTarget(t *testing.T) {
	if err := ValidateAngleTarget(""); err == nil {
		t.Fatal("empty target accepted")
	}
	if err := ValidateAngleTarget("   "); err == nil {
		t.Fatal("whitespace-only target accepted")
	}
	for _, bad := range []string{"security", "Performance", "CORRECTNESS", "reliability", "safety"} {
		if err := ValidateAngleTarget(bad); err == nil {
			t.Errorf("blacklisted target %q accepted", bad)
		}
	}
	good := []string{
		"internal/runtime/change.go:CreateChange rejects second active record",
		"loop-state.schema.json#changeRecord additionalProperties:false",
		"runtime CreateChange with mismatched req_sha256 fail-closed",
	}
	for _, g := range good {
		if err := ValidateAngleTarget(g); err != nil {
			t.Errorf("good target %q rejected: %v", g, err)
		}
	}
}

func TestCreateAngleAutoAssignsMonotonicID(t *testing.T) {
	root := t.TempDir()
	req := CreateAngleRequest{
		ModulePath: "internal/change",
		Statement:  "single active change record under concurrent writes",
		Target:     "internal/runtime/change.go:CreateChange second-active rejection",
		DeclaredIn: "REQ-002",
	}
	reg1, a1, err := CreateAngle(root, req)
	if err != nil {
		t.Fatalf("first CreateAngle: %v", err)
	}
	if a1.ID != "ANG-CHANGE-001" {
		t.Errorf("first id = %q; want ANG-CHANGE-001", a1.ID)
	}
	if a1.Status != AngleStatusActive {
		t.Errorf("status = %q; want %s", a1.Status, AngleStatusActive)
	}
	_, a2, err := CreateAngle(root, req)
	if err != nil {
		t.Fatalf("second CreateAngle: %v", err)
	}
	if a2.ID != "ANG-CHANGE-002" {
		t.Errorf("second id = %q; want ANG-CHANGE-002", a2.ID)
	}
	// File written at expected path
	path := AngleRegistryFilePath(root, "internal/change")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("registry file not written: %v", err)
	}
	// Reload round-trip
	reg2, err := LoadRegistry(root, "internal/change")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reg2.Angles) != 2 {
		t.Errorf("after two creates, len(angles) = %d; want 2", len(reg2.Angles))
	}
	if reg1.Module != "internal/change" {
		t.Errorf("registry module = %q; want internal/change", reg1.Module)
	}
}

func TestCreateAngleRejectsBadTarget(t *testing.T) {
	root := t.TempDir()
	_, _, err := CreateAngle(root, CreateAngleRequest{
		ModulePath: "internal/change",
		Statement:  "x",
		Target:     "security",
		DeclaredIn: "REQ-002",
	})
	if err == nil {
		t.Fatal("blacklisted target accepted")
	}
	if !strings.Contains(err.Error(), "generic category") {
		t.Errorf("error message = %q; want contains 'generic category'", err.Error())
	}
}

func TestConfirmAngleUpdatesLastAppliedIn(t *testing.T) {
	root := t.TempDir()
	_, a, err := CreateAngle(root, CreateAngleRequest{
		ModulePath: "internal/change",
		Statement:  "x",
		Target:     "internal/runtime/change.go second-active rejection",
		DeclaredIn: "REQ-002",
	})
	if err != nil {
		t.Fatalf("CreateAngle: %v", err)
	}
	reg, err := ConfirmAngle(root, "internal/change", a.ID, "REQ-003")
	if err != nil {
		t.Fatalf("ConfirmAngle: %v", err)
	}
	got := findAngleByID(reg, a.ID)
	if got.LastAppliedIn != "REQ-003" {
		t.Errorf("LastAppliedIn = %q; want REQ-003", got.LastAppliedIn)
	}
}

func TestConfirmAngleRejectsNonActive(t *testing.T) {
	root := t.TempDir()
	_, a, _ := CreateAngle(root, CreateAngleRequest{
		ModulePath: "internal/change",
		Statement:  "x",
		Target:     "internal/runtime/change.go second-active rejection",
		DeclaredIn: "REQ-002",
	})
	if _, err := RetractAngle(root, "internal/change", a.ID, "REQ-003", "obsolete after rewrite"); err != nil {
		t.Fatalf("RetractAngle: %v", err)
	}
	_, err := ConfirmAngle(root, "internal/change", a.ID, "REQ-003")
	if err == nil {
		t.Fatal("ConfirmAngle on retracted accepted")
	}
}

func TestRetractAngleRequiresReason(t *testing.T) {
	root := t.TempDir()
	_, a, _ := CreateAngle(root, CreateAngleRequest{
		ModulePath: "internal/change",
		Statement:  "x",
		Target:     "internal/runtime/change.go second-active rejection",
		DeclaredIn: "REQ-002",
	})
	if _, err := RetractAngle(root, "internal/change", a.ID, "REQ-003", ""); err == nil {
		t.Fatal("empty retract reason accepted")
	}
	if _, err := RetractAngle(root, "internal/change", a.ID, "REQ-003", "  "); err == nil {
		t.Fatal("whitespace retract reason accepted")
	}
}

func TestRetractAngleAppendsToRefactorHistory(t *testing.T) {
	root := t.TempDir()
	_, a, _ := CreateAngle(root, CreateAngleRequest{
		ModulePath: "internal/change",
		Statement:  "x",
		Target:     "internal/runtime/change.go second-active rejection",
		DeclaredIn: "REQ-002",
	})
	reg, err := RetractAngle(root, "internal/change", a.ID, "REQ-003", "obsolete after rewrite")
	if err != nil {
		t.Fatalf("RetractAngle: %v", err)
	}
	got := findAngleByID(reg, a.ID)
	if got.Status != AngleStatusRetracted {
		t.Errorf("status = %q; want %s", got.Status, AngleStatusRetracted)
	}
	if got.RetractReason != "obsolete after rewrite" {
		t.Errorf("reason = %q; want 'obsolete after rewrite'", got.RetractReason)
	}
	if len(reg.RefactorHistory) != 1 {
		t.Fatalf("refactor_history len = %d; want 1", len(reg.RefactorHistory))
	}
	if reg.RefactorHistory[0].AngleID != a.ID {
		t.Errorf("history[0].AngleID = %q; want %q", reg.RefactorHistory[0].AngleID, a.ID)
	}
}

func TestReviveAngleAllowedOnce(t *testing.T) {
	root := t.TempDir()
	_, a, _ := CreateAngle(root, CreateAngleRequest{
		ModulePath: "internal/change",
		Statement:  "x",
		Target:     "internal/runtime/change.go second-active rejection",
		DeclaredIn: "REQ-002",
	})
	if _, err := RetractAngle(root, "internal/change", a.ID, "REQ-003", "obsolete"); err != nil {
		t.Fatalf("RetractAngle: %v", err)
	}
	// First revive OK
	reg, err := ReviveAngle(root, "internal/change", a.ID, "REQ-004")
	if err != nil {
		t.Fatalf("first revive: %v", err)
	}
	if findAngleByID(reg, a.ID).Status != AngleStatusActive {
		t.Errorf("status after revive = %q; want active", findAngleByID(reg, a.ID).Status)
	}
	if len(reg.ReviveHistory) != 1 {
		t.Errorf("revive_history len = %d; want 1", len(reg.ReviveHistory))
	}
	// Retract again, then try revive again -> fail
	if _, err := RetractAngle(root, "internal/change", a.ID, "REQ-005", "obsolete again"); err != nil {
		t.Fatalf("second retract: %v", err)
	}
	if _, err := ReviveAngle(root, "internal/change", a.ID, "REQ-006"); err == nil {
		t.Fatal("second revive accepted (FR-007 single-revive invariant)")
	}
}

func TestReviveAngleRejectsActiveAngle(t *testing.T) {
	root := t.TempDir()
	_, a, _ := CreateAngle(root, CreateAngleRequest{
		ModulePath: "internal/change",
		Statement:  "x",
		Target:     "internal/runtime/change.go second-active rejection",
		DeclaredIn: "REQ-002",
	})
	if _, err := ReviveAngle(root, "internal/change", a.ID, "REQ-003"); err == nil {
		t.Fatal("revive on active accepted")
	}
}

func TestListBaselineForAggregatesActiveAcrossModules(t *testing.T) {
	root := t.TempDir()
	for _, mp := range []string{"internal/change", "internal/runtime"} {
		_, _, err := CreateAngle(root, CreateAngleRequest{
			ModulePath: mp,
			Statement:  "angle in " + mp,
			Target:     mp + "/file.go:func something specific",
			DeclaredIn: "REQ-002",
		})
		if err != nil {
			t.Fatalf("CreateAngle %s: %v", mp, err)
		}
	}
	// Add a retracted angle that should be filtered out
	_, a2, _ := CreateAngle(root, CreateAngleRequest{
		ModulePath: "internal/change",
		Statement:  "to be retracted",
		Target:     "internal/change/file.go:AnotherFunction specific check",
		DeclaredIn: "REQ-002",
	})
	if _, err := RetractAngle(root, "internal/change", a2.ID, "REQ-003", "obsolete"); err != nil {
		t.Fatalf("RetractAngle: %v", err)
	}
	baseline, err := ListBaselineFor(root, []string{"internal/change", "internal/runtime"})
	if err != nil {
		t.Fatalf("ListBaselineFor: %v", err)
	}
	if len(baseline) != 2 {
		t.Errorf("baseline len = %d; want 2 (retracted excluded)", len(baseline))
	}
	seen := map[string]bool{}
	for _, b := range baseline {
		seen[b.Module] = true
	}
	if !seen["internal/change"] || !seen["internal/runtime"] {
		t.Errorf("baseline missing modules; seen=%v", seen)
	}
}

func TestListBaselineForEmptyModule(t *testing.T) {
	root := t.TempDir()
	// No registry files exist yet
	baseline, err := ListBaselineFor(root, []string{"internal/nope"})
	if err != nil {
		t.Fatalf("ListBaselineFor on missing module: %v", err)
	}
	if len(baseline) != 0 {
		t.Errorf("baseline len = %d; want 0", len(baseline))
	}
}

func TestBumpVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v0.0.0", "v0.0.1"},
		{"v1.2.3", "v1.2.4"},
		{"v0.0.9", "v0.0.10"},
		{"garbage", "v0.0.1"},
		{"", "v0.0.1"},
	}
	for _, c := range cases {
		if got := bumpVersion(c.in); got != c.want {
			t.Errorf("bumpVersion(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestLoadRegistryNonExistentReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	reg, err := LoadRegistry(root, "internal/change")
	if err != nil {
		t.Fatalf("LoadRegistry missing: %v", err)
	}
	if reg == nil {
		t.Fatal("reg is nil")
	}
	if reg.Module != "internal/change" {
		t.Errorf("Module = %q; want internal/change", reg.Module)
	}
	if len(reg.Angles) != 0 {
		t.Errorf("Angles len = %d; want 0", len(reg.Angles))
	}
}

func TestSaveRegistryAtomic(t *testing.T) {
	root := t.TempDir()
	reg := NewModuleRegistry("internal/change")
	reg.Angles = append(reg.Angles, Angle{
		ID:            "ANG-CHANGE-001",
		Statement:     "x",
		Target:        "internal/runtime/change.go:specific invariant",
		DeclaredIn:    "REQ-002",
		LastAppliedIn: "REQ-002",
		Status:        AngleStatusActive,
		DeclaredAt:    time.Now().UTC(),
	})
	if err := SaveRegistry(root, "internal/change", reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	// Temp file should be gone
	if _, err := os.Stat(filepath.Join(root, "docs", "design", "angles", "internal-change.yaml.tmp")); err == nil {
		t.Fatal(".tmp file left behind")
	}
	// Final file should exist
	if _, err := os.Stat(AngleRegistryFilePath(root, "internal/change")); err != nil {
		t.Fatalf("final file missing: %v", err)
	}
}

func TestSaveRegistryRejectsModuleMismatch(t *testing.T) {
	root := t.TempDir()
	reg := NewModuleRegistry("internal/change")
	if err := SaveRegistry(root, "internal/runtime", reg); err == nil {
		t.Fatal("SaveRegistry accepted module mismatch")
	}
}
