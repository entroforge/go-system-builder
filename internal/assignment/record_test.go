package assignment

import "testing"

func TestAssignmentRecordCompatibilityWrappers(t *testing.T) {
	enabled, err := ManifestAssignmentRecordEnabled([]byte(`{"assignment_record_contract":"s6-v1.2"}`))
	if err != nil || !enabled {
		t.Fatalf("explicit v1.2 opt-in = %v, %v", enabled, err)
	}
	scope, err := EffectiveScope(ScopeBasis{
		ManifestScope:       []string{"internal/"},
		ManifestWritePaths:  []string{"internal/pkg/"},
		ManifestOutputPaths: []string{},
		TaskWritePaths:      []string{"internal/"},
	})
	if err != nil || len(scope) != 1 || scope[0] != "internal/pkg" {
		t.Fatalf("compatibility scope = %v, %v", scope, err)
	}
}
