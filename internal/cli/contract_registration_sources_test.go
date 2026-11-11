package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// A received S3 answer continues to S4 using current original source paths.
func TestS3RegistrationPreservesAcceptedResponsibilityAcrossSourceEdits(t *testing.T) {
	for _, changed := range []string{"docs/architecture/data-model/request.json", "docs/dev/contracts/BE-039.md"} {
		t.Run(filepath.Base(changed), func(t *testing.T) {
			root, packet := newContractAnswerFixture(t, func(root string) {
				index := "docs/dev/contracts/CONTRACTS-039.md"
				data, _ := os.ReadFile(filepath.Join(root, index))
				content := strings.Replace(string(data), "Shared model policy: none", "Shared model policy: json-schema-v1", 1)
				content += "| REQ-039/NFR-001 | — | BE-039 §1 | — |\n\n## Shared model baseline\n\n| Operation | Slot | Schema | Consumers | Valid example | Structural negative |\n|---|---|---|---|---|---|\n| fixture | request | [request](../../architecture/data-model/request.schema.json) | [BE](BE-039.md) | [valid](../../architecture/data-model/request.json) | N/A |\n"
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
			recovered, err := service.Recover(context.Background(), "Main", 1)
			if err != nil {
				t.Fatal(err)
			}
			after := stageSnapshot(t, root)
			resumed, found, err := runtime.FindSubmission(after.State, packet.ID)
			if err != nil || !found || resumed.ControlStatus != "advanced" || resumed.EvidenceID != accepted.EvidenceID {
				t.Fatalf("original accepted responsibility not resumed: %+v, %v", resumed, err)
			}
			if after.State["lifecycle"].(map[string]any)["phase"] != "tasks" {
				t.Fatalf("original S3 did not reach S4: %+v", recovered)
			}
			if data, err := os.ReadFile(filepath.Join(root, changed)); err != nil || string(data) != string(altered) {
				t.Fatalf("current authored source was replaced: %v", err)
			}

		})
	}
}
