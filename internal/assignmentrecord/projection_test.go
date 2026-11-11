package assignmentrecord

import "testing"

func TestAssignmentProjectionRequiresExactRecordAndScope(t *testing.T) {
	record := recordTestRecord()
	projection := map[string]any{"assignment_record": record.Reference(), "allowed_write_paths": record.EffectiveWritePaths}
	if err := ValidateProjection(record, projection); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"internal", "../outside", "internal/other"} {
		projection["allowed_write_paths"] = []string{path}
		if err := ValidateProjection(record, projection); err == nil {
			t.Fatalf("expanded scope %s accepted", path)
		}
	}
	projection["allowed_write_paths"] = record.EffectiveWritePaths
	ref := record.Reference()
	ref.Revision++
	projection["assignment_record"] = ref
	if err := ValidateProjection(record, projection); err == nil {
		t.Fatal("stale Record revision accepted")
	}
	projection["assignment_record"] = record.Reference()
	if err := ValidateProjection(record, projection); err != nil {
		t.Fatalf("original responsibility rejected without a digest: %v", err)
	}
}
