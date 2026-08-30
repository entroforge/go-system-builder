package evidence_test

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/evidence"
)

func TestDefaultCatalogSeparatesSlotsFromRegisteredKinds(t *testing.T) {
	catalog := evidence.DefaultCatalog()
	tests := []struct {
		slot string
		want []string
	}{
		{slot: "builder_report_record", want: []string{"builder_report", "agent_completion"}},
		{slot: "team_manifest_record", want: []string{"builder_report", "team_manifest"}},
		{slot: "planning_design_record", want: []string{"planning_design"}},
		{slot: "pause_record", want: []string{"human_decision"}},
	}
	for _, test := range tests {
		got := catalog.RegisteredAcceptedKinds(test.slot)
		if len(got) != len(test.want) {
			t.Fatalf("%s registered accepted kinds = %v, want %v", test.slot, got, test.want)
		}
		for i := range test.want {
			if got[i] != test.want[i] {
				t.Fatalf("%s registered accepted kinds = %v, want %v", test.slot, got, test.want)
			}
		}
	}
	if catalog.IsRegisteredKind("builder_report_record") {
		t.Fatal("requirement slot must not be registerable as a persisted evidence kind")
	}
	if !catalog.Accepts("team_manifest_record", "team_manifest_record") {
		t.Fatal("legacy alias must remain compatible for in-memory consumers")
	}
}

func TestCatalogRejectsUnknownRequiredSlotAndExplainsMissingBinding(t *testing.T) {
	catalog := evidence.DefaultCatalog()
	if err := catalog.ValidateSlots([]string{"unknown_slot"}); err == nil {
		t.Fatal("unknown required slot must fail catalog closure")
	}
	message := catalog.MissingBindingMessage("TR-006", "builder_report_record")
	for _, want := range []string{
		"--evidence builder_report_record=<reference>",
		"builder_report",
		"agent_completion",
		"loop-harness explain TR-006",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("missing binding message %q lacks %q", message, want)
		}
	}
}

func TestGeneratedSlotExposesExecutableReferenceMetadata(t *testing.T) {
	generator, ok := evidence.DefaultCatalog().Generator("pause_record")
	if !ok {
		t.Fatal("pause_record must declare generator metadata")
	}
	if generator.Name != "pause_checkpoint" {
		t.Fatalf("generator name = %q, want pause_checkpoint", generator.Name)
	}
	if generator.Reference != "generated:pause_checkpoint" {
		t.Fatalf("generator reference = %q, want generated:pause_checkpoint", generator.Reference)
	}
	if generator.Description == "" {
		t.Fatal("generated slot should provide operator-facing description")
	}
}

func TestCatalogExposesImportAllowlistAndCompatibilityPreference(t *testing.T) {
	catalog := evidence.DefaultCatalog()

	preferred := catalog.PreferredKinds("team_manifest_record")
	if want := []string{"team_manifest", "team_manifest_record"}; len(preferred) != len(want) || preferred[0] != want[0] || preferred[1] != want[1] {
		t.Fatalf("team_manifest_record preferred kinds = %v, want %v", preferred, want)
	}

	importable := catalog.ImportableKinds()
	if len(importable) == 0 {
		t.Fatal("catalog import allowlist must not be empty")
	}
	for _, processBound := range []string{"agent_activation", "agent_completion", "agent_readback", "team_manifest"} {
		if catalog.IsImportableKind(processBound) {
			t.Fatalf("process-bound evidence %q must require new-epoch re-attestation", processBound)
		}
	}
	if catalog.IsImportableKind("team_manifest_record") {
		t.Fatal("requirement-slot alias must not become an importable persisted kind")
	}
}
