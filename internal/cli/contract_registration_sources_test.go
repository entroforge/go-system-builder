package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// Keep a real accepted S3 reception pending, then change valid source bytes.
// Recovery must neither re-sign the envelope nor register a mixed baseline.
func TestS3RegistrationPreservesAcceptedSourceClosure(t *testing.T) {
	for _, changed := range []string{"docs/architecture/data-model/request.json", "docs/dev/contracts/BE-039.md"} {
		t.Run(filepath.Base(changed), func(t *testing.T) {
			root, packet := newContractAnswerFixture(t, func(root string) {
				index := "docs/dev/contracts/CONTRACTS-039.md"
				data, _ := os.ReadFile(filepath.Join(root, index))
				content := strings.Replace(string(data), "Shared model policy: none", "Shared model policy: json-schema-v1", 1)
				content += "\n## Shared model baseline\n\n| Operation | Slot | Schema | Consumers | Valid example | Structural negative |\n|---|---|---|---|---|---|\n| fixture | request | [request](../../architecture/data-model/request.schema.json) | [BE](BE-039.md) | [valid](../../architecture/data-model/request.json) | N/A |\n"
				writeContractSource(t, root, index, content)
				data, _ = os.ReadFile(filepath.Join(root, "docs/dev/contracts/BE-039.md"))
				writeContractSource(t, root, "docs/dev/contracts/BE-039.md", string(data)+"\n## Shared model inputs\n[Request](../../architecture/data-model/request.schema.json)\n")
				writeContractSource(t, root, "docs/architecture/data-model/request.schema.json", `{"type":"object","required":["value"],"properties":{"value":{"type":"string"}}}`)
				writeContractSource(t, root, "docs/architecture/data-model/request.json", `{"value":"accepted"}`)
			})
			stageJSON(t, root, packet.AnswerPath, completedContractDraft(packet))
			service, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := pendingMainFollowupContext(service, packet.ID)
			receipt, err := service.SaveContracts(ctx, packet.AnswerPath)
			cancel()
			if err != nil || !receipt.Accepted {
				t.Fatalf("accept original S3: %+v %v", receipt, err)
			}
			before := stageSnapshot(t, root)
			accepted, _, _ := runtime.FindSubmission(before.State, packet.ID)
			original, err := os.ReadFile(filepath.Join(root, changed))
			if err != nil {
				t.Fatal(err)
			}
			altered := append(append([]byte(nil), original...), '\n')
			writeContractSource(t, root, changed, string(altered))
			fixtures.CommitCurrentFixture(t, root)
			service, err = submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			stateBytes, _ := os.ReadFile(service.StatePath)
			journalBytes, _ := os.ReadFile(service.JournalPath)
			recovered, err := service.Recover(context.Background(), "Main", 1)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(recovered)
			if !strings.Contains(string(encoded), "accepted S3 source changed before registration: "+changed) {
				t.Fatalf("did not identify accepted-source drift: %s", encoded)
			}
			if data, _ := os.ReadFile(service.StatePath); string(data) != string(stateBytes) {
				t.Fatal("failed registration changed Runtime")
			}
			if data, _ := os.ReadFile(service.JournalPath); string(data) != string(journalBytes) {
				t.Fatal("failed registration changed journal")
			}
			writeContractSource(t, root, changed, string(original))
			fixtures.CommitCurrentFixture(t, root)
			service, _ = submission.New(root)
			if _, err := service.Recover(context.Background(), "Main", 1); err != nil {
				t.Fatal(err)
			}
			after := stageSnapshot(t, root)
			resumed, _, _ := runtime.FindSubmission(after.State, packet.ID)
			if resumed.ControlStatus != "advanced" || resumed.EvidenceID != accepted.EvidenceID || resumed.ContentHash != accepted.ContentHash {
				t.Fatalf("original accepted work not resumed: %+v", resumed)
			}
		})
	}
}
