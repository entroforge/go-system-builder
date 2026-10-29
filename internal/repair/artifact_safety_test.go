package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func immutablePlanReportFixture() map[string]any {
	return map[string]any{
		"schema_version": "1.0.0", "record_type": "repair_plan_report", "report_id": "repair-plan-report-safety",
		"session_id": "repair-session-safety", "plan_id": "repair-plan-safety", "assignment_id": "repair-assignment-safety",
		"assertion_ids": []string{"symptom-1"}, "agent_id": "builder-safety", "plan": "Restore the approved invariant.",
		"red_checks":     []any{map[string]any{"name": "before", "command": "probe", "result": "fail", "evidence_refs": []string{"test://before"}}},
		"proposed_paths": []string{"internal/api.go"}, "status": "reported", "reported_at": "2026-10-14T00:00:00Z",
	}
}

func TestRepairImmutableArtifactExclusivePublicationAndBoundRead(t *testing.T) {
	root := t.TempDir()
	path := artifactRoot + "/plan-reports/repair-plan-report-safety.json"
	ref, err := writeImmutable(root, path, "repair-plan-report.schema.json", immutablePlanReportFixture())
	if err != nil {
		t.Fatal(err)
	}
	data, err := readArtifact(root, ref, "repair-plan-report.schema.json")
	if err != nil || sha256Bytes(data) != ref.SHA256 {
		t.Fatalf("immutable bytes/SHA: %v", err)
	}
	absolute := ref
	absolute.Path = filepath.Join(root, filepath.FromSlash(ref.Path))
	if _, err := readArtifact(root, absolute, "repair-plan-report.schema.json"); err != nil {
		t.Fatalf("legacy contained absolute ref: %v", err)
	}
	if _, err := writeImmutable(root, path, "repair-plan-report.schema.json", immutablePlanReportFixture()); err == nil {
		t.Fatal("immutable artifact overwritten")
	}
	after, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil || string(data) != string(after) {
		t.Fatal("exclusive retry changed original bytes")
	}
}

func TestRepairImmutableArtifactRejectsSymlinkParentAndRead(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "review"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".claude", "review", "repair")); err != nil {
		t.Skip(err)
	}
	if _, err := writeImmutable(root, artifactRoot+"/plan-reports/repair-plan-report-safety.json", "repair-plan-report.schema.json", immutablePlanReportFixture()); err == nil {
		t.Fatal("followed symlink artifact parent")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("publication wrote outside original root")
	}
	bytes := []byte("outside")
	if err := os.WriteFile(filepath.Join(outside, "report.json"), bytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "report.json"), filepath.Join(root, "report.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := readArtifact(root, ArtifactRef{Path: "report.json", SHA256: sha256Bytes(bytes)}, ""); err == nil {
		t.Fatal("read followed symlink artifact")
	}
}

func TestRepairImmutableArtifactRejectsOversizedRead(t *testing.T) {
	root := t.TempDir()
	data := make([]byte, (20<<20)+1)
	if err := os.WriteFile(filepath.Join(root, "large.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readArtifact(root, ArtifactRef{Path: "large.json", SHA256: sha256Bytes(data)}, ""); err == nil {
		t.Fatal("read accepted artifact beyond bounded source limit")
	}
}

func TestRepairImmutableArtifactRejectsOversizedFinalSerializationBeforePublication(t *testing.T) {
	root := t.TempDir()
	document := immutablePlanReportFixture()
	document["plan"] = strings.Repeat("a", (20<<20)+1)
	if _, err := writeImmutable(root, artifactRoot+"/plan-reports/repair-plan-report-safety.json", "repair-plan-report.schema.json", document); err == nil {
		t.Fatal("oversized final immutable serialization accepted")
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("oversized artifact created control paths: %v", err)
	}
}
