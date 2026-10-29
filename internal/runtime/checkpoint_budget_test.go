package runtime

import (
	"strings"
	"testing"
)

func TestCheckpointPublicationBudgetChecksExactProspectiveBytes(t *testing.T) {
	store := &Store{root: t.TempDir()}
	state := map[string]any{"schema_version": "1.2.0", "entities": map[string]any{"work_packets": []any{map[string]any{"packet_id": "fixture"}}}, "fixture": ""}
	event := map[string]any{"message": "fixture"}
	data, err := jsonDocumentBytes(state)
	if err != nil {
		t.Fatal(err)
	}
	state["fixture"] = strings.Repeat("x", checkpointStateLimit-len(data))
	if err := store.validateCheckpointPublicationBudget(state, event, journalInspection{}); err != nil {
		t.Fatalf("exact state limit rejected: %v", err)
	}
	state["fixture"] = state["fixture"].(string) + "x"
	if err := store.validateCheckpointPublicationBudget(state, event, journalInspection{}); err == nil || !strings.Contains(err.Error(), "16 MiB") {
		t.Fatalf("oversize candidate: %v", err)
	}
	state["fixture"] = ""
	prior := map[string]any{"message": ""}
	priorBytes, _ := jsonLineBytes(prior)
	eventBytes, _ := jsonLineBytes(event)
	prior["message"] = strings.Repeat("x", checkpointJournalLimit-len(priorBytes)-len(eventBytes))
	journal := journalInspection{Events: []map[string]any{prior}}
	if err := store.validateCheckpointPublicationBudget(state, event, journal); err != nil {
		t.Fatalf("exact complete journal limit rejected: %v", err)
	}
	prior["message"] = prior["message"].(string) + "x"
	if err := store.validateCheckpointPublicationBudget(state, event, journal); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("oversize prospective journal: %v", err)
	}
}

func TestCheckpointPublicationBudgetPreservesLegacyAndOfflineScope(t *testing.T) {
	oversize := map[string]any{"schema_version": "1.1.0", "fixture": strings.Repeat("x", checkpointStateLimit+1)}
	store := &Store{root: t.TempDir()}
	if err := store.validateCheckpointPublicationBudget(oversize, nil, journalInspection{}); err != nil {
		t.Fatal(err)
	}
	oversize["schema_version"] = "1.2.0"
	oversize["entities"] = map[string]any{"work_packets": []any{map[string]any{"packet_id": "fixture"}}}
	store.offlineRecovery = true
	if err := store.validateCheckpointPublicationBudget(oversize, nil, journalInspection{}); err != nil {
		t.Fatal(err)
	}
}

func TestHasIssuedIndicesIncludesUnifiedPacketRuntime(t *testing.T) {
	state := map[string]any{
		"schema_version": "1.3.0",
		"entities":       map[string]any{"work_packets": []any{map[string]any{"packet_id": "packet-1"}}},
	}
	if !hasIssuedIndices(state) {
		t.Fatal("unified packet Runtime with an issued packet was omitted from checkpoint publication")
	}
	state["schema_version"] = "1.1.0"
	if hasIssuedIndices(state) {
		t.Fatal("legacy Runtime was incorrectly treated as a packet Runtime")
	}
}
