package runtime

import (
	"fmt"
	"sort"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// FormalInputDocument is the shared read projection of an input that a
// consumer may freeze for the current Runtime cursor. Kind is carried from
// Runtime.documents, or from an accepted typed-stage proof when the input is
// not registered there.
type FormalInputDocument struct {
	Reference workpacket.Reference
	Kind      string
}

// CurrentFormalInputDocuments is the common formal-input selector used by
// context builders and validators. Outside packet-enabled S5 it preserves the
// current-generation registered/locked projection. For packet-enabled S5 it
// delegates eligibility to S5FormalInputs, including the original accepted
// S4 closure proof, and retains the kinds proven by that result.
func CurrentFormalInputDocuments(root string, state map[string]any, files fileview.Reader) ([]FormalInputDocument, error) {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] == "document_verification" && PacketEnabled(state) {
		formal, err := S5FormalInputs(root, state, files)
		if err != nil {
			return nil, err
		}
		baseline, _ := state["baseline"].(map[string]any)
		generation, err := integerField(baseline, "generation")
		if err != nil {
			return nil, err
		}
		registered, err := currentRegisteredFormalInputDocuments(state, generation)
		if err != nil {
			return nil, err
		}
		byReference := make(map[workpacket.Reference]FormalInputDocument, len(registered)+len(formal.Facts))
		for _, document := range registered {
			byReference[document.Reference] = document
		}
		for _, fact := range formal.Facts {
			byReference[fact.Reference] = FormalInputDocument{Reference: fact.Reference, Kind: fact.Kind}
		}
		result := make([]FormalInputDocument, 0, len(formal.References))
		for _, ref := range formal.References {
			document, ok := byReference[ref]
			if !ok {
				return nil, fmt.Errorf("S5 formal input has no validated kind: %s", ref.Path)
			}
			result = append(result, document)
		}
		return result, nil
	}

	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return nil, err
	}
	return currentRegisteredFormalInputDocuments(state, generation)
}

// currentRegisteredFormalInputDocuments is the one shared legacy/current
// cursor predicate. S5's stronger accepted-source path is handled by
// S5FormalInputs before this projection is used.
func currentRegisteredFormalInputDocuments(state map[string]any, generation int) ([]FormalInputDocument, error) {
	docs, _ := state["documents"].([]any)
	result := make([]FormalInputDocument, 0, len(docs))
	for _, raw := range docs {
		document, _ := raw.(map[string]any)
		if document == nil {
			continue
		}
		currentGeneration, err := integerField(document, "generation")
		if err != nil {
			return nil, err
		}
		if currentGeneration != generation || (document["status"] != "registered" && document["status"] != "locked") {
			continue
		}
		ref := workpacket.Reference{
			ID:      stringValue(document["id"]),
			Path:    stringValue(document["path"]),
			Version: stringValue(document["version"]),
			SHA256:  stringValue(document["sha256"]),
		}
		result = append(result, FormalInputDocument{Reference: ref, Kind: stringValue(document["kind"])})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Reference.Path < result[j].Reference.Path })
	return result, nil
}
