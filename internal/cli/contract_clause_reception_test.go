package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestContractReceptionRejectsMentionThenAcceptsActualClauseDeclaration(t *testing.T) {
	const index = "docs/dev/contracts/CONTRACTS-039.md"
	const contract = "docs/dev/contracts/BE-039.md"
	root, packet := newContractAnswerFixture(t, func(root string) {
		data, err := os.ReadFile(filepath.Join(root, index))
		if err != nil {
			t.Fatal(err)
		}
		writeContractSource(t, root, index, string(data)+"| REQ-039/NFR-001 | — | BE-039 §9 | — |\n")
		data, err = os.ReadFile(filepath.Join(root, contract))
		if err != nil {
			t.Fatal(err)
		}
		writeContractSource(t, root, contract, string(data)+"\nA reference to §9 does not declare a deliverable obligation.\n")
		for _, rel := range []string{"docs/dev/tasks/TASK-template.md", "docs/rules/dispatch-plan.md"} {
			data, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), rel))
			if err != nil {
				t.Fatal(err)
			}
			writeContractSource(t, root, rel, string(data))
		}
	})
	stageJSON(t, root, packet.AnswerPath, completedContractDraft(packet))
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	beforeState, err := os.ReadFile(service.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(service.JournalPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt, saveErr := service.SaveContracts(context.Background(), packet.AnswerPath)
	if saveErr == nil || receipt.Accepted || !strings.Contains(saveErr.Error(), "BE-039") || !strings.Contains(saveErr.Error(), "§9") {
		t.Fatalf("a prose mention was accepted as the S3 obligation: %+v %v", receipt, saveErr)
	}
	for _, pair := range []struct {
		path string
		want []byte
	}{{service.StatePath, beforeState}, {service.JournalPath, beforeJournal}} {
		after, err := os.ReadFile(pair.path)
		if err != nil || !bytes.Equal(after, pair.want) {
			t.Fatalf("rejected undeclared clause mutated original Runtime/journal: %s %v", pair.path, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, contract))
	if err != nil {
		t.Fatal(err)
	}
	writeContractSource(t, root, contract, string(data)+"\n## Additional reviewed obligation\n| 本合同条款 | 条款 |\n|---|---|\n| §9 | Preserve the agreed fixture output |\n")
	fixtures.CommitCurrentFixture(t, root)
	// A fresh original consumer reads the updated declared Git source. No
	// hand-edited evidence, replacement packet, fingerprint or activation.
	service, err = submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, saveErr = service.SaveContracts(context.Background(), packet.AnswerPath)
	if saveErr != nil || !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("corrected same S3 responsibility did not receive: %+v %v", receipt, saveErr)
	}
	snapshot := stageSnapshot(t, root)
	accepted, found, err := runtime.FindSubmission(snapshot.State, packet.ID)
	if err != nil || !found || accepted.ControlStatus != "advanced" {
		t.Fatalf("corrected S3 did not use its original transition: %+v %v", accepted, err)
	}
	life := snapshot.State["lifecycle"].(map[string]any)
	if life["state"] != "planning" || life["phase"] != "tasks" {
		t.Fatalf("corrected S3 did not hand off to S4: %+v", life)
	}
}
