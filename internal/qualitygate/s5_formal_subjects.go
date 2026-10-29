package qualitygate

import (
	"fmt"

	"github.com/entroforge/go-system-builder/internal/fileview"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
)

func verifiedS5FormalFacts(input Input) ([]documentFact, error) {
	files, _ := input.Files.(fileview.Reader)
	formal, err := runtimepkg.S5FormalInputs(input.Root, input.Snapshot.State, files)
	if err != nil {
		return nil, err
	}
	generation := nestedInt(input.Snapshot.State, "baseline", "generation")
	facts := make([]documentFact, 0, len(formal.Facts))
	for _, fact := range formal.Facts {
		data, err := input.Files.ReadFile(fact.Reference.Path)
		if err != nil || sha256Hex(data) != fact.Reference.SHA256 {
			return nil, fmt.Errorf("S5 formal subject drifted: %s", fact.Reference.Path)
		}
		facts = append(facts, documentFact{
			ID: fact.Reference.ID, Kind: fact.Kind, Path: fact.Reference.Path,
			Version: fact.Reference.Version, SHA256: fact.Reference.SHA256,
			Status: fact.Status, Generation: generation,
		})
	}
	return facts, nil
}

func verifiedCurrentDocumentsWithS5Facts(input Input) ([]documentFact, error) {
	documents := verifiedCurrentDocuments(input)
	lifecycle, _ := input.Snapshot.State["lifecycle"].(map[string]any)
	if !runtimepkg.PacketEnabled(input.Snapshot.State) || lifecycle["state"] != "document_verification" {
		return documents, nil
	}
	facts, err := verifiedS5FormalFacts(input)
	if err != nil {
		return nil, err
	}
	return append(documents, facts...), nil
}
