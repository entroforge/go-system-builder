package assignmentrecord

import "testing"

func TestAssignmentProjectionRequiresExactRecordAndScope(t *testing.T) {
	record := recordTestRecord()
	sha, err := record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	projection := map[string]any{"assignment_record": record.Reference(), "assignment_record_sha256": sha, "allowed_write_paths": record.EffectiveWritePaths}
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
	projection["assignment_record_sha256"] = ""
	if err := ValidateProjection(record, projection); err == nil {
		t.Fatal("missing Record digest accepted")
	}
}

func TestAssignmentFrozenSourceChangesReject(t *testing.T) {
	record := recordTestRecord()
	bytes := []byte("registered source bytes")
	for i := range record.ContextSources {
		record.ContextSources[i].SHA256 = digest(bytes)
	}
	for _, source := range record.ContextSources {
		switch source.Kind {
		case "manifest":
			record.Manifest = source
		case "task":
			record.Task = source
		case "agent_definition":
			record.AgentDefinition = source
		}
	}
	if err := VerifySources(record, func(string) ([]byte, error) { return bytes, nil }); err != nil {
		t.Fatal(err)
	}
	if err := VerifySources(record, func(string) ([]byte, error) { return []byte("changed source bytes"), nil }); err == nil {
		t.Fatal("changed source accepted")
	}
}
